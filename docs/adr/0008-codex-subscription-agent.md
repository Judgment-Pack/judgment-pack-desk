---
status: proposed
date: 2026-09-25
---

# A constrained Codex subprocess for ChatGPT subscription access

ChatGPT subscription authentication belongs to the official Codex client. Desk
currently runs a browser Vercel loop through an API-key relay, so a subscription
requires a second engine adapter. It cannot be represented as another API key.

Amend ADR-0001's browser-only rule narrowly: permit a managed local Codex App
Server process through a bounded Go bridge. The browser remains the MCP client;
all JPS dynamic-tool callbacks return to the existing browser `callTool` and
ToolGate. Host tools, proposal handling, critique, and explicit acceptance keep
their existing ownership. There is no OpenHands service, token import, or
subscription-to-API translation.

The candidate native boundary is `environments: []` on both thread and turn,
a private profile, a private empty working directory, restricted named
permissions, disabled ambient integrations, and a model catalog Desk supplies.
Codex takes a model's tool mode and sub-agent version from the catalog before
it reads the disabled features, and a signed-in client fetches the catalog from
the account; Desk's copy of the pinned release's catalog clears that metadata
for every listed model, and a launched process must list exactly those models.
The empty orchestrator-skills utilities are the only exception to Desk-supplied
tools; they grant no JPS, shell, filesystem, browser, or subagent capability.
The [local proof](../reviews/codex-subscription-proof.md) records the exact
inventory for every listed model and adversarial call results, including
failing negative controls.

This ADR remains proposed pending authenticated release evidence. The private
account bridge, bounded run transport, configuration and setup UI are implemented
with managed runtime preparation on explicit connection. API and agent engines have separate
protocol matrices and share the recorded JPS scenario. The
[setup record](../design/codex-subscription-setup.md) documents activation,
compatibility and verification limits. Local checks do not substitute for a real
subscription smoke test, and the experimental source implementation is not a general
release certification.
