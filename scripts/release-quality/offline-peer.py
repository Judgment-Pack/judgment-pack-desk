#!/usr/bin/env python3
"""Finite handwritten offline peers. Never invokes a CLI or reads authentication."""
import json
import os
import sys

role, case = sys.argv[1:3]
extra = sys.argv[3:]
trace = None
if extra[:1] == ["--trace"]:
    trace, extra = extra[1], extra[2:]
    if not os.path.isabs(trace):
        sys.exit(59)
if role not in ("runtime", "native") or case not in ("policy", "schema"):
    sys.exit(60)
if role == "runtime" and extra != ["mcp"]:
    sys.exit(61)
if role == "native" and extra != ["app-server", "--stdio", "--strict-config"]:
    sys.exit(62)

def record(kind, tag=""):
    if trace:
        fd = os.open(trace, os.O_WRONLY | os.O_APPEND | os.O_NOFOLLOW)
        try:
            os.write(fd, (json.dumps({"kind": kind, "role": role, "pid": os.getpid(), "tag": tag}) + "\n").encode())
        finally:
            os.close(fd)

record("start")

def send(value):
    print(json.dumps(value, separators=(",", ":")), flush=True)

def note(method, params):
    send({"method": method, "params": params})

def finish():
    note("item/completed", {"threadId": "offline-thread", "turnId": "offline-turn", "item": {"id": "offline-message", "type": "agentMessage", "phase": "final_answer", "text": "Offline fixture answer."}})
    note("thread/tokenUsage/updated", {"threadId": "offline-thread", "turnId": "offline-turn", "tokenUsage": {"last": {"inputTokens": 2, "cachedInputTokens": 0, "outputTokens": 3, "reasoningOutputTokens": 1, "totalTokens": 5}, "total": {"inputTokens": 2, "cachedInputTokens": 0, "outputTokens": 3, "reasoningOutputTokens": 1, "totalTokens": 5}}})
    note("turn/completed", {"threadId": "offline-thread", "turn": {"id": "offline-turn", "status": "completed"}})

request = None
turns = callbacks = 0
for index, line in enumerate(sys.stdin):
    if index >= 40 or len(line) > 4 * 1024 * 1024:
        sys.exit(63)
    q = json.loads(line)
    method, p = q.get("method"), q.get("params", {})
    if "id" not in q:
        if method not in ("initialized", "notifications/initialized"):
            sys.exit(64)
        continue
    if role == "runtime":
        if method == "initialize":
            record("metadata", "initialize")
            result = {"protocolVersion": "2025-06-18", "capabilities": {"tools": {}, "prompts": {}}, "serverInfo": {"name": "offline-test", "version": "1"}}
        elif method == "tools/list":
            record("metadata", "tools/list")
            result = {"tools": [{"name": "get_schema", "description": "Public recorded schema fixture", "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False}}]}
        elif method == "prompts/list":
            record("metadata", "prompts/list")
            result = {"prompts": [{"name": "author_pack", "arguments": []}]}
        elif method == "prompts/get" and p.get("name") == "author_pack" and p.get("arguments", {}) == {}:
            record("metadata", "prompts/get")
            result = {"messages": [{"role": "user", "content": {"type": "text", "text": "Answer the person's exact question."}}]}
        elif method == "tools/call" and p == {"name": "list_packs", "arguments": {}}:
            # The mounted fixture creates an empty project, without pack files.
            # This is UI inventory metadata, never a model callback/tool offer.
            record("metadata", "list_packs")
            result = {"content": [{"type": "text", "text": "{\"packs\":[]}"}], "structuredContent": {"packs": []}, "isError": False}
        elif method == "tools/call" and p == {"name": "describe_runtime", "arguments": {}}:
            record("metadata", "describe_runtime")
            result = {"content": [{"type": "text", "text": "{\"name\":\"offline-test\"}"}]}
        elif method == "tools/call" and p == {"name": "get_schema", "arguments": {}} and case == "schema" and callbacks == 0:
            callbacks += 1
            record("schema")
            result = {"content": [{"type": "text", "text": "{\"schema\":\"recorded\"}"}], "structuredContent": {"schema": "recorded"}, "isError": False}
        else:
            record("refused")
            sys.exit(65)
    elif method == "initialize":
        result = {"userAgent": "jps_desk/0.157.1 (Linux)"}
    elif method == "account/read":
        result = {"requiresOpenaiAuth": True, "account": {"type": "chatgpt", "planType": "plus"}}
    elif method == "model/list":
        models = ["gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5"] if p.get("includeHidden") else ["gpt-6-sol"]
        result = {"data": [{"model": m, "displayName": m, "defaultReasoningEffort": "medium", "supportedReasoningEfforts": [{"reasoningEffort": e} for e in ["low", "medium", "high"]]} for m in models]}
    elif method == "thread/start" and request is None:
        request = p
        if p["model"] != "gpt-6-sol" or p["allowProviderModelFallback"] is not False:
            sys.exit(66)
        result = {"thread": {"id": "offline-thread"}, "model": "gpt-6-sol", "modelProvider": "openai", "approvalPolicy": "never", "cwd": os.getcwd(), "reasoningEffort": "medium"}
    elif method == "turn/start" and turns == 0:
        turns += 1
        record("native-turn")
        send({"id": q["id"], "result": {"turn": {"id": "offline-turn"}}})
        if case == "policy":
            finish()
        else:
            tool = next(t["name"] for t in request["dynamicTools"] if t["description"].startswith("get_schema:"))
            record("callback")
            send({"id": "offline-callback", "method": "item/tool/call", "params": {"threadId": "offline-thread", "turnId": "offline-turn", "callId": "offline-call", "tool": tool, "arguments": {}}})
        continue
    elif method is None and q["id"] == "offline-callback" and case == "schema" and callbacks == 0:
        callbacks += 1
        if q.get("result", {}).get("success") is not True:
            record("refused")
            sys.exit(68)
        finish()
        continue
    else:
        record("refused")
        sys.exit(67)
    send({"jsonrpc": "2.0", "id": q["id"], "result": result})
