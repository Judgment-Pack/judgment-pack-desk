import { describe, expect, it } from 'vitest'
import config from '../../vite.config'

/**
 * What the dev server is configured to do — asserted against the exported
 * config, not against a running Vite.
 *
 * **What this proves and what it does not.** It proves the two proxy entries
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

  it('proxies the relay and the file API, and no less', () => {
    // `/api` is the authoring surface. Without it those calls hit the Vite dev
    // server, which knows nothing about them, and authoring simply does not
    // work under `npm run dev`.
    expect(Object.keys(proxy).sort()).toEqual(['/api', '/ws'])
  })

  for (const route of ['/ws', '/api']) {
    it(`rewrites Host on ${route} so the chassis' origin check can decide`, () => {
      // With Host left as the dev server's, Origin and Host both name the dev
      // server, they match, and the request is accepted whether or not
      // --dev-token was given — which would make the documented requirement a
      // fiction the check could never enforce.
      expect((proxy[route] as { changeOrigin?: boolean }).changeOrigin).toBe(true)
    })
  }

  it('carries the WebSocket upgrade on the relay route', () => {
    expect((proxy['/ws'] as { ws?: boolean }).ws).toBe(true)
  })
})
