import { defineConfig } from 'vitest/config'

// Component tests run against fixture payloads shaped like the wire's own, so
// a rendering rule — never compare display values, never paint a verdict the
// runtime did not state — fails a test rather than surviving as a habit.
export default defineConfig({
  test: {
    environment: 'jsdom',
    setupFiles: ['src/testing/setup.ts'],
    include: ['src/**/*.test.tsx', 'src/**/*.test.ts'],
    /**
     * Vitest's default is 5s, chosen for unit tests.
     *
     * Seventy files run in parallel, one per core, and a good half of them
     * stand the **whole page** up — the router, the query client, the shell
     * slot, a chassis stub and a pack document with ninety-odd blocks in it.
     * Each of those takes about a second alone and several seconds under that
     * contention, so the route cases sat on the ceiling: the suite passed or
     * failed depending on what else the machine was doing, and a mutation run
     * that reads a flaky baseline reports every row as meaningless. Raising
     * the ceiling costs nothing on a passing run — only a failing test waits
     * — and a mutation that hangs a render is still caught, by the harness's
     * own bound on the whole suite.
     */
    testTimeout: 20_000
  }
})
