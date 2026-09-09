/**
 * Scroll to the element a URL fragment names.
 *
 * The rail's Admin menu and the user menu link to `/admin#storage`,
 * `/admin#organization`, `/help#shortcuts` and the rest, and
 * `createBrowserRouter` performs no fragment scrolling at all — so without this
 * the menus changed the address bar and moved nothing, which reads as a broken
 * menu rather than as a missing feature.
 *
 * The browser's own fragment scrolling is not a substitute, and the reason is
 * narrower than it was once written here: it happens on a **full load** and not
 * on the client-side navigation a menu performs. It does reach `.desk-main` —
 * the browser scrolls the nearest scrollable ancestor, not only the document —
 * which is measurable in the drive on a hard load of `/admin#assistant`, so a
 * page that wants the top of itself after such a load has to ask for it.
 *
 * It runs on the fragment and on nothing else, so a route that re-renders does
 * not drag the reader back to a heading they have scrolled away from. An
 * unknown fragment is left alone: the page it names is already open, and
 * jumping somewhere else would be worse than staying put.
 *
 * **`when` exists because a fragment can select rather than address.** Admin's
 * fragment opens a section, and on a wide shell that section is rendered
 * *beside* the list rather than below it — so scrolling its heading to the top
 * of `.desk-main` took the page's own heading, its status line and the top of
 * the list with it, because all three share one scroll container. Where the
 * thing a fragment names is already at the top of its column there is nothing
 * to scroll to, and the page says so rather than scrolling anyway. Below that
 * shell's breakpoint the section is under the list and the scroll is exactly
 * what a reader needs, so the answer is a condition and not a deletion.
 */
import { useEffect } from 'react'
import { useLocation } from 'react-router-dom'

export function useHashTarget(when = true): void {
  const { hash } = useLocation()
  useEffect(() => {
    if (!when) return
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
  }, [hash, when])
}
