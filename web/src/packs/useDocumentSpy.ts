/**
 * Which member the reader is looking at.
 *
 * An `IntersectionObserver` rooted on `.desk-main`, which is the shell's one
 * scroll container — the document does not scroll, so the browser's own
 * fragment behaviour and a viewport-rooted observer would both watch the wrong
 * box.
 *
 * **Where there is no `IntersectionObserver`, the active entry follows the
 * `?at` selection instead of guessing.** jsdom has neither the observer nor
 * layout, so a spy that fell back to "the first entry" would report a member
 * nobody is looking at, and a test asserting that would be testing the
 * fallback's fiction. Following the selection is true in both worlds: a
 * selected member *is* the one being attended to.
 *
 * This is named in the PR body as not directly testable: what the tests hold
 * is the fallback, not the observing path.
 */
import { useEffect, useState } from 'react'

export function useDocumentSpy(
  pointers: readonly string[],
  selected: string | null
): string | null {
  const [seen, setSeen] = useState<string | null>(null)
  const key = pointers.join(' ')

  useEffect(() => {
    if (typeof IntersectionObserver === 'undefined') return
    const root = document.querySelector('.desk-main')
    const visible = new Set<string>()
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const pointer = entry.target.getAttribute('data-pointer')
          if (pointer === null) continue
          if (entry.isIntersecting) visible.add(pointer)
          else visible.delete(pointer)
        }
        // The first in document order that is on screen: a reader scrolling
        // down is reading the topmost visible member, not the largest one.
        const ordered = key.split(' ').find((pointer) => visible.has(pointer))
        setSeen(ordered ?? null)
      },
      { root: root instanceof HTMLElement ? root : null, rootMargin: '0px 0px -60% 0px' }
    )
    for (const pointer of key.split(' ')) {
      const element = document.getElementById(pointer)
      if (element !== null) observer.observe(element)
    }
    return () => observer.disconnect()
  }, [key])

  // The selection wins where there is one: a reader who has just clicked a
  // member is looking at that member whatever the scroll position says.
  return selected ?? seen
}
