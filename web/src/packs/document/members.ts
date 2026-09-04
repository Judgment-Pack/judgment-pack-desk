/**
 * The document's members, in the order the schema declares them, in one place.
 *
 * Mirrored from the bundled schema's root `properties` order at
 * `internal/artifacts/jps/0.2.0-draft/schema.json`:
 *
 *   specVersion, id, version, title, description, decision, applicability,
 *   evidenceRequirements, sources, outcomes, rules, exceptions,
 *   fallbackOutcome, escalation, metadata, extensions
 *
 * It is one list because it is used three times — the reading order, the
 * outline, and the position an omitted member's line is spliced into — and
 * three copies would be three chances to disagree with the schema.
 *
 * The five identity members are one **unit** because they are one thing on the
 * page: the eyebrow above the decision question. Every other member is its own
 * unit. A unit is what the outline lists and what "omitted" is said about.
 */
import type { PackDocument } from '../../mcp/types'

/** Every root member name the schema declares, in its order. */
export const ROOT_MEMBERS = [
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
] as const

export type RootMember = (typeof ROOT_MEMBERS)[number]

export interface MemberUnit {
  /** The unit's id, which is also its outline entry's key. */
  id: string
  /** What the outline calls it. */
  label: string
  /**
   * What the block calls it, where that differs from the outline's word.
   *
   * The five identity members share one outline entry and are five blocks, so
   * an absent one saying "Identity — not declared" would name the group rather
   * than the member. The omission line has to say which member is missing:
   * `/description` is the one this fixes, and it is the member that had no
   * omission line at all while the five were drawn as one unit.
   */
  blockLabel?: string
  /**
   * True where the schema requires this member.
   *
   * **Seven of them**: `specVersion`, `id`, `version`, `title`, `decision`,
   * `outcomes` and `rules` — the root `required` list in the bundled
   * `jps/0.2.0-draft` schema, which `PackDocument` also spells as
   * non-optional. Four were marked here and three were not, so a pack missing
   * its decision, its outcomes or its rules drew "not declared" over the
   * refusal the runtime issues at that pointer.
   *
   * An omission line states an **optional** member's absence: "the pack cites
   * nothing" is a fact about a document that is complete without sources. A
   * required member that is missing is not an omission, it is a refusal — the
   * runtime issues one at that member's pointer, and drawing a block there
   * would take that diagnostic off the strip, where it is visible, and put it
   * behind a selection nobody has made. So a required member that is absent
   * renders nothing and its diagnostic anchors on the document.
   */
  required?: true
  /** The root members this unit draws, in the schema's order. */
  members: readonly RootMember[]
  /** The pointer the outline links to and the spy watches. */
  pointer: string
  /** True where the unit is a list and the outline may carry its length. */
  counted?: boolean
  /**
   * The outline entry this unit is listed under, where it is not its own.
   *
   * **Grouping is an outline concern and nothing else.** The five identity
   * members used to be one unit, which made them one position in reading
   * order — so a document that wrote `decision` before `specVersion`, `id` and
   * `version` had those three moved in front of `decision` by the page, and a
   * reordered document was silently re-ordered back. They are five units now,
   * each finding its own place in the document's own order; the outline still
   * shows one "Identity" line, because a nav with five near-identical entries
   * is a worse nav and that is a presentation question.
   */
  group?: string
}

/**
 * The units, in the schema's own order.
 *
 * `pointer` is the member's own pointer, except for the identity group, which
 * has five members and therefore no single one of its own — it is addressed by
 * `/title`, the member the eyebrow is built around and the one a diagnostic
 * about the document's name would carry.
 */
export const MEMBER_UNITS: readonly MemberUnit[] = [
  {
    id: 'specVersion',
    label: 'Identity',
    blockLabel: '/specVersion',
    members: ['specVersion'],
    pointer: '/specVersion',
    group: 'identity',
    required: true
  },
  {
    id: 'id',
    label: 'Identity',
    blockLabel: '/id',
    members: ['id'],
    pointer: '/id',
    group: 'identity',
    required: true
  },
  {
    id: 'version',
    label: 'Identity',
    blockLabel: '/version',
    members: ['version'],
    pointer: '/version',
    group: 'identity',
    required: true
  },
  {
    id: 'title',
    label: 'Identity',
    blockLabel: '/title',
    members: ['title'],
    pointer: '/title',
    group: 'identity',
    required: true
  },
  {
    id: 'description',
    label: 'Identity',
    blockLabel: '/description',
    members: ['description'],
    pointer: '/description',
    group: 'identity'
  },
  { id: 'decision', label: 'Decision', members: ['decision'], pointer: '/decision', required: true },
  {
    id: 'applicability',
    label: 'Applicability',
    members: ['applicability'],
    pointer: '/applicability'
  },
  {
    id: 'evidenceRequirements',
    label: 'Evidence',
    members: ['evidenceRequirements'],
    pointer: '/evidenceRequirements',
    counted: true
  },
  { id: 'sources', label: 'Sources', members: ['sources'], pointer: '/sources', counted: true },
  {
    id: 'outcomes',
    label: 'Outcomes',
    members: ['outcomes'],
    pointer: '/outcomes',
    counted: true,
    required: true
  },
  {
    id: 'rules',
    label: 'Rules',
    members: ['rules'],
    pointer: '/rules',
    counted: true,
    required: true
  },
  {
    id: 'exceptions',
    label: 'Exceptions',
    members: ['exceptions'],
    pointer: '/exceptions',
    counted: true
  },
  {
    id: 'fallbackOutcome',
    label: 'Fallback outcome',
    members: ['fallbackOutcome'],
    pointer: '/fallbackOutcome'
  },
  { id: 'escalation', label: 'Escalation', members: ['escalation'], pointer: '/escalation' },
  { id: 'metadata', label: 'Metadata', members: ['metadata'], pointer: '/metadata' },
  { id: 'extensions', label: 'Extensions', members: ['extensions'], pointer: '/extensions' }
]

/** True where the document declares any member of this unit. */
export function unitIsPresent(document: PackDocument, unit: MemberUnit): boolean {
  return unit.members.some((member) => (document as unknown as Record<string, unknown>)[member] !== undefined)
}

/**
 * The units in reading order: **present ones in the document's own key order**,
 * with each absent unit's line spliced in where the schema's order would put
 * it.
 *
 * The document's order is the one that is read, because a pack that puts
 * `rules` before `outcomes` is a pack whose author put them that way, and the
 * page has no business re-arranging what someone wrote. An absent **optional**
 * unit has no position of its own, so it takes the one the schema would give
 * it — after the nearest earlier unit, which is why the walk below is in schema
 * order and chains: two absent neighbours end up in their own relative order
 * too. An absent **required** unit gets no line at all; see `required`.
 */
export function readingOrder(document: PackDocument): MemberUnit[] {
  const documentOrder = Object.keys(document)
  const positionOf = (unit: MemberUnit) => {
    const positions = unit.members
      .map((member) => documentOrder.indexOf(member))
      .filter((index) => index >= 0)
    return positions.length === 0 ? Number.POSITIVE_INFINITY : Math.min(...positions)
  }

  const order = MEMBER_UNITS.filter((unit) => unitIsPresent(document, unit)).sort(
    (left, right) => positionOf(left) - positionOf(right)
  )

  for (const [index, unit] of MEMBER_UNITS.entries()) {
    if (order.includes(unit)) continue
    // **Only optional members get a line.** A required member that is absent is
    // not an omission but a refusal, issued by the runtime at that member's own
    // pointer, and a block here would take that diagnostic off the strip — where
    // every reader sees it — and hide it behind a selection nobody has made. It
    // is decided here, once: a document missing `decision` used to put a "not
    // declared" line first in reading order, which also made the page's single
    // tab stop a pointer with no element behind it.
    if (unit.required === true) continue
    let anchor = -1
    for (let earlier = index - 1; earlier >= 0; earlier -= 1) {
      const at = order.indexOf(MEMBER_UNITS[earlier]!)
      if (at >= 0) {
        anchor = at
        break
      }
    }
    order.splice(anchor + 1, 0, unit)
  }
  return order
}

/** How many entries a counted unit carries, or undefined. */
export function unitCount(document: PackDocument, unit: MemberUnit): number | undefined {
  if (unit.counted !== true) return undefined
  const value = (document as unknown as Record<string, unknown>)[unit.members[0]!]
  return Array.isArray(value) ? value.length : undefined
}

/**
 * The outline's entries, which is not the same list as reading order.
 *
 * One entry per group, at the position of that group's **first** unit in
 * reading order, and one entry per ungrouped unit. This is the whole of the
 * grouping: the page draws every unit where the document put it, and the nav
 * shows `Identity` once because five entries called Identity would be a worse
 * nav — a presentation choice, made where presentation is decided rather than
 * by collapsing five members into one position in the document.
 *
 * A group is present where **any** of its members is, and absent where none
 * is; its pointer is the first present member's, so the link lands on
 * something that is on the page.
 */
export function outlineUnits(document: PackDocument, order: readonly MemberUnit[]): MemberUnit[] {
  const entries: MemberUnit[] = []
  const placed = new Set<string>()
  for (const unit of order) {
    if (unit.group === undefined) {
      entries.push(unit)
      continue
    }
    if (placed.has(unit.group)) continue
    placed.add(unit.group)
    const members = order.filter((other) => other.group === unit.group)
    const present = members.find((other) => unitIsPresent(document, other))
    entries.push({
      id: unit.group,
      label: unit.label,
      members: members.flatMap((other) => other.members),
      pointer: (present ?? unit).pointer
    })
  }
  return entries
}

/**
 * Which outline entry each reading unit is listed under.
 *
 * The spy answers in **reading-unit** pointers — `/specVersion`, `/id`,
 * `/version`, `/title`, `/description` — and the outline lists one `Identity`
 * entry addressed by whichever of the five is present. Comparing the two
 * directly meant four of the five could never mark anything: a reader looking
 * at the version, or a link to `/id`, left the nav with no current entry at
 * all. The grouping happens in one place, so its inverse belongs beside it.
 */
export function outlineRepresentatives(
  document: PackDocument,
  order: readonly MemberUnit[]
): Map<string, string> {
  const entries = outlineUnits(document, order)
  const representative = new Map<string, string>()
  for (const unit of order) {
    const listed = entries.find((entry) => entry.id === (unit.group ?? unit.id))
    if (listed !== undefined) representative.set(unit.pointer, listed.pointer)
  }
  return representative
}
