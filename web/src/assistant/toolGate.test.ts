/**
 * The gate, measured at the frame and at the socket.
 *
 * Every assertion here is about **what left the page**, never about what the
 * gate believes it did: the recorder is a transport that keeps the frames it
 * was handed, so a gate that emitted the right notice and sent the wrong frame
 * fails. That is the same discipline the chassis' relay tests use, and it is
 * the reason these are not assertions on `inspectOutboundFrame` alone.
 */
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js'
import type { JSONRPCMessage } from '@modelcontextprotocol/sdk/types.js'
import { describe, expect, it } from 'vitest'
import { ASSISTANT_TOOLS } from '../config/deskConfig'
import {
  GateViolation,
  allowedTools,
  gateTransport,
  inspectOutboundFrame,
  type GuardrailNotice
} from './toolGate'

const FIVE = allowedTools([...ASSISTANT_TOOLS])

/** A transport that sends nowhere and remembers everything. */
function recorder(): { transport: Transport; sent: JSONRPCMessage[] } {
  const sent: JSONRPCMessage[] = []
  const transport: Transport = {
    async start() {},
    async close() {},
    async send(message: JSONRPCMessage) {
      sent.push(message)
    }
  }
  return { transport, sent }
}

function call(name: string, args: Record<string, unknown> = {}): JSONRPCMessage {
  return {
    jsonrpc: '2.0',
    id: 1,
    method: 'tools/call',
    params: { name, arguments: args }
  } as unknown as JSONRPCMessage
}

function gated(allowed: ReadonlySet<string>) {
  const { transport, sent } = recorder()
  const notices: GuardrailNotice[] = []
  gateTransport(transport, { allowed, onGuardrail: (notice) => notices.push(notice) })
  return { transport, sent, notices }
}

describe('the allow-list', () => {
  it('is the configured subset intersected with the five', () => {
    expect([...allowedTools(['validate', 'get_schema'])]).toEqual(['validate', 'get_schema'])
    // The ceiling holds even where a caller wrote a longer list.
    expect([...allowedTools(['validate', 'write_file', 'bash'])]).toEqual(['validate'])
    expect([...allowedTools([])]).toEqual([])
  })

  it('refuses a name outside it, and the frame never reaches the socket', async () => {
    const { transport, sent, notices } = gated(FIVE)
    await expect(transport.send(call('write_file', { path: 'p', content: 'c' }))).rejects.toThrow(
      GateViolation
    )
    expect(sent).toEqual([])
    expect(notices).toHaveLength(1)
    expect(notices[0]!.action).toBe('refused')
    expect(notices[0]!.tool).toBe('write_file')
  })

  it('names the tool on the violation, not only in its sentence', async () => {
    const { transport } = gated(FIVE)
    const refused = await transport.send(call('get_pack')).catch((error: unknown) => error)
    expect(refused).toBeInstanceOf(GateViolation)
    expect((refused as GateViolation).tool).toBe('get_pack')
  })

  it('refuses a tool the desk allows but this file did not grant', async () => {
    const { transport, sent, notices } = gated(allowedTools(['get_schema']))
    await expect(transport.send(call('validate', { document: '{}' }))).rejects.toThrow(GateViolation)
    expect(sent).toEqual([])
    expect(notices[0]!.detail).toContain('get_schema')
  })

  it('refuses everything where the file granted nothing', async () => {
    const { transport, sent } = gated(allowedTools([]))
    for (const tool of ASSISTANT_TOOLS) {
      await expect(transport.send(call(tool))).rejects.toThrow(GateViolation)
    }
    expect(sent).toEqual([])
  })

  it('refuses a frame whose tool name is not a string at all', async () => {
    const { transport, sent } = gated(FIVE)
    const frame = {
      jsonrpc: '2.0',
      id: 2,
      method: 'tools/call',
      params: { name: 7, arguments: {} }
    } as unknown as JSONRPCMessage
    await expect(transport.send(frame)).rejects.toThrow(GateViolation)
    expect(sent).toEqual([])
  })
})

describe('the rehearsal rewrite', () => {
  it('rewrites a call that carried no rehearsal member', async () => {
    const { transport, sent, notices } = gated(FIVE)
    await transport.send(call('experimental_evaluate', { pack: '{"a":1}', facts: '{}' }))
    expect(sent).toHaveLength(1)
    const params = (sent[0] as unknown as { params: { arguments: Record<string, unknown> } }).params
    expect(params.arguments.rehearsal).toBe(true)
    // Everything else travels exactly as the engine wrote it.
    expect(params.arguments.pack).toBe('{"a":1}')
    expect(params.arguments.facts).toBe('{}')
    expect(notices[0]!.action).toBe('rewrote')
  })

  it('rewrites a call that said rehearsal: false', async () => {
    const { transport, sent, notices } = gated(FIVE)
    await transport.send(call('experimental_evaluate', { pack: '{}', rehearsal: false }))
    const params = (sent[0] as unknown as { params: { arguments: Record<string, unknown> } }).params
    expect(params.arguments.rehearsal).toBe(true)
    expect(notices[0]!.detail).toContain('rehearsal: false')
  })

  it('rewrites a call whose rehearsal member is a truthy non-true value', async () => {
    // `"true"` is not `true`, and a gate that read this as satisfied would let
    // a call through on the strength of a string.
    const { transport, sent } = gated(FIVE)
    await transport.send(call('experimental_evaluate', { pack: '{}', rehearsal: 'true' }))
    const params = (sent[0] as unknown as { params: { arguments: Record<string, unknown> } }).params
    expect(params.arguments.rehearsal).toBe(true)
  })

  it('sends a call that already said rehearsal: true untouched, and says nothing', async () => {
    const { transport, sent, notices } = gated(FIVE)
    const frame = call('experimental_evaluate', { pack: '{}', rehearsal: true })
    await transport.send(frame)
    expect(sent[0]).toBe(frame)
    expect(notices).toEqual([])
  })

  it('never puts the pack or the facts in the notice', async () => {
    const { transport, notices } = gated(FIVE)
    await transport.send(
      call('experimental_evaluate', { pack: '{"title":"SECRET DRAFT"}', facts: '{"x":"SECRET"}' })
    )
    expect(notices[0]!.detail).not.toContain('SECRET')
  })

  it('refuses before it rewrites, where the file granted no evaluate', async () => {
    // The one case the order decides: a tool nobody granted must not be
    // corrected on its way out.
    const { transport, sent, notices } = gated(allowedTools(['validate']))
    await expect(transport.send(call('experimental_evaluate', { pack: '{}' }))).rejects.toThrow(
      GateViolation
    )
    expect(sent).toEqual([])
    expect(notices.map((notice) => notice.action)).toEqual(['refused'])
  })
})

describe('what the gate leaves alone', () => {
  it.each([
    ['initialize', { jsonrpc: '2.0', id: 1, method: 'initialize', params: {} }],
    ['tools/list', { jsonrpc: '2.0', id: 2, method: 'tools/list', params: {} }],
    ['prompts/get', { jsonrpc: '2.0', id: 3, method: 'prompts/get', params: { name: 'p' } }],
    ['a notification', { jsonrpc: '2.0', method: 'notifications/initialized' }],
    ['a response', { jsonrpc: '2.0', id: 4, result: {} }]
  ])('passes %s through by identity', async (_what, frame) => {
    const { transport, sent, notices } = gated(FIVE)
    await transport.send(frame as unknown as JSONRPCMessage)
    expect(sent).toEqual([frame])
    expect(sent[0]).toBe(frame)
    expect(notices).toEqual([])
  })

  it('carries the send options through', async () => {
    const seen: unknown[] = []
    const transport: Transport = {
      async start() {},
      async close() {},
      async send(_message: JSONRPCMessage, options?: unknown) {
        seen.push(options)
      }
    }
    gateTransport(transport, { allowed: FIVE })
    await transport.send(call('validate', { document: '{}' }), {
      relatedRequestId: 9
    } as never)
    await transport.send(call('experimental_evaluate', { pack: '{}' }), {
      relatedRequestId: 10
    } as never)
    expect(seen).toEqual([{ relatedRequestId: 9 }, { relatedRequestId: 10 }])
  })
})

describe('inspectOutboundFrame, with no socket at all', () => {
  it('reports the verdict a frame would get', () => {
    expect(inspectOutboundFrame(call('validate', { document: '{}' }), FIVE).verdict).toBe('pass')
    expect(inspectOutboundFrame(call('write_file'), FIVE).verdict).toBe('refused')
    expect(inspectOutboundFrame(call('experimental_evaluate', { pack: '{}' }), FIVE).verdict).toBe(
      'rewrote'
    )
  })

  it('leaves the caller’s frame unmutated when it rewrites', () => {
    // The rewrite builds a new frame. A gate that edited the caller's object
    // would change what the engine believes it asked for, which is the one
    // thing a guardrail may not do quietly.
    const original = call('experimental_evaluate', { pack: '{}' })
    const decided = inspectOutboundFrame(original, FIVE)
    expect(decided.verdict).toBe('rewrote')
    const params = (original as unknown as { params: { arguments: Record<string, unknown> } }).params
    expect(params.arguments.rehearsal).toBeUndefined()
  })
})
