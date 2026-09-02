import { defineConfig } from 'vitest/config'

// Component tests run against fixture payloads shaped like the wire's own, so
// a rendering rule — never compare display values, never paint a verdict the
// runtime did not state — fails a test rather than surviving as a habit.
export default defineConfig({
  test: {
    environment: 'jsdom',
    setupFiles: ['src/testing/setup.ts'],
    include: ['src/**/*.test.tsx', 'src/**/*.test.ts']
  }
})
