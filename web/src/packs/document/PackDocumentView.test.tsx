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

/**
 * The schema's root order and its required list, written out here rather than
 * imported from `members.ts`.
 *
 * A test that imports the list the view sorts by is a restatement of the view,
 * and would agree with it however wrong both were. These two arrays are copied
 * from `internal/artifacts/jps/0.2.0-draft/schema.json` — `properties` order
 * and root `required` — and they are the thing the page is being held to.
 */
const SCHEMA_ORDER = [
  'specVersion',
  'id',
  'version',
  'title',
  'description',
  'decision',
  'applicability',
  'evidenceRequirements',
  'sources',
  'outcomes',
  'rules',
  'exceptions',
  'fallbackOutcome',
  'escalation',
  'metadata',
  'extensions'
]
const REQUIRED = ['specVersion', 'id', 'version', 'title', 'decision', 'outcomes', 'rules']
const OPTIONAL = SCHEMA_ORDER.filter((member) => !REQUIRED.includes(member))

/**
 * The complete top-level sequence this document should draw: its own members in
 * its own order, with each omitted **optional** member spliced in after the
 * nearest earlier member the schema puts before it, and a missing required
 * member drawn not at all.
 */
function expectedTopLevel(document: PackDocument): string[] {
  const declared = Object.keys(document as unknown as Record<string, unknown>)
  const order = declared.filter((key) => SCHEMA_ORDER.includes(key)).map((key) => `/${key}`)
  for (const [index, member] of SCHEMA_ORDER.entries()) {
    if (declared.includes(member) || REQUIRED.includes(member)) continue
    let anchor = -1
    for (let earlier = index - 1; earlier >= 0; earlier -= 1) {
      const at = order.indexOf(`/${SCHEMA_ORDER[earlier]}`)
      if (at >= 0) {
        anchor = at
        break
      }
    }
    order.splice(anchor + 1, 0, `/${member}`)
  }
  return order
}

/** Every top-level pointer the page drew, in the order it drew them. */
function topLevel(container: HTMLElement): string[] {
  return pointers(container).filter((pointer) => /^\/[^/]+$/.test(pointer))
}

describe('the order the page draws', () => {
  // **One comparison, and nothing filtered out of the actual output.** There
  // were three assertions here and each looked away from something: the first
  // removed every omission before comparing, the second only checked
  // containment, and the third named three pointers by hand. Moving every
  // omission line to the end of the document would have passed all three.
  it.each([
    ['full', full],
    ['minimal', minimal],
    ['reordered', reordered]
  ])('is the document’s own, with the optional omissions spliced in (%s)', (_name, document) => {
    const { container } = draw(document)
    expect(topLevel(container)).toEqual(expectedTopLevel(document))
  })

  it('is the document’s order and not the schema’s, where they differ', () => {
    // The point of the `reordered` fixture: it writes `decision` before
    // `specVersion`, `id` and `version`, and the page used to move those three
    // in front of it.
    const declared = Object.keys(reordered as unknown as Record<string, unknown>)
    expect(declared.indexOf('decision')).toBeLessThan(declared.indexOf('specVersion'))
    const { container } = draw(reordered)
    const drawn = topLevel(container)
    expect(drawn.indexOf('/decision')).toBeLessThan(drawn.indexOf('/specVersion'))
  })
})

describe('a document missing one of its members', () => {
  const without = (member: string) => {
    const copy = JSON.parse(JSON.stringify(full)) as Record<string, unknown>
    delete copy[member]
    return copy as unknown as PackDocument
  }

  it.each(REQUIRED)('draws no block where required %s is missing', (member) => {
    // The absence of a required member is a refusal, issued by the runtime at
    // that pointer and printed on the strip where every reader sees it. A "not
    // declared" line here would take that diagnostic off the strip and hide it
    // behind a selection nobody has made.
    const { container } = draw(without(member))
    expect(topLevel(container)).not.toContain(`/${member}`)
    expect(document.getElementById(`/${member}`)).toBeNull()
  })

  it.each(OPTIONAL)('states the absence of optional %s', (member) => {
    const { container } = draw(without(member))
    expect(topLevel(container)).toContain(`/${member}`)
    expect(document.getElementById(`/${member}`)?.textContent).toContain('not declared')
  })

  it.each(REQUIRED)('still has exactly one tab stop without %s', (member) => {
    // `readingOrder` used to put a missing required member first, and the stop
    // is the first unit in reading order — so deleting `specVersion` left the
    // document's one tab stop on a pointer with no element behind it, and the
    // keyboard could not reach the document at all.
    const { container } = draw(without(member))
    const blocks = [...container.querySelectorAll<HTMLElement>('[data-pointer]')].filter(
      (element) => element.getAttribute('data-pointer') !== ''
    )
    const stops = blocks.filter((element) => element.getAttribute('tabindex') === '0')
    expect(stops).toHaveLength(1)
    expect(document.getElementById(stops[0]!.getAttribute('data-pointer')!)).toBeTruthy()
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
      '/extensions',
      // **`/description` is the ninth**, and it had no line at all while the
      // five identity members were drawn as one unit: the unit was present
      // because `title` was, so nothing ever said the description was absent.
      // An optional member's absence is a fact about the document.
      '/description'
    ]) {
      expect(found.has(pointer), pointer).toBe(true)
    }
    // Nine optional members, nine statements of absence. The view this
    // replaces rendered nothing at all for any of them.
    expect(screen.getAllByText('not declared')).toHaveLength(9)
  })

  it('marks the same members “not declared” in the outline', () => {
    draw(minimal)
    const outline = screen.getByRole('navigation', { name: 'Members' })
    expect(outline.textContent).toContain('Applicability — not declared')
    expect(outline.textContent).toContain('Escalation — not declared')
    // **Every entry is a link, absent ones included.** The document renders an
    // addressed block for an omitted member — that is what `OmittedMember` is
    // for — so an entry that could not reach it was the only line in this nav
    // naming something you could not go to. The words stay: the link goes to
    // the statement of absence, and the entry still says which it is.
    expect(screen.getByRole('link', { name: /Rules/ })).toBeTruthy()
    const absent = screen.getByRole('link', { name: /Applicability/ })
    expect(absent.textContent).toContain('not declared')
    expect(absent.getAttribute('href')).toContain('#/applicability')
    // The block it names is on the page and carries that pointer.
    expect(document.getElementById('/applicability')).toBeTruthy()
    expect(outline.querySelectorAll('a').length).toBe(
      outline.querySelectorAll('li').length
    )
  })

  it('names Identity once, however many members it collects', () => {
    // **The grouping is the nav's, and only the nav's.** Five near-identical
    // entries would be a worse nav, which is a reason to collapse them *here*
    // and not a reason to move anything on the page: the five are five units
    // in the document's own order, and this line is where they become one.
    draw(full)
    const outline = screen.getByRole('navigation', { name: 'Members' })
    const labels = [...outline.querySelectorAll('a')].map((entry) => entry.textContent ?? '')
    expect(labels.filter((label) => label.startsWith('Identity'))).toHaveLength(1)
    // And each of them is still its own addressed block on the page.
    for (const pointer of ['/specVersion', '/id', '/version', '/title']) {
      expect(document.getElementById(pointer), pointer).toBeTruthy()
    }
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
    // The document's own first member, whichever it is. The full fixture
    // writes `specVersion` first; the stop follows the document rather than a
    // fixed idea of which member comes first.
    expect(stops(blocks)[0]!.getAttribute('data-pointer')).toBe(
      `/${Object.keys(full as unknown as Record<string, unknown>)[0]}`
    )
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
