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

/**
 * A transport that sends nowhere and remembers everything — **as bytes**.
 *
 * The serialized frame is what the chassis relays and what the runtime reads,
 * and it is not the object: an inherited `rehearsal: true` reads as `true` from
 * the object and is absent from the bytes. So assertions are made on
 * `JSON.stringify`'d text parsed back, exactly as `DeskWebSocketTransport`
 * writes it, and the object is kept only where identity is the claim.
 */
function recorder(): {
  transport: Transport
  sent: JSONRPCMessage[]
  wire: string[]
  onWire: () => Record<string, unknown>[]
} {
  const sent: JSONRPCMessage[] = []
  const wire: string[] = []
  const transport: Transport = {
    async start() {},
    async close() {},
    async send(message: JSONRPCMessage) {
      sent.push(message)
      // The transport's own line, byte for byte.
      wire.push(JSON.stringify(message))
    }
  }
  return {
    transport,
    sent,
    wire,
    onWire: () => wire.map((line) => JSON.parse(line) as Record<string, unknown>)
  }
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
  const { transport, sent, wire, onWire } = recorder()
  const notices: GuardrailNotice[] = []
  gateTransport(transport, { allowed, onGuardrail: (notice) => notices.push(notice) })
  return { transport, sent, wire, onWire, notices }
}

/** The `arguments` object of the first frame that actually reached the wire. */
function wireArguments(onWire: () => Record<string, unknown>[]): Record<string, unknown> {
  const frames = onWire()
  expect(frames.length, 'no frame reached the wire').toBeGreaterThan(0)
  return (frames[0]!.params as { arguments: Record<string, unknown> }).arguments
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

describe('the rehearsal rewrite, read off the wire', () => {
  it('rewrites a call that carried no rehearsal member', async () => {
    const { transport, onWire, notices } = gated(FIVE)
    await transport.send(call('experimental_evaluate', { pack: '{"a":1}', facts: '{}' }))
    const args = wireArguments(onWire)
    expect(args.rehearsal).toBe(true)
    // Everything else travels exactly as the engine wrote it.
    expect(args.pack).toBe('{"a":1}')
    expect(args.facts).toBe('{}')
    expect(notices[0]!.action).toBe('rewrote')
  })

  it('rewrites a call that said rehearsal: false', async () => {
    const { transport, onWire, notices } = gated(FIVE)
    await transport.send(call('experimental_evaluate', { pack: '{}', rehearsal: false }))
    expect(wireArguments(onWire).rehearsal).toBe(true)
    expect(notices[0]!.detail).toContain('rehearsal: false')
  })

  it('rewrites a call whose rehearsal member is a truthy non-true value', async () => {
    // `"true"` is not `true`, and a gate that read this as satisfied would let
    // a call through on the strength of a string.
    const { transport, onWire } = gated(FIVE)
    await transport.send(call('experimental_evaluate', { pack: '{}', rehearsal: 'true' }))
    expect(wireArguments(onWire).rehearsal).toBe(true)
  })

  it('sends an own rehearsal: true on, and says nothing about it', async () => {
    const { transport, onWire, notices } = gated(FIVE)
    await transport.send(call('experimental_evaluate', { pack: '{}', rehearsal: true }))
    expect(wireArguments(onWire)).toEqual({ pack: '{}', rehearsal: true })
    expect(notices).toEqual([])
  })

  /**
   * **The shape corpus.** Every one of these reads as satisfied to a check on
   * the object, or defeats a copy written the obvious way; the assertion is
   * always on the bytes, because the bytes are what the runtime reads.
   */
  describe('whatever shape the arguments arrive in', () => {
    it('sends an own rehearsal: true where the value was only inherited', async () => {
      // The defect this rebuild exists for. `args.rehearsal` reads `true`, and
      // `JSON.stringify` drops it: an inspect-and-forward gate would have sent
      // an unrehearsed evaluation and the runtime would have recorded it.
      const inherited = Object.create({ rehearsal: true }) as Record<string, unknown>
      inherited.pack = '{"a":1}'
      const { transport, onWire, notices } = gated(FIVE)
      await transport.send(call('experimental_evaluate', inherited))
      expect(wireArguments(onWire)).toEqual({ pack: '{"a":1}', rehearsal: true })
      expect(notices[0]!.detail).toContain('inherited')
    })

    it('sends an own rehearsal: true for a null-prototype object', async () => {
      const bare = Object.create(null) as Record<string, unknown>
      bare.pack = '{}'
      const { transport, onWire } = gated(FIVE)
      await transport.send(call('experimental_evaluate', bare))
      expect(wireArguments(onWire)).toEqual({ pack: '{}', rehearsal: true })
    })

    it('does not mutate a frozen arguments object, and still sends the member', async () => {
      const frozen = Object.freeze({ pack: '{}' })
      const { transport, onWire } = gated(FIVE)
      await transport.send(call('experimental_evaluate', frozen as Record<string, unknown>))
      expect(wireArguments(onWire)).toEqual({ pack: '{}', rehearsal: true })
      expect(Object.hasOwn(frozen, 'rehearsal')).toBe(false)
    })

    it('reads a getter once, so it cannot answer the check and the wire differently', async () => {
      // Two reads, two answers, is how a check on the object and a
      // serialization of the object come apart. There is one read.
      let reads = 0
      const shifty = {
        pack: '{}',
        get rehearsal() {
          reads += 1
          return reads === 1
        }
      }
      const { transport, onWire } = gated(FIVE)
      await transport.send(call('experimental_evaluate', shifty as Record<string, unknown>))
      expect(wireArguments(onWire).rehearsal).toBe(true)
    })

    it('sends an own rehearsal: true through a proxy that lies about the member', async () => {
      const lying = new Proxy(
        { pack: '{}' } as Record<string, unknown>,
        {
          get(target, key) {
            if (key === 'rehearsal') return true
            return Reflect.get(target, key)
          }
        }
      )
      const { transport, onWire } = gated(FIVE)
      await transport.send(call('experimental_evaluate', lying))
      // `rehearsal` is not an own key of the target, so the proxy's `get` lie
      // never reaches the bytes — and the rebuild puts a real one there.
      expect(wireArguments(onWire)).toEqual({ pack: '{}', rehearsal: true })
    })

    it('sends the member where the call carried no arguments at all', async () => {
      const { transport, onWire } = gated(FIVE)
      await transport.send({
        jsonrpc: '2.0',
        id: 5,
        method: 'tools/call',
        params: { name: 'experimental_evaluate' }
      } as unknown as JSONRPCMessage)
      expect(wireArguments(onWire)).toEqual({ rehearsal: true })
    })

    it.each([
      ['a string', 'not an object'],
      ['a number', 7],
      ['null', null],
      ['an array', [1, 2]]
    ])('refuses arguments that are %s rather than guessing', async (_what, args) => {
      const { transport, sent } = gated(FIVE)
      await expect(
        transport.send({
          jsonrpc: '2.0',
          id: 6,
          method: 'tools/call',
          params: { name: 'experimental_evaluate', arguments: args }
        } as unknown as JSONRPCMessage)
      ).rejects.toThrow(GateViolation)
      expect(sent).toEqual([])
    })
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

describe('the frames the gate will not send at all', () => {
  it.each([
    ['a JSON-RPC batch', [{ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'validate' } }]],
    [
      'a batch mixing an allowed call with a write',
      [
        { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'validate' } },
        { jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'write_file' } }
      ]
    ],
    ['null', null],
    ['a string', 'tools/call'],
    ['a number', 7],
    ['a frame with a numeric method', { jsonrpc: '2.0', id: 1, method: 9 }],
    ['a frame that is neither request nor response', { jsonrpc: '2.0', id: 1 }],
    ['a response with no id', { jsonrpc: '2.0', result: {} }],
    ['Tools/Call', { jsonrpc: '2.0', id: 1, method: 'Tools/Call', params: { name: 'validate' } }],
    ['TOOLS/CALL', { jsonrpc: '2.0', id: 1, method: 'TOOLS/CALL', params: { name: 'validate' } }],
    [
      'tools/call with whitespace around it',
      { jsonrpc: '2.0', id: 1, method: ' tools/call ', params: { name: 'validate' } }
    ],
    ['tools/call with no params', { jsonrpc: '2.0', id: 1, method: 'tools/call' }],
    [
      'tools/call whose params are a string',
      { jsonrpc: '2.0', id: 1, method: 'tools/call', params: 'name=validate' }
    ]
  ])('refuses %s, and nothing reaches the socket', async (_what, frame) => {
    // **Fail closed.** The rule this replaces was "anything whose method is not
    // exactly tools/call is traffic I have no opinion about", and a batch has
    // no method at all — so an array carrying an allowed call beside a
    // write_file went out whole. The SDK's type says one message; the type is
    // not what runs.
    const { transport, sent } = gated(FIVE)
    await expect(transport.send(frame as unknown as JSONRPCMessage)).rejects.toThrow(GateViolation)
    expect(sent).toEqual([])
  })
})

describe('what the gate leaves alone', () => {
  it.each([
    ['initialize', { jsonrpc: '2.0', id: 1, method: 'initialize', params: {} }],
    ['tools/list', { jsonrpc: '2.0', id: 2, method: 'tools/list', params: {} }],
    ['prompts/get', { jsonrpc: '2.0', id: 3, method: 'prompts/get', params: { name: 'p' } }],
    ['a notification', { jsonrpc: '2.0', method: 'notifications/initialized' }],
    ['a notification with no params', { jsonrpc: '2.0', method: 'notifications/cancelled' }],
    ['a response', { jsonrpc: '2.0', id: 4, result: {} }],
    ['an error response', { jsonrpc: '2.0', id: 5, error: { code: -32601, message: 'no' } }]
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

  it('rebuilds an evaluate frame even where nothing needed reporting', () => {
    // There is no branch that forwards the caller's object, which is why there
    // is no shape that can slip past.
    const original = call('experimental_evaluate', { pack: '{}', rehearsal: true })
    const decided = inspectOutboundFrame(original, FIVE)
    expect(decided.verdict).toBe('rewrote')
    expect(decided.verdict === 'rewrote' && decided.notice).toBeNull()
    expect(decided.verdict === 'rewrote' && decided.frame).not.toBe(original)
  })
})
