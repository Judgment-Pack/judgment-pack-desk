import react from '@vitejs/plugin-react'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { defineConfig, type Plugin } from 'vite'

// The dev server proxies /launch, /ws and /api to the chassis so the page talks
// to a real `jpack mcp` — and to the real file API — while Vite handles hot
// reload. Start the chassis first:
//   go run . --dev-token dev --port 8791 <projectDir>
// then open http://localhost:5173/launch?secret=dev once: the chassis sets the
// session cookie for the dev origin and redirects to Vite's own `/`.
const CHASSIS = process.env.JPACK_DESK_CHASSIS ?? 'http://127.0.0.1:8791'

/**
 * `go:embed all:web/dist` fails to compile against a directory that does not
 * exist, so dist/.gitkeep is committed and a fresh clone builds before the SPA
 * has ever been built. emptyOutDir removes it on every build; this puts it
 * back, so the tree after a build is the tree that was committed plus assets.
 */
function keepEmbedDirectory(): Plugin {
  return {
    name: 'jpack-desk:keep-embed-directory',
    apply: 'build',
    closeBundle() {
      writeFileSync(resolve(import.meta.dirname, 'dist/.gitkeep'), '')
    }
  }
}

export default defineConfig({
  plugins: [react(), keepEmbedDirectory()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      // `changeOrigin: true` rewrites Host to the chassis' own, and the
      // browser's Origin is forwarded unchanged. That combination is what
      // actually puts the decision in front of the chassis' origin check: with
      // Host left as the dev server's, Origin and Host both name the dev server,
      // they match, and the request is accepted whether or not --dev-token was
      // given — which would make the documented requirement a fiction.
      //
      // With this shape the two differ, the check fires, and only --dev-token
      // permits the dev origin. `internal/desk` has a test modelling exactly
      // these headers, in both modes.
      // The launch exchange. Without it `http://localhost:5173/launch?secret=…`
      // is a client-side route Vite answers with index.html, no cookie is set
      // for the dev origin, and nothing under `npm run dev` is authorized.
      //
      // The redirect it answers with is `Location: /`, which is a *relative*
      // location and therefore resolves against the dev server the browser is
      // talking to rather than against the chassis — so the browser lands on
      // Vite's `/` holding a cookie scoped to `localhost:5173`.
      '/launch': {
        target: CHASSIS,
        changeOrigin: true
      },
      '/ws': {
        target: CHASSIS,
        ws: true,
        changeOrigin: true
      },
      // The authoring surface (issue #14) calls /api/* relatively. Without this
      // those calls hit the Vite dev server, which knows nothing about them, so
      // authoring simply does not work under `npm run dev`.
      '/api': {
        target: CHASSIS,
        changeOrigin: true
      }
    }
  },
  build: {
    // The Go chassis embeds this directory with go:embed.
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: true
  }
})
