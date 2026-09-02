/**
 * The ground every test runs on, registered as vitest's one `setupFiles`.
 *
 * **Radix does not need this.** Every primitive the shell uses — `Dialog`,
 * `DropdownMenu`, `Collapsible`, `Tabs`, `Toggle`, `Separator`, `Tooltip`,
 * `Avatar`, `VisuallyHidden` — renders and operates under jsdom 30 with no
 * shim at all; that was checked before this file was written, and saying
 * otherwise here would leave the next reader believing a dependency exists
 * that does not. What this file buys is *determinism*: a component that grows
 * a `ResizeObserver` or a `matchMedia` read should fail on the assertion it
 * broke, not on a missing global three frames earlier, and a shell whose
 * breakpoint hook reads `matchMedia` should read the same answer in every
 * file rather than whichever answer the environment happens to hold.
 *
 * Every install is guarded, so a real implementation — a future jsdom, a
 * browser-mode run — is never overwritten by a stub of it.
 */

/** A no-op observer: nothing under test asserts on an observed resize. */
if (!('ResizeObserver' in globalThis)) {
  class ResizeObserverStub {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  }
  ;(globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = ResizeObserverStub
}

/**
 * Reached through an untyped alias, deliberately. Written against the real
 * types, `'matchMedia' in window` narrows the *else* branch to `never` and
 * the assignment stops compiling — the guard is exactly the thing the type
 * says cannot happen, and the whole point of a shim is that it can.
 */
const untypedWindow = globalThis.window as unknown as Record<string, unknown>
const untypedElement = Element.prototype as unknown as Record<string, unknown>

/**
 * `matches: false` for every query, which puts every test at the widest
 * breakpoint — the shell's three-pane layout — unless the test says otherwise.
 * A test that wants a narrow viewport overrides this with its own stub, and
 * the override is then visible in that test rather than implied by a default.
 */
if (typeof untypedWindow.matchMedia !== 'function') {
  untypedWindow.matchMedia = (query: string) => ({
    media: query,
    matches: false,
    onchange: null,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent: () => false
  })
}

/** Pointer capture and `scrollIntoView`: present in browsers, absent in jsdom. */
if (typeof untypedElement.hasPointerCapture !== 'function') {
  untypedElement.hasPointerCapture = () => false
}
if (typeof untypedElement.setPointerCapture !== 'function') {
  untypedElement.setPointerCapture = () => {}
}
if (typeof untypedElement.releasePointerCapture !== 'function') {
  untypedElement.releasePointerCapture = () => {}
}
if (typeof untypedElement.scrollIntoView !== 'function') {
  untypedElement.scrollIntoView = () => {}
}

/**
 * **A default `fetch` that refuses rather than dials.**
 *
 * The shell reads `jpack-desk.json` through the chassis file API on every
 * render of the desk, and `testing/harness.tsx` stubs no `fetch`. Without a
 * default, every such render opens a real socket to jsdom's own origin — slow,
 * flaky, and dependent on nothing being listening. This makes the absence of a
 * stub an immediate, named rejection instead, which the config query is
 * required to survive (it fails closed to the built-in defaults).
 *
 * A file that wants a real answer overrides it with `vi.stubGlobal('fetch', …)`,
 * exactly as `AuthorView.test.tsx` already does.
 */
if (!('__jpackDeskDefaultFetch' in globalThis)) {
  ;(globalThis as unknown as { __jpackDeskDefaultFetch: boolean }).__jpackDeskDefaultFetch = true
  globalThis.fetch = (() =>
    Promise.reject(
      new Error('no fetch stub in this test — see src/testing/setup.ts')
    )) as unknown as typeof fetch
}
