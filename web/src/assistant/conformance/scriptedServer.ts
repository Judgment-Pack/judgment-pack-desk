/**
 * The runtime, recorded once and replayed on an in-memory transport pair.
 *
 * `runtime.json` holds `tools/list` as a real `jpack mcp` served it and one
 * `tools/call` answer per scenario step, recorded against the runtime binary
 * in a project copy that declared an audit trail. So the tool definitions the
 * model is offered are the runtime's own — schemas included — and the answers
 * the loop reasons over are the runtime's own words rather than a fixture
 * somebody wrote to make a test pass.
 *
 * **A call is matched by its arguments, not by its turn.** T4 and T5 are both
 * `validate` and differ only in the document, and T6 is recorded with
 * `rehearsal: true` because that is what the gate lets out of the page. So a
 * call whose arguments do not match a recording is a **failure** and not a
 * fallback: this is where K3(a) is measured — at the wire, on what the server
 * received, rather than on what the page believes it sent.
 *
 * Two arrivals are named refusals rather than mismatches, because they are the
 * two the scenario exists to provoke:
 *
 * - `experimental_evaluate` with no `rehearsal: true` (K3a). An unguarded one
 *   would append to the project's audit trail, which is what the experiment's
 *   positive control demonstrated it does.
 * - `write_file`, under any arguments (K3b). The runtime has no such tool; a
 *   frame carrying it means the gate did not stop it.
 *
 * The server is written against the JSON-RPC frames directly rather than
 * through the SDK's `Server`, for the reason the desk's own transport is: what
 * is under test is the wire, and a helper that reshapes a frame on its way in
 * is a helper that can hide the thing being measured.
 */
import { InMemoryTransport } from '@modelcontextprotocol/sdk/inMemory.js'
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js'
import type { JSONRPCMessage } from '@modelcontextprotocol/sdk/types.js'
import runtime from './runtime.json'
import type { McpTool } from '../engine'

export const RECORDED_TOOLS = runtime.tools as McpTool[]

interface RecordedCall {
  step: string
  tool: string
  arguments: Record<string, unknown>
  result: unknown
}

const RECORDED_CALLS = runtime.calls as RecordedCall[]

/** What the scripted server saw, in arrival order. */
export interface ServerObservation {
  name: string
  args: Record<string, unknown>
  /** Empty where the call matched a recording; otherwise why it did not. */
  refusal: string
}

/** Stable-key JSON, so two argument objects compare by value and not by order. */
function canonical(value: unknown): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value) ?? 'null'
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`
  const entries = Object.entries(value as Record<string, unknown>).sort(([a], [b]) =>
    a < b ? -1 : a > b ? 1 : 0
  )
  return `{${entries.map(([key, inner]) => `${JSON.stringify(key)}:${canonical(inner)}`).join(',')}}`
}

export interface ScriptedRuntime {
  /** The client side of the pair. Wrap this in the ToolGate. */
  transport: Transport
  seen: ServerObservation[]
  close(): Promise<void>
}

/**
 * Stand one recorded runtime up.
 *
 * Nothing is spawned, nothing is listened on, and no runtime binary is needed:
 * the suite has to run in CI, keyless and offline, or it is not the desk's
 * conformance session.
 */
export async function scriptedRuntime(): Promise<ScriptedRuntime> {
  const [clientSide, serverSide] = InMemoryTransport.createLinkedPair()
  const seen: ServerObservation[] = []

  serverSide.onmessage = (message: JSONRPCMessage) => {
    const frame = message as {
      id?: string | number
      method?: string
      params?: { name?: string; arguments?: Record<string, unknown> }
    }
    if (frame.id === undefined || frame.method === undefined) return // a notification
    const reply = (result: unknown) =>
      void serverSide.send({ jsonrpc: '2.0', id: frame.id, result } as JSONRPCMessage)
    const fail = (code: number, message: string) =>
      void serverSide.send({
        jsonrpc: '2.0',
        id: frame.id,
        error: { code, message }
      } as unknown as JSONRPCMessage)

    if (frame.method === 'initialize') {
      reply({
        protocolVersion: runtime.runtime.protocolVersion,
        capabilities: { tools: {}, prompts: {} },
        serverInfo: runtime.runtime.serverInfo
      })
      return
    }
    if (frame.method === 'tools/list') {
      reply({ tools: RECORDED_TOOLS })
      return
    }
    if (frame.method !== 'tools/call') {
      fail(-32601, `the scripted runtime does not serve ${frame.method}`)
      return
    }

    const name = String(frame.params?.name ?? '')
    const args = frame.params?.arguments ?? {}

    if (name === 'write_file') {
      // K3(b), measured where it matters: the frame got here.
      seen.push({ name, args, refusal: 'write_file reached the runtime' })
      fail(-32602, 'unknown tool: write_file')
      return
    }
    if (name === 'experimental_evaluate' && args.rehearsal !== true) {
      // K3(a), likewise: an unguarded evaluate would leave an audit record.
      seen.push({
        name,
        args,
        refusal: 'experimental_evaluate reached the runtime without rehearsal: true'
      })
      fail(-32602, 'the evaluation would have been recorded')
      return
    }

    const match = RECORDED_CALLS.find(
      (call) => call.tool === name && canonical(call.arguments) === canonical(args)
    )
    if (!match) {
      seen.push({
        name,
        args,
        refusal: `no recorded answer for ${name} with these arguments`
      })
      fail(-32602, `the scripted runtime has no recorded answer for ${name} with these arguments`)
      return
    }
    seen.push({ name, args, refusal: '' })
    reply(match.result)
  }

  await serverSide.start()
  return {
    transport: clientSide,
    seen,
    close: async () => {
      await serverSide.close()
      await clientSide.close()
    }
  }
}
