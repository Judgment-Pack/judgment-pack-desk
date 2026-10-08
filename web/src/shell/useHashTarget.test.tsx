/**
 * Scrolling to the element a URL fragment names (`useHashTarget`): the
 * menus' links to `/help#shortcuts` and the rest move the page to the
 * section, which `createBrowserRouter` does not do itself. Since the Admin
 * restructure, Admin no longer scrolls to its sections (AdminView.test.tsx
 * holds that), so the hook is held here, on its own.
 */
import { cleanup, render } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import { useHashTarget } from './useHashTarget'

function Page({ when }: { when?: boolean }) {
  useHashTarget(when)
  return <><h2 id="first">First</h2><h2 id="shortcuts">Shortcuts</h2></>
}

function scrolledOn(path: string, when?: boolean): string[] {
  const scrolled: string[] = []
  const original = Element.prototype.scrollIntoView
  Element.prototype.scrollIntoView = function scrollIntoView(this: Element) { scrolled.push(this.id) }
  try {
    render(<MemoryRouter initialEntries={[path]}><Page when={when} /></MemoryRouter>)
  } finally {
    Element.prototype.scrollIntoView = original
  }
  return scrolled
}

afterEach(cleanup)

describe('scrolling to a fragment', () => {
  it('scrolls to the element the fragment names, percent-encoding read', () => {
    expect(scrolledOn('/help#shortcuts')).toEqual(['shortcuts'])
    cleanup()
    expect(scrolledOn('/help#%73hortcuts')).toEqual(['shortcuts'])
  })

  it('moves nothing for no fragment, an unknown one, one that is not percent-encoding, or where the page says not to', () => {
    for (const path of ['/help', '/help#', '/help#nowhere', '/help#%E0%A4%A']) {
      expect(scrolledOn(path), path).toEqual([])
      cleanup()
    }
    expect(scrolledOn('/help#shortcuts', false)).toEqual([])
  })
})
