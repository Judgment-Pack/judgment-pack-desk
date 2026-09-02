/**
 * Every icon, on the same geometry and none of them an accessible name.
 *
 * The test iterates the module's own exports rather than a list written beside
 * it, so an icon added by pasting markup out of a drawing tool — with its own
 * viewBox, its own stroke weight, or a `<title>` that would silently become
 * some control's accessible name — fails here instead of shipping.
 */
import { cleanup, render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import * as icons from './icons'

afterEach(cleanup)

const exported = Object.entries(icons) as [string, () => ReactElement][]

describe('the desk icons', () => {
  it('exports the glyph vocabulary the shell names', () => {
    expect(exported.map(([name]) => name).sort()).toEqual([
      'IconChevronDown',
      'IconChevronLeft',
      'IconChevronRight',
      'IconChevronUp',
      'IconClose',
      'IconCopy',
      'IconGear',
      'IconGraph',
      'IconHelp',
      'IconMatrix',
      'IconPack',
      'IconPanelBottom',
      'IconPanelRight',
      'IconPencil',
      'IconPlus'
    ])
  })

  it.each(exported)('draws %s on the shared 16px grid, hidden and untitled', (_name, Icon) => {
    const { container } = render(<Icon />)
    const svgs = container.querySelectorAll('svg')
    expect(svgs.length).toBe(1)
    const svg = svgs[0]
    expect(svg.getAttribute('aria-hidden')).toBe('true')
    expect(svg.getAttribute('viewBox')).toBe('0 0 16 16')
    expect(svg.getAttribute('stroke-width')).toBe('1.75')
    expect(svg.getAttribute('stroke-linecap')).toBe('round')
    expect(svg.getAttribute('stroke-linejoin')).toBe('round')
    expect(svg.getAttribute('stroke')).toBe('currentColor')
    // An icon is never the name of anything here.
    expect(svg.querySelector('title')).toBeNull()
    expect(svg.getAttribute('role')).toBeNull()
  })
})
