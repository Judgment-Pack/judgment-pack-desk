#!/usr/bin/env python3
"""Handwritten two-chat qualification peer; no auth files or network access."""
import json
import os
import sys

POLICY = "Explain briefly why the host must keep the declared policy boundary in force. Answer in one sentence without calling tools."
SCHEMA = "Read the schema with get_schema, then state what the recorded schema says in one sentence. Do not call any other tool."
role, trace = sys.argv[1:3]
if role not in ("native", "runtime") or not os.path.isabs(trace):
    sys.exit(60)
if sys.argv[3:] != (["app-server", "--stdio", "--strict-config"] if role == "native" else ["mcp"]):
    sys.exit(61)

def record(kind, case=""):
    fd = os.open(trace, os.O_WRONLY | os.O_APPEND | os.O_NOFOLLOW)
    try:
        os.write(fd, (json.dumps({"provenance": "offline-qualification", "role": role, "pid": os.getpid(), "kind": kind, "case": case}) + "\n").encode())
    finally:
        os.close(fd)

def send(value):
    print(json.dumps(value, separators=(",", ":")), flush=True)

def note(method, params):
    send({"method": method, "params": params})

def finish():
    # The existing public fixture's literal answer and optional usage observation.
    note("item/completed", {"threadId": "offline-thread", "turnId": "offline-turn", "item": {"id": "offline-message", "type": "agentMessage", "phase": "final_answer", "text": "Offline fixture answer."}})
    usage = {"inputTokens": 2, "cachedInputTokens": 0, "outputTokens": 3, "reasoningOutputTokens": 1, "totalTokens": 5}
    note("thread/tokenUsage/updated", {"threadId": "offline-thread", "turnId": "offline-turn", "tokenUsage": {"last": usage, "total": usage}})
    note("turn/completed", {"threadId": "offline-thread", "turn": {"id": "offline-turn", "status": "completed"}})

record("start")
request = None
case = None
callback = False
schema_calls = 0
for index, line in enumerate(sys.stdin):
    if index >= 128 or len(line) > 4 * 1024 * 1024:
        sys.exit(62)
    q = json.loads(line)
    method, p = q.get("method"), q.get("params", {})
    if "id" not in q:
        if method not in ("initialized", "notifications/initialized"):
            record("refused")
            sys.exit(63)
        continue
    if role == "runtime":
        if method == "initialize":
            result = {"protocolVersion": "2025-06-18", "capabilities": {"tools": {}, "prompts": {}}, "serverInfo": {"name": "offline-qualification", "version": "1"}}
        elif method == "tools/list":
            result = {"tools": [{"name": "get_schema", "description": "Public recorded schema fixture", "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False}}]}
        elif method == "prompts/list":
            result = {"prompts": [{"name": "author_pack", "arguments": []}]}
        elif method == "prompts/get" and p in ({"name": "author_pack"}, {"name": "author_pack", "arguments": {}}):
            result = {"messages": [{"role": "user", "content": {"type": "text", "text": "Answer the person's exact question."}}]}
        elif method == "tools/call" and p == {"name": "list_packs", "arguments": {}}:
            result = {"content": [{"type": "text", "text": "{\"packs\":[]}"}], "structuredContent": {"packs": []}, "isError": False}
        elif method == "tools/call" and p == {"name": "describe_runtime", "arguments": {}}:
            result = {"content": [{"type": "text", "text": "{\"name\":\"offline-qualification\"}"}]}
        elif method == "tools/call" and p == {"name": "get_schema", "arguments": {}} and schema_calls == 0:
            schema_calls += 1
            record("schema", "schema")
            result = {"content": [{"type": "text", "text": "{\"schema\":\"recorded\"}"}], "structuredContent": {"schema": "recorded"}, "isError": False}
        else:
            record("refused")
            sys.exit(64)
        record("metadata", method)
    elif method == "initialize":
        result = {"userAgent": "jps_desk/0.157.1 (Linux)"}
    elif method == "account/read":
        # Synthetic fixture account only; never reads or writes auth state.
        result = {"requiresOpenaiAuth": True, "account": {"type": "chatgpt", "planType": "plus"}}
    elif method == "model/list":
        models = ["gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5"] if p.get("includeHidden") else ["gpt-6-sol"]
        result = {"data": [{"model": m, "displayName": m, "defaultReasoningEffort": "medium", "supportedReasoningEfforts": [{"reasoningEffort": e} for e in ["low", "medium", "high"]]} for m in models]}
    elif method == "thread/start" and request is None:
        if p.get("model") != "gpt-6-sol" or p.get("allowProviderModelFallback") is not False:
            sys.exit(65)
        request = p
        result = {"thread": {"id": "offline-thread"}, "model": "gpt-6-sol", "modelProvider": "openai", "approvalPolicy": "never", "cwd": os.getcwd(), "reasoningEffort": "medium"}
    elif method == "turn/start" and case is None and request is not None:
        text = p.get("input", [{}])[0].get("text", "")
        if (POLICY in text) == (SCHEMA in text) or p.get("effort") != "medium":
            sys.exit(66)
        case = "policy" if POLICY in text else "schema"
        record("native-turn", case)
        send({"id": q["id"], "result": {"turn": {"id": "offline-turn"}}})
        if case == "policy":
            finish()
        else:
            tools = [t["name"] for t in request["dynamicTools"] if t["description"].startswith("get_schema:")]
            if len(tools) != 1:
                sys.exit(67)
            record("callback", case)
            send({"id": "offline-callback", "method": "item/tool/call", "params": {"threadId": "offline-thread", "turnId": "offline-turn", "callId": "offline-call", "tool": tools[0], "arguments": {}}})
        continue
    elif method is None and q["id"] == "offline-callback" and case == "schema" and not callback:
        callback = True
        if q.get("result", {}).get("success") is not True:
            sys.exit(68)
        finish()
        continue
    else:
        record("refused")
        sys.exit(69)
    send({"jsonrpc": "2.0", "id": q["id"], "result": result})
