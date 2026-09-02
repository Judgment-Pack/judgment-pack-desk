/**
 * One media query, subscribed.
 *
 * The guard is in *production* code, not only in the test setup: a shell that
 * threw where `matchMedia` is absent would make the test shim a dependency
 * rather than a convenience, and the two are different claims about what the
 * page needs to run.
 *
 * `useSyncExternalStore` rather than an effect and a state, because StrictMode
 * double-subscribes and this way the second subscription is free — and because
 * the first paint reads the current value rather than the default and then
 * correcting itself.
 */
import { useCallback, useSyncExternalStore } from 'react'

/** Below this the rail is an overlay drawer rather than a column. */
export const RAIL_DRAWER_BELOW = '(max-width: 899px)'
/** Below this the inspector is an overlay drawer rather than a column. */
export const INSPECTOR_DRAWER_BELOW = '(max-width: 1099px)'

function hasMatchMedia(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function'
}

export function useMediaQuery(query: string): boolean {
  const subscribe = useCallback(
    (onChange: () => void) => {
      if (!hasMatchMedia()) return () => {}
      const list = window.matchMedia(query)
      // `addListener` is the pre-2019 spelling, still the only one some
      // engines carry. Both are tried rather than one assumed.
      if (typeof list.addEventListener === 'function') {
        list.addEventListener('change', onChange)
        return () => list.removeEventListener('change', onChange)
      }
      const legacy = list as unknown as {
        addListener?: (handler: () => void) => void
        removeListener?: (handler: () => void) => void
      }
      legacy.addListener?.(onChange)
      return () => legacy.removeListener?.(onChange)
    },
    [query]
  )
  const read = useCallback(() => (hasMatchMedia() ? window.matchMedia(query).matches : false), [query])
  // The server snapshot is the same answer: there is no server, and a desk
  // rendered without a window is at the widest breakpoint by definition.
  return useSyncExternalStore(subscribe, read, () => false)
}
