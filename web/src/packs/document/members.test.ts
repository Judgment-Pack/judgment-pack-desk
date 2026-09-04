/**
 * The member list itself: what is read, in what order, and what the nav calls
 * it.
 *
 * Three claims live here rather than in the view, because they are facts about
 * the list and the view is only one of the three places that reads it.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import type { PackDocument } from '../../mcp/types'
import { outlineRepresentatives, outlineUnits, readingOrder } from './members'

const FIXTURES = join(import.meta.dirname, '..', '__fixtures__')
const full = JSON.parse(readFileSync(join(FIXTURES, 'full.pack.json'), 'utf8')) as PackDocument

const without = (member: string) => {
  const copy = JSON.parse(JSON.stringify(full)) as Record<string, unknown>
  delete copy[member]
  return copy as unknown as PackDocument
}

const IDENTITY = ['/specVersion', '/id', '/version', '/title', '/description']

describe('the outline entry a reading unit is listed under', () => {
  it('is the one Identity entry, for every one of the five', () => {
    // The spy answers in reading-unit pointers and the outline lists `Identity`
    // once, so four of the five equalled no entry at all: reading the version,
    // or following a link to `/id`, left the nav marking nothing.
    const order = readingOrder(full)
    const representative = outlineRepresentatives(full, order)
    const identity = outlineUnits(full, order).find((entry) => entry.id === 'identity')
    expect(identity).toBeDefined()
    for (const pointer of IDENTITY) {
      expect(representative.get(pointer), pointer).toBe(identity!.pointer)
    }
  })

  it('is the unit itself, for every unit the outline lists on its own', () => {
    const order = readingOrder(full)
    const representative = outlineRepresentatives(full, order)
    for (const unit of order) {
      if (IDENTITY.includes(unit.pointer)) continue
      expect(representative.get(unit.pointer), unit.pointer).toBe(unit.pointer)
    }
  })

  it('lands on a member that is on the page, where the first identity member is not', () => {
    const document = without('specVersion')
    const order = readingOrder(document)
    const representative = outlineRepresentatives(document, order)
    const landing = representative.get('/title')
    expect(landing).toBeDefined()
    // Whatever it is, it is a unit the page draws — never the missing one.
    expect(landing).not.toBe('/specVersion')
    expect(order.some((unit) => unit.pointer === landing)).toBe(true)
  })
})

describe('reading order', () => {
  it.each(['specVersion', 'id', 'version', 'title', 'decision', 'outcomes', 'rules'])(
    'leaves a missing required %s out entirely',
    (member) => {
      const order = readingOrder(without(member))
      expect(order.map((unit) => unit.pointer)).not.toContain(`/${member}`)
    }
  )

  it.each([
    'description',
    'applicability',
    'evidenceRequirements',
    'sources',
    'exceptions',
    'fallbackOutcome',
    'escalation',
    'metadata',
    'extensions'
  ])('keeps a missing optional %s, which is what an omission line is', (member) => {
    const order = readingOrder(without(member))
    expect(order.map((unit) => unit.pointer)).toContain(`/${member}`)
  })

  it('starts at a unit the page can draw, whichever member is missing', () => {
    // The document's one tab stop is the first unit in reading order. With a
    // missing required member first in the list, that pointer had no element
    // behind it and the keyboard could not reach the document at all.
    for (const member of ['specVersion', 'id', 'version', 'title']) {
      const document = without(member)
      const first = readingOrder(document)[0]!
      expect(first.pointer, member).not.toBe(`/${member}`)
    }
  })
})
