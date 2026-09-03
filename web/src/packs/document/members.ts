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
  /** The root members this unit draws, in the schema's order. */
  members: readonly RootMember[]
  /** The pointer the outline links to and the spy watches. */
  pointer: string
  /** True where the unit is a list and the outline may carry its length. */
  counted?: boolean
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
    id: 'identity',
    label: 'Identity',
    members: ['specVersion', 'id', 'version', 'title', 'description'],
    pointer: '/title'
  },
  { id: 'decision', label: 'Decision', members: ['decision'], pointer: '/decision' },
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
  { id: 'outcomes', label: 'Outcomes', members: ['outcomes'], pointer: '/outcomes', counted: true },
  { id: 'rules', label: 'Rules', members: ['rules'], pointer: '/rules', counted: true },
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
 * page has no business re-arranging what someone wrote. An absent unit has no
 * position of its own, so it takes the one the schema would give it — after
 * the nearest earlier unit, which is why the walk below is in schema order and
 * chains: two absent neighbours end up in their own relative order too.
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
