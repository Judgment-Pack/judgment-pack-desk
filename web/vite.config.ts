import react from '@vitejs/plugin-react'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { defineConfig, type Plugin } from 'vite'

// The dev server proxies /ws to the chassis so the page talks to a real
// `jpack mcp` while Vite handles hot reload. Start the chassis first:
//   go run . --dev-token dev --port 8791 <projectDir>
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
      '/ws': {
        target: CHASSIS,
        ws: true,
        // Keep the browser's Origin so the chassis makes the origin decision
        // itself; --dev-token is what permits the dev server's origin.
        changeOrigin: false
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
