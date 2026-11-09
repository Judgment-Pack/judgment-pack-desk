import { afterEach, describe, expect, it, vi } from 'vitest'
import { normalize } from '@judgment-pack/harness/thinking'
import { vercel } from '@judgment-pack/harness/vercel'
import { scenario, scriptedModel, scriptedRuntime } from '@judgment-pack/harness/certification'
import { bindModelCall, openAssistantConnection, runAssistantSession } from '@judgment-pack/harness/session'
import type { AssistantEvent } from '@judgment-pack/harness/engine'
import { sessionBearer } from '../mcp/session'
import './harnessHost'

/**
 * The recorded scenario of `@judgment-pack/harness`, run through the engine on
 * the **Desk's own host wiring** (`harnessHost.ts`, imported above exactly as
 * `main.tsx` imports it), and the injection points it reaches checked on the
 * wire: the session's bearer, the relay's address, the connection's headers
 * and the Desk's own headers.
 *
 * This is the part of the package's certification the Desk can run in its own
 * suite, from what the installed package ships (`@judgment-pack/harness/
 * certification`: the scenario, the scripted model, the scripted runtime). The
 * whole suite, with its checks of both adapters, runs in CI on the same wiring
 * from the revision the lock pins (`.github/workflows/harness-certification.yml`).
 * A change to an injection point that breaks the wiring fails here and there.
 */
afterEach(() => vi.unstubAllGlobals())

const CONNECTION = { connectionId: 'ai-111111111111111111111111', connectionRevision: 'a'.repeat(64) }

describe('the recorded scenario on the Desk\'s wiring', () => {
  it('completes on the OpenAI-compatible wire through the relay, with the session\'s bearer and the connection named', async () => {
    const model = scriptedModel({ api: 'openai-compatible', answerAs: 'stream', thinking: 'off', refuted: false, signatures: 'thought', toolCalls: 'native' })
    const calls: { url: string; headers: Headers }[] = []
    vi.stubGlobal('fetch', (url: unknown, init?: RequestInit) => {
      calls.push({ url: String(url), headers: new Headers(init?.headers) })
      return model.fetch(url as string, init)
    })
    const runtime = await scriptedRuntime()
    const events: AssistantEvent[] = []
    const connection = openAssistantConnection({ allowed: scenario.scenarioTools, onEvent: (event) => events.push(event), transport: runtime.transport })
    try {
      const ready = await connection.ready
      await runAssistantSession(vercel, {
        prompt: scenario.policy,
        testPrompt: '',
        tools: ready.tools,
        callTool: ready.callTool,
        hostTools: [],
        model: { family: 'openai-compatible', model: 'scripted-model', call: bindModelCall('openai-compatible', CONNECTION) },
        thinking: normalize('off', 'openai-compatible'),
        critic: 'off',
        signal: new AbortController().signal
      }, (event) => events.push(event))
    } finally {
      await connection.close()
      await runtime.close()
    }
    expect(events.filter((event) => event.type === 'error')).toEqual([])
    expect(events.some((event) => event.type === 'end')).toBe(true)
    expect(calls.length).toBeGreaterThan(7)
    const session = await sessionBearer()
    for (const call of calls) {
      // The Desk's relay, on the Desk's chassis; the session's bearer and the connection's name are the Desk's own.
      expect(call.url).toContain('/api/assistant/relay/')
      expect(call.headers.get('Authorization')).toBe(`Bearer ${session}`)
      expect(call.headers.get('X-Assistant-Connection')).toBe(CONNECTION.connectionId)
      expect(call.headers.get('X-Assistant-Revision')).toBe(CONNECTION.connectionRevision)
    }
  })
})
