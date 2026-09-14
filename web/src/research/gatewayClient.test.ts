import { afterEach, describe, expect, it, vi } from 'vitest'
import { GatewayError, OverBudget, acquire, newResearchSession, readBounded, registry, seal } from './gatewayClient'

function stub(handler: (input: string, init: RequestInit) => Response | Promise<Response>) {
  const calls: { input: string; init: RequestInit }[] = []
  vi.stubGlobal('fetch', async (input: string, init: RequestInit) => {
    calls.push({ input, init })
    return handler(input, init)
  })
  return calls
}

afterEach(() => vi.unstubAllGlobals())

describe('the gateway client', () => {
  it('posts an acquire on the relay with the session bearer and reads the answer strictly', async () => {
    const calls = stub(
      () =>
        new Response('{"result":{"status":200,"body":{"n":1.0}},"receipt":{"sessionId":"s1","callIndex":0},"salts":{"args":"aa"}}', {
          status: 200,
          headers: { 'content-type': 'application/json' }
        })
    )
    const answer = await acquire('s1', 'read', { path: '/', body: { url: 'https://x' } }, 1 << 20)
    expect(calls).toHaveLength(1)
    expect(calls[0]!.input).toBe('/api/research/gateway/acquire')
    expect(calls[0]!.init.method).toBe('POST')
    expect((calls[0]!.init.headers as Record<string, string>).Authorization).toMatch(/^Bearer /)
    expect(calls[0]!.init.credentials).toBe('omit')
    expect(JSON.parse(calls[0]!.init.body as string)).toEqual({ session: 's1', source: 'read', arguments: { path: '/', body: { url: 'https://x' } } })
    // The literal survives: 1.0 stays 1.0 rather than becoming 1.
    expect(answer.result).toMatchObject({ kind: 'object' })
    expect(JSON.stringify(answer.result)).toContain('"literal":"1.0"')
    expect(answer.salts).toEqual({ args: 'aa' })
    expect(answer.text.startsWith('{"result"')).toBe(true)
  })
  it('reports a refusal with the gateway’s own sentence, or the status line', async () => {
    stub(() => new Response('{"error":"source failed: adapter-http: the endpoint answered 401 Unauthorized"}', { status: 502 }))
    await expect(acquire('s1', 'read', {}, 1 << 20)).rejects.toMatchObject({ status: 502, message: expect.stringContaining('401 Unauthorized') })
    stub(() => new Response('{"error":"no research gateway is configured","code":"research-unconfigured"}', { status: 409 }))
    await expect(acquire('s1', 'read', {}, 1 << 20)).rejects.toMatchObject({ code: 'research-unconfigured' })
    stub(() => new Response('gateway down', { status: 503, statusText: 'Service Unavailable' }))
    await expect(acquire('s1', 'read', {}, 1 << 20)).rejects.toMatchObject({ message: 'the gateway answered 503 Service Unavailable' })
    stub(() => new Response('{"result":1}', { status: 200 }))
    await expect(acquire('s1', 'read', {}, 1 << 20)).rejects.toThrow(GatewayError)
  })
  it('refuses an answer that is not UTF-8, or not one JSON document', async () => {
    // An invalid byte inside a string of an otherwise valid envelope: a
    // decoder that replaced it would hand back a document that parses.
    const envelope = new Uint8Array([...new TextEncoder().encode('{"result":"a'), 0xff, ...new TextEncoder().encode('b","receipt":{}}')])
    stub(() => new Response(envelope, { status: 200 }))
    await expect(acquire('s1', 'read', {}, 1 << 20)).rejects.toThrow(/invalid|decode|UTF/i)
    stub(() => new Response('{"result":{},"receipt":{}} trailing', { status: 200 }))
    await expect(acquire('s1', 'read', {}, 1 << 20)).rejects.toThrow(/trailing/)
  })
  it('cuts an answer at the bytes the caller may still take, and reads nothing more of it', async () => {
    let pulled = 0
    const body = new ReadableStream<Uint8Array>({
      pull(controller) {
        pulled += 1
        controller.enqueue(new Uint8Array(1024).fill(0x20))
      }
    })
    stub(() => new Response(body, { status: 200 }))
    await expect(acquire('s1', 'read', {}, 4096)).rejects.toMatchObject({ name: 'OverBudget', limit: 4096 })
    // Five pulls at most: four within the limit and the one that crossed it.
    expect(pulled).toBeLessThanOrEqual(6)
    expect(Array.from(await readBounded(new Response('abc'), 3))).toEqual([97, 98, 99])
    // A source whose cancel never settles does not hold the refusal.
    const stuck = new ReadableStream<Uint8Array>({
      pull(controller) {
        controller.enqueue(new Uint8Array(2))
      },
      cancel: () => new Promise<void>(() => {})
    })
    await expect(readBounded(new Response(stuck), 1)).rejects.toThrow(OverBudget)
    await expect(readBounded(new Response('abcd'), 3)).rejects.toThrow(OverBudget)
  })
  it('seals and fetches the registry on their own routes', async () => {
    const calls = stub((input) =>
      input.endsWith('/seal')
        ? new Response('{"sessionId":"s1","finalCount":2}', { status: 200 })
        : new Response('{"sessionId":"s1"}\n', { status: 200 })
    )
    const sealed = await seal('s1')
    expect(sealed).toMatchObject({ kind: 'object' })
    expect(await registry()).toBe('{"sessionId":"s1"}\n')
    expect(calls.map((call) => [call.input, call.init.method])).toEqual([
      ['/api/research/gateway/seal', 'POST'],
      ['/api/research/gateway/registry', 'GET']
    ])
  })
  it('mints a flat session token', () => {
    const token = newResearchSession(new Date('2026-09-14T12:34:56Z'))
    expect(token).toMatch(/^desk-20260914T123456-[0-9a-f]{12}$/)
  })
})
