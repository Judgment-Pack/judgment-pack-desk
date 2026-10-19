// The harness package's certification suite, run on the Desk's own wiring.
//
// `@judgment-pack/harness` certifies its adapters in its own repository against
// a stand-in host (`testing/host.ts`). It does not ship its test files, so a
// change to one of the Desk's injection points (`src/assistant/harnessHost.ts`:
// headers, bearer, transport, `endpointCapabilities`, `modelsFor`) or a bump of
// the pin was never certified against the Desk's wiring. This config runs the
// suite's own test files, at the revision the component lock pins, from the
// Desk's `web/` on the Desk's test ground (`src/testing/setup.ts` imports
// `harnessHost.ts` before every test), so every injection point is the one the
// Desk's `configureHarness` call supplies and none is the package's stand-in.
//
// **The package is aliased to its source**, so the suite's files and the Desk's
// wiring share one host module: through the installed `dist/` they would be two
// copies, and the suite would run on the stand-in the Desk never replaced.
//
// `HARNESS_SOURCE` names the package directory (`packages/harness`) of a
// checkout of the pinned revision. `scripts/certify-harness.mjs` is what runs
// this, one adapter at a time, and CI checks the revision out of the lock.
import { resolve } from 'node:path'
import { defineConfig } from 'vitest/config'

const web = import.meta.dirname
const named = process.env.HARNESS_SOURCE
if (named === undefined || named === '') {
  throw new Error('HARNESS_SOURCE names the harness package\'s source (packages/harness of a checkout at the revision the component lock pins)')
}
const pkg = resolve(web, named)

export default defineConfig({
  root: web,
  resolve: {
    alias: [
      { find: /^@judgment-pack\/harness\/contract$/, replacement: pkg + '/entries/contract.ts' },
      { find: /^@judgment-pack\/harness\/vercel$/, replacement: pkg + '/engines/vercel/index.ts' },
      { find: /^@judgment-pack\/harness\/codex$/, replacement: pkg + '/engines/codex/index.ts' },
      { find: /^@judgment-pack\/harness\/certification$/, replacement: pkg + '/certification/index.ts' },
      { find: /^@judgment-pack\/harness\/(.*)$/, replacement: pkg + '/$1' }
    ],
    dedupe: ['vitest', '@modelcontextprotocol/sdk', 'ai', '@ai-sdk/anthropic', '@ai-sdk/google', '@ai-sdk/openai-compatible', 'zod']
  },
  server: { fs: { allow: [web, pkg, resolve(pkg, '../..')] } },
  test: {
    environment: 'jsdom',
    dir: pkg,
    setupFiles: [resolve(web, 'src/testing/setup.ts'), resolve(web, 'vitest.certification.setup.ts')],
    include: ['certification/**/*.test.ts', 'engines/codex/codex.test.ts'],
    testTimeout: 30_000
  }
})
