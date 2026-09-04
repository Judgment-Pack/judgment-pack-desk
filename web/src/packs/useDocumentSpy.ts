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
 * **The selection is resolved to the outline's own unit before it is
 * preferred.** The outline addresses twelve member units; `?at` addresses every
 * block in the document, which is ninety-odd. Returning the selection verbatim
 * meant that selecting a rule card, a chip, a condition operand — or following
 * `#/rules/0`, which is the ordinary way in — returned a pointer no outline
 * entry carries, so no entry was marked *and* the observer's answer was thrown
 * away for the rest of the visit. `/rules/0` is a reader looking at Rules.
 *
 * This is named in the PR body as not directly testable: what the tests hold
 * is the fallback, not the observing path.
 */
import { useEffect, useState } from 'react'
import { parentPointers } from './pointers'

export function useDocumentSpy(
  pointers: readonly string[],
  selected: string | null,
  /**
   * What makes this document *this* document — its raw bytes, or any key that
   * changes when the bytes do.
   *
   * **The pointer list is not that key.** A refetch that returns a different
   * revision with the same top-level members leaves `pointers.join(' ')`
   * identical, so neither effect below re-ran: the answer from the old document
   * stood, and the observer kept watching element objects the render had
   * already replaced — it was reporting on nodes that are not on the page.
   */
  revision: string
): string | null {
  const [seen, setSeen] = useState<string | null>(null)
  const key = pointers.join(' ')

  // A different document, or a different set of listed units, is a different
  // question — and an answer carried over from the last one would mark a unit
  // that is no longer on the page. Reset before observing, not after.
  useEffect(() => {
    setSeen(null)
  }, [key, revision])

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
  }, [key, revision])

  /**
   * **What is on screen wins; the selection is the fallback.**
   *
   * This was the other way round, and it made the outline stop following the
   * page. `?at` persists — it is how the Inspector knows what it is looking at,
   * and it survives every scroll — so a selection, once made, answered for ever
   * and no observer update could ever be seen. A reader who picked `/rules/0`
   * and then scrolled to `/sources` watched the outline keep marking rules.
   *
   * The selection still answers before anything has been observed, which is the
   * case that matters on arrival: a link with `?at` marks its member before the
   * reader has scrolled at all.
   */
  const inOutline = selected === null ? undefined : outlineUnitFor(pointers, selected)
  return seen ?? inOutline ?? null
}

/** The listed unit a pointer sits at or under, or undefined. */
function outlineUnitFor(pointers: readonly string[], selected: string): string | undefined {
  if (pointers.includes(selected)) return selected
  for (const ancestor of parentPointers(selected)) {
    if (pointers.includes(ancestor)) return ancestor
  }
  return undefined
}
