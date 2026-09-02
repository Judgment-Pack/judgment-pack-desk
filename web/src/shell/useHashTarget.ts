/**
 * Scroll to the element a URL fragment names.
 *
 * The rail's Admin menu and the user menu link to `/admin#runtime`,
 * `/admin#panes`, `/help#shortcuts` and the rest. Nothing does this for them:
 * `createBrowserRouter` performs no fragment scrolling, and the browser's own
 * would not help either, because the shell's scroll container is `.desk-main`
 * and not the document. Without this the menus changed the address bar and
 * moved nothing, which reads as a broken menu rather than as a missing feature.
 *
 * It runs on the fragment and on nothing else, so a route that re-renders does
 * not drag the reader back to a heading they have scrolled away from. An
 * unknown fragment is left alone: the page it names is already open, and
 * jumping somewhere else would be worse than staying put.
 */
import { useEffect } from 'react'
import { useLocation } from 'react-router-dom'

export function useHashTarget(): void {
  const { hash } = useLocation()
  useEffect(() => {
    if (hash.length < 2) return
    let id: string
    try {
      id = decodeURIComponent(hash.slice(1))
    } catch {
      // A fragment that is not valid percent-encoding names nothing.
      return
    }
    const target = document.getElementById(id)
    target?.scrollIntoView()
  }, [hash])
}
