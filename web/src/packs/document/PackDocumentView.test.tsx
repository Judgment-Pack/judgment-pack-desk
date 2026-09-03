/**
 * The reading document, against real files.
 *
 * The fixtures are read off disk rather than written inline, so what is
 * asserted is a document someone could have authored — including its member
 * order, which one of these cases is entirely about.
 */
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import type { PackDocument } from '../../mcp/types'
import { SelectionContext } from './Block'
import { PackDocumentView } from './PackDocumentView'

afterEach(cleanup)

const FIXTURES = join(import.meta.dirname, '..', '__fixtures__')
const load = (name: string) =>
  JSON.parse(readFileSync(join(FIXTURES, name), 'utf8')) as PackDocument

const full = load('full.pack.json')
const minimal = load('minimal.pack.json')
const reordered = load('reordered.pack.json')

function draw(document: PackDocument) {
  return render(
    <MemoryRouter>
      <PackDocumentView document={document} active={null} />
    </MemoryRouter>
  )
}

/** Every block's pointer, in the order the document renders them. */
function pointers(container: HTMLElement): string[] {
  return [...container.querySelectorAll('[data-pointer]')].map(
    (element) => element.getAttribute('data-pointer')!
  )
}

describe('the full document', () => {
  it('renders every member the document declares', () => {
    const { container } = draw(full)
    const found = new Set(pointers(container))
    for (const pointer of [
      '/title',
      '/decision',
      '/applicability',
      '/evidenceRequirements',
      '/sources',
      '/outcomes',
      '/rules',
      '/exceptions',
      '/fallbackOutcome',
      '/escalation',
      '/metadata',
      '/extensions'
    ]) {
      expect(found.has(pointer), pointer).toBe(true)
    }
    expect(screen.getByRole('heading', { level: 1 }).textContent).toBe(full.decision.question)
  })

  it('renders metadata.reviews, which the view this replaces dropped', () => {
    draw(full)
    expect(screen.getByText('a named reviewer')).toBeTruthy()
    expect(screen.getByText('approved')).toBeTruthy()
    expect(screen.getByText('a second reviewer')).toBeTruthy()
    // Rendered, never written: this surface has no reviewer identity.
    expect(screen.queryByRole('button', { name: /review/i })).toBeNull()
  })

  it('renders an extensions block at the root and inside a rule', () => {
    const { container } = draw(full)
    const found = new Set(pointers(container))
    expect(found.has('/extensions')).toBe(true)
    expect(found.has('/rules/1/extensions')).toBe(true)
    expect(found.has('/decision/extensions')).toBe(true)
    expect(screen.getByText('example.pipeline')).toBeTruthy()
    // Twice: once as the rule's own extension, once as a name
    // `metadata.requiredExtensions` lists.
    expect(screen.getAllByText('example.review-window')).toHaveLength(2)
  })

  it('gives every block a data-pointer and a matching id', () => {
    const { container } = draw(full)
    for (const element of container.querySelectorAll('[data-pointer]')) {
      const pointer = element.getAttribute('data-pointer')!
      if (pointer === '') {
        // The document's own pointer is the empty string, which is not a legal
        // id, so the root carries the attribute and no id.
        expect(element.id).toBe('')
        continue
      }
      expect(element.id, pointer).toBe(pointer)
      expect(document.getElementById(pointer)).toBeTruthy()
    }
  })

  it('gives each pointer exactly one element', () => {
    // One address, one element. `applicability` used to render two — the
    // member's own wrapper and the root row of the condition tree inside it —
    // so the document carried a duplicate id, `getElementById` answered by
    // tree order, and one selection put `aria-current` on both. The assertion
    // above cannot see that: both duplicates have an id equal to their own
    // pointer.
    const { container } = draw(full)
    const all = pointers(container)
    const seen = new Map<string, number>()
    for (const pointer of all) seen.set(pointer, (seen.get(pointer) ?? 0) + 1)
    expect([...seen].filter(([, count]) => count > 1)).toEqual([])
    expect(seen.has('/applicability')).toBe(true)
  })

  it('tags the fallback outcome from the document’s own member', () => {
    draw(full)
    expect(screen.getByText('fallback')).toBeTruthy()
  })
})

describe('a document whose members are not in the schema’s order', () => {
  it('renders the present ones in the document’s own order', () => {
    // `rules` before `outcomes` is how someone wrote this file, and a page
    // that re-sorted would be showing a document nobody wrote.
    const { container } = draw(reordered)
    const order = pointers(container).filter((pointer) =>
      ['/rules', '/outcomes', '/title', '/decision'].includes(pointer)
    )
    expect(order).toEqual(['/rules', '/outcomes', '/title', '/decision'])
  })

  it('splices each omitted member’s line in at its canonical position', () => {
    const { container } = draw(minimal)
    const order = pointers(container).filter((pointer) =>
      ['/decision', '/applicability', '/evidenceRequirements'].includes(pointer)
    )
    expect(order).toEqual(['/decision', '/applicability', '/evidenceRequirements'])
  })
})

describe('the minimal document', () => {
  it('states each omitted optional member rather than rendering nothing', () => {
    const { container } = draw(minimal)
    const found = new Set(pointers(container))
    for (const pointer of [
      '/applicability',
      '/evidenceRequirements',
      '/sources',
      '/exceptions',
      '/fallbackOutcome',
      '/escalation',
      '/metadata',
      '/extensions'
    ]) {
      expect(found.has(pointer), pointer).toBe(true)
    }
    // Eight optional members, eight statements of absence. The view this
    // replaces rendered nothing at all for any of them.
    expect(screen.getAllByText('not declared')).toHaveLength(8)
  })

  it('marks the same members “not declared” in the outline', () => {
    draw(minimal)
    const outline = screen.getByRole('navigation', { name: 'Members' })
    expect(outline.textContent).toContain('Applicability — not declared')
    expect(outline.textContent).toContain('Escalation — not declared')
    // A present member is a link; an absent one is not, because a link to a
    // block that is not there is a link that does nothing.
    expect(screen.getByRole('link', { name: /Rules/ })).toBeTruthy()
    expect(outline.querySelectorAll('a')).toHaveLength(4)
  })

  it('counts the lists it lists', () => {
    draw(full)
    const outline = screen.getByRole('navigation', { name: 'Members' })
    expect(outline.textContent).toContain('Outcomes 2')
    expect(outline.textContent).toContain('Rules 2')
    expect(outline.textContent).toContain('Exceptions 1')
  })
})

describe('reaching a member without a mouse', () => {
  /** The document, with the selection captured. */
  function drive(at: string | null = null) {
    const chosen: string[] = []
    const view = render(
      <MemoryRouter>
        <SelectionContext.Provider value={{ at, select: (pointer) => chosen.push(pointer) }}>
          <PackDocumentView document={full} active={null} />
        </SelectionContext.Provider>
      </MemoryRouter>
    )
    const blocks = [...view.container.querySelectorAll<HTMLElement>('[data-pointer]')].filter(
      (element) => element.getAttribute('data-pointer') !== ''
    )
    return { chosen, blocks, ...view }
  }

  const stops = (blocks: HTMLElement[]) =>
    blocks.filter((element) => element.getAttribute('tabindex') === '0')

  it('is one tab stop and not ninety-seven', () => {
    // Every block used to carry `tabIndex={-1}` and a click handler, so the
    // only keyboard route into `?at` was the outline — twelve member units and
    // nothing under them.
    const { blocks } = drive()
    expect(blocks.length).toBeGreaterThan(50)
    expect(stops(blocks)).toHaveLength(1)
    expect(stops(blocks)[0]!.getAttribute('data-pointer')).toBe('/title')
  })

  it('follows the selection, so a deep link is where Tab comes back to', () => {
    const { blocks } = drive('/rules/0')
    expect(stops(blocks)[0]!.getAttribute('data-pointer')).toBe('/rules/0')
  })

  it('moves the stop and the focus with the arrow keys', () => {
    const { blocks } = drive()
    const first = blocks[0]!
    first.focus()
    fireEvent.keyDown(first, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(blocks[1])
    expect(stops(blocks).map((element) => element.getAttribute('data-pointer'))).toEqual([
      blocks[1]!.getAttribute('data-pointer')
    ])

    fireEvent.keyDown(blocks[1]!, { key: 'ArrowUp' })
    expect(document.activeElement).toBe(first)
  })

  it('reaches the ends with Home and End', () => {
    const { blocks } = drive()
    blocks[0]!.focus()
    fireEvent.keyDown(blocks[0]!, { key: 'End' })
    expect(document.activeElement).toBe(blocks[blocks.length - 1])
    fireEvent.keyDown(blocks[blocks.length - 1]!, { key: 'Home' })
    expect(document.activeElement).toBe(blocks[0])
  })

  it('selects the block under the stop with Enter and with Space', () => {
    const { chosen, blocks } = drive()
    const operand = blocks.find(
      (element) => element.getAttribute('data-pointer') === '/rules/1/when/conditions/0/value'
    )!
    operand.focus()
    fireEvent.keyDown(operand, { key: 'Enter' })
    fireEvent.keyDown(operand, { key: ' ' })
    expect(chosen).toEqual([
      '/rules/1/when/conditions/0/value',
      '/rules/1/when/conditions/0/value'
    ])
  })

  it('leaves the outline’s own links alone', () => {
    // Enter on a link is the link's. Arrowing out of one would be a surprise.
    const { chosen } = drive()
    const link = screen.getAllByRole('link')[0]!
    fireEvent.keyDown(link, { key: 'Enter' })
    fireEvent.keyDown(link, { key: 'ArrowDown' })
    expect(chosen).toEqual([])
  })
})
