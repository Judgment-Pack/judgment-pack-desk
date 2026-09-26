import { describe, expect, it } from 'vitest'
import config from '../../vite.config'

/**
 * What the dev server is configured to do — asserted against the exported
 * config, not against a running Vite.
 *
 * **What this proves and what it does not.** It proves the required proxy entries
 * exist and carry `changeOrigin`, so deleting `/api` or flipping
 * `changeOrigin` fails here rather than in someone's afternoon. It does *not*
 * prove Vite honours them, or what headers actually arrive: that would need a
 * real dev server and a capture backend, which is a great deal of flake for a
 * property Vite already tests. The chassis side of the same arrangement is
 * covered by `TestViteProxyShapeNeedsDevMode`, which sends the header shape
 * this config produces.
 */
describe('the dev server proxy', () => {
  // The config is a plain object here; the cast is to read it without pulling
  // Vite's full UserConfig type into a test that only wants two members.
  const proxy =
    (config as unknown as { server?: { proxy?: Record<string, unknown> } }).server?.proxy ?? {}

  it('proxies launch, both socket transports and the file API', () => {
    // `/api` is the authoring surface. Without it those calls hit the Vite dev
    // server, which knows nothing about them, and authoring simply does not
    // work under `npm run dev`.
    //
    // `/launch` is how a session is acquired at all. Without it the dev origin
    // has no handoff, and every one of the other two answers 401 — which is the
    // whole of `npm run dev` not working.
    expect(Object.keys(proxy).sort()).toEqual(['/api', '/api/agent/run', '/launch', '/ws'])
  })

  for (const route of ['/launch', '/ws', '/api/agent/run', '/api']) {
    it(`rewrites Host on ${route} so the chassis' origin check can decide`, () => {
      // With Host left as the dev server's, Origin and Host both name the dev
      // server, they match, and the request is accepted whether or not
      // --dev-token was given — which would make the documented requirement a
      // fiction the check could never enforce. `/launch` is proxied the same
      // way for consistency of shape, though it is not itself gated.
      expect((proxy[route] as { changeOrigin?: boolean }).changeOrigin).toBe(true)
    })
  }

  it('carries both WebSocket upgrades, with the agent route before the HTTP prefix', () => {
    expect((proxy['/ws'] as { ws?: boolean }).ws).toBe(true)
    expect((proxy['/api/agent/run'] as { ws?: boolean }).ws).toBe(true)
    expect(Object.keys(proxy).indexOf('/api/agent/run')).toBeLessThan(Object.keys(proxy).indexOf('/api'))
  })
})
