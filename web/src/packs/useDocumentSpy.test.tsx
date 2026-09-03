/**
 * Which member the outline marks, and which of the two answers wins.
 *
 * The spy has two sources — what an `IntersectionObserver` reports is on
 * screen, and what `?at` says the reader picked — and the order between them is
 * the whole behaviour. It was "selection first", and because `?at` persists for
 * as long as the Inspector is looking at something, the observer could never
 * win again: an outline that had followed the page stopped following it the
 * moment anything was selected, for the rest of the visit.
 */
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useDocumentSpy } from './useDocumentSpy'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

/** An observer a test drives, standing in for a viewport nobody scrolls. */
function observable() {
  const callbacks: ((entries: { target: Element; isIntersecting: boolean }[]) => void)[] = []
  class Stub {
    constructor(callback: (entries: { target: Element; isIntersecting: boolean }[]) => void) {
      callbacks.push(callback)
    }
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  vi.stubGlobal('IntersectionObserver', Stub)
  return {
    /** Report exactly these pointers as on screen. */
    show(...pointers: string[]) {
      act(() => {
        for (const callback of callbacks) {
          callback(
            pointers.map((pointer) => ({
              target: { getAttribute: () => pointer } as unknown as Element,
              isIntersecting: true
            }))
          )
        }
      })
    },
    hide(...pointers: string[]) {
      act(() => {
        for (const callback of callbacks) {
          callback(
            pointers.map((pointer) => ({
              target: { getAttribute: () => pointer } as unknown as Element,
              isIntersecting: false
            }))
          )
        }
      })
    }
  }
}

const POINTERS = ['/title', '/decision', '/rules', '/sources']

function Spy({ pointers, selected }: { pointers: string[]; selected: string | null }) {
  const active = useDocumentSpy(pointers, selected)
  return <p data-testid="active">{active ?? 'none'}</p>
}

const active = () => screen.getByTestId('active').textContent

describe('the outline follows the page', () => {
  it('marks what the reader has scrolled to, even with a selection standing', () => {
    // `?at` persists, so "selection first" meant the observer's answer was
    // never seen again: a reader who picked `/rules/0` and scrolled to
    // `/sources` watched the outline keep marking rules for the rest of the
    // visit.
    const viewport = observable()
    render(<Spy pointers={POINTERS} selected="/rules/0" />)
    expect(active()).toBe('/rules')

    viewport.show('/sources')
    expect(active()).toBe('/sources')
  })

  it('falls back to the selection before anything has been observed', () => {
    // The case that matters on arrival: a link with `?at` marks its member
    // before the reader has scrolled at all.
    observable()
    render(<Spy pointers={POINTERS} selected="/decision" />)
    expect(active()).toBe('/decision')
  })

  it('resolves a selection under a listed unit to that unit', () => {
    observable()
    render(<Spy pointers={POINTERS} selected="/rules/2/when" />)
    expect(active()).toBe('/rules')
  })

  it('marks the topmost member on screen, not the last one reported', () => {
    const viewport = observable()
    render(<Spy pointers={POINTERS} selected={null} />)
    viewport.show('/sources', '/decision')
    // Document order decides: a reader scrolling down is reading the topmost
    // visible member, not the largest or the most recently reported.
    expect(active()).toBe('/decision')
  })

  it('goes back to the selection when nothing is on screen', () => {
    const viewport = observable()
    render(<Spy pointers={POINTERS} selected="/title" />)
    viewport.show('/sources')
    expect(active()).toBe('/sources')
    viewport.hide('/sources')
    expect(active()).toBe('/title')
  })

  it('forgets what it saw when the document changes', () => {
    // An answer carried over from the last document would mark a unit that is
    // no longer on the page.
    const viewport = observable()
    const { rerender } = render(<Spy pointers={POINTERS} selected={null} />)
    viewport.show('/sources')
    expect(active()).toBe('/sources')

    act(() => {
      rerender(<Spy pointers={['/title', '/decision']} selected={null} />)
    })
    expect(active()).toBe('none')
  })
})
