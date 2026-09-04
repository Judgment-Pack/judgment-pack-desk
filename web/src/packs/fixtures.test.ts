/**
 * The fixtures are documents the runtime accepts.
 *
 * They were not. `full.pack.json` wrote `"triggers": ["screening-report"]` —
 * an evidence-requirement id in a slot that takes one of five reason words —
 * and a References test asserted the desk resolved it, so an invalid fixture
 * made a wrong model pass and the test froze it in place. `minimal` and
 * `reordered` each declared one outcome where the schema needs two.
 *
 * The suite has no runtime in it, so this holds the next one by shape: every
 * enum-valued member of every fixture must be spelled the way the bundled
 * `jps/0.2.0-draft` schema spells it, and the closed lists below are that
 * schema's own, copied from `internal/artifacts/jps/0.2.0-draft/schema.json`.
 * **Including a condition node's own `op`**, at every depth: the walk collected
 * the operators *inside* `fact` nodes and never checked the word that says
 * which kind of node it is, so `"op": "alll"` — a tree the runtime refuses and
 * this desk's renderer draws as nothing at all — survived the test that exists
 * to catch exactly that.
 * Checked against `jpack 0.19.0 spec validate`, which reports all three
 * fixtures conformant — `full` reaching `unsupported` only for the required
 * extension the runtime does not bundle, with carrier, structural and semantic
 * all passed.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const FIXTURES = join(import.meta.dirname, '__fixtures__')
const load = (name: string) =>
  JSON.parse(readFileSync(join(FIXTURES, name), 'utf8')) as Record<string, unknown>

/** Each closed list the schema declares, by the pointer its values sit at. */
const ENUMS: { where: (document: Record<string, unknown>) => unknown[]; allowed: string[] }[] = [
  {
    where: (doc) => list(doc.evidenceRequirements).map((entry) => at(entry, 'kind')),
    allowed: ['document', 'fact', 'measurement', 'attestation']
  },
  {
    where: (doc) => list(doc.sources).map((entry) => at(at(entry, 'locator'), 'kind')),
    allowed: ['uri', 'repository', 'path', 'other']
  },
  {
    where: (doc) => list(doc.rules).map((entry) => at(entry, 'onUnknown')),
    allowed: ['ignore', 'escalate']
  },
  {
    where: (doc) => list(doc.exceptions).map((entry) => at(entry, 'effect')),
    allowed: ['suppress-rule', 'force-outcome', 'escalate']
  },
  {
    where: (doc) => list(doc.exceptions).map((entry) => at(entry, 'onUnknown')),
    allowed: ['ignore', 'escalate']
  },
  {
    where: (doc) => list(at(doc.escalation, 'triggers')),
    allowed: ['not-applicable', 'missing-required-evidence', 'unknown', 'conflict', 'no-match']
  },
  {
    where: (doc) => [at(at(doc.escalation, 'target'), 'kind')],
    allowed: ['human-role', 'queue', 'system']
  },
  {
    where: (doc) => list(at(doc.metadata, 'reviews')).map((entry) => at(entry, 'disposition')),
    allowed: ['approved', 'changes-requested', 'rejected']
  },
  {
    // **Every condition node's own `op`.** This list was the one closed enum the
    // walk below collected values *inside* and never checked itself: a fixture
    // spelling `"op": "alll"` — a tree the runtime refuses and the desk's own
    // renderer draws as nothing — passed every assertion here.
    where: (doc) => conditions(doc).map((node) => node.op),
    allowed: ['literal', 'all', 'any', 'not', 'fact', 'evidence-present']
  },
  {
    where: (doc) => conditions(doc).filter((node) => node.op === 'fact').map((node) => node.operator),
    allowed: [
      'equals',
      'not-equals',
      'greater-than',
      'greater-than-or-equal',
      'less-than',
      'less-than-or-equal',
      'in'
    ]
  }
]

const CONFORMANT = ['full.pack.json', 'minimal.pack.json', 'reordered.pack.json']

describe('every fixture a behaviour is asserted against', () => {
  it.each(CONFORMANT)('spells each enum-valued member the way the spec does (%s)', (name) => {
    const document = load(name)
    for (const { where, allowed } of ENUMS) {
      for (const value of where(document)) {
        if (value === undefined) continue
        expect(allowed, `${name}: ${String(value)}`).toContain(value)
      }
    }
  })

  it.each(CONFORMANT)('declares the two outcomes the schema requires (%s)', (name) => {
    // `minimal` and `reordered` each declared one, which
    // `JPS-STRUCTURE-COLLECTION-ARITY /outcomes` refuses by name.
    expect(list(load(name).outcomes).length).toBeGreaterThanOrEqual(2)
  })
})

describe('the fixture that is meant to be refused', () => {
  it('is refused for the one reason it exists to carry', () => {
    // `duplicate-member.pack.json` is deliberately not conformant: it is the
    // document the two readings of one file disagree about. Its defect is a
    // duplicated member and nothing else, so a second one creeping in cannot
    // pass for the one under test.
    const text = readFileSync(join(FIXTURES, 'duplicate-member.pack.json'), 'utf8')
    expect(text.match(/"title":/g)?.length).toBe(2)
  })
})

function list(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function at(value: unknown, name: string): unknown {
  return typeof value === 'object' && value !== null
    ? (value as Record<string, unknown>)[name]
    : undefined
}

/** Every condition node in the document, at whatever depth a tree carries it. */
function conditions(document: Record<string, unknown>): Record<string, unknown>[] {
  const found: Record<string, unknown>[] = []
  const walk = (node: unknown) => {
    if (typeof node !== 'object' || node === null) return
    const value = node as Record<string, unknown>
    found.push(value)
    for (const child of list(value.conditions)) walk(child)
    if (value.condition !== undefined) walk(value.condition)
  }
  walk(document.applicability)
  for (const rule of list(document.rules)) walk(at(rule, 'when'))
  for (const exception of list(document.exceptions)) walk(at(exception, 'when'))
  return found
}
