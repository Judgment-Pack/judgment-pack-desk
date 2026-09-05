/**
 * The two exits an unsaved buffer can leave by, and neither covers the other.
 *
 * `beforeunload` is the browser's, and it fires only when the document itself
 * goes — a reload, a close, a link off the site. Everything inside this
 * application is same-document routing, which that event never sees: press
 * Back out of an editor, or follow any in-app link, and the component simply
 * unmounts with the buffer in it. So the router's blocker is the second guard.
 *
 * **The blocker's predicate is the pathname and nothing else.** That is the
 * whole reason edit mode is a search parameter: `?edit` and `?at` are the same
 * page, and a predicate that compared whole locations would ask "leave without
 * saving?" every time a viewer switched to Read, selected a member, or chose a
 * tab. A path segment for the mode would have made that unavoidable.
 *
 * Both guards were `AuthorView`'s. They are here so the pack editor holds the
 * same pair rather than a second spelling of it.
 */
import { useEffect } from 'react'
import { useBlocker } from 'react-router-dom'

export function useDirtyGuard(dirty: boolean, question: string): void {
  useEffect(() => {
    if (!dirty) return
    const warn = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

  const blocker = useBlocker(
    ({ currentLocation, nextLocation }) =>
      dirty && currentLocation.pathname !== nextLocation.pathname
  )
  useEffect(() => {
    if (blocker.state !== 'blocked') return
    if (window.confirm(question)) blocker.proceed()
    else blocker.reset()
  }, [blocker, question])
}
