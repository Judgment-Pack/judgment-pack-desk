/**
 * The reading document, against real files.
 *
 * The fixtures are read off disk rather than written inline, so what is
 * asserted is a document someone could have authored — including its member
 * order, which one of these cases is entirely about.
 */
import { cleanup, render, screen } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import type { PackDocument } from '../../mcp/types'
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
