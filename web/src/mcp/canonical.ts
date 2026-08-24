/**
 * Reading the two encodings the matrix surfaces report results in.
 *
 * A matrix row does not carry a disposition object. It carries the RFC 8785
 * canonical *bytes* of one, as a string, because byte equality against the
 * row's own expectation is what decided the row. So a view that wants to show
 * `kind` and `outcomeId` parses that text, and a view that wants the verdict
 * compares the text — never the parse, which would be a second comparator
 * disagreeing with the one that ran.
 *
 * Probe names are the other encoding: a small colon-delimited grammar the
 * coverage derivation emits. Only the parts the grammar fixes are read here.
 * Everything else is left as the runtime spelled it.
 */
import type { Disposition } from './types'

/**
 * The canonical disposition text of a row, parsed for display.
 *
 * Undefined where there is nothing to parse — a row expecting a refusal has
 * empty expected and actual members — or where the text is not the object this
 * expects. Nothing is invented on a parse failure: the caller falls back to
 * showing the text exactly as it arrived.
 */
export function parseDisposition(text: string | undefined): Disposition | undefined {
  if (!text) return undefined
  try {
    const value = JSON.parse(text) as unknown
    if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined
    return value as Disposition
  } catch {
    return undefined
  }
}

/** The three things a reported handoff target member can be (ADR-0025). */
export const NO_HANDOFF_TARGET = 'null'
export const HANDOFF_TARGET_UNAVAILABLE = 'unavailable'

/**
 * One reported handoff target, rendered for a person.
 *
 * The member holds a canonical `{"kind":…,"name":…}` rendering, the literal
 * `null` for an evaluation that reported no target at all, or `unavailable`
 * where the report cannot state one — a refused evaluation, most often. Those
 * last two are distinct and are never collapsed: "no target" is an answer, and
 * "unavailable" is the absence of one.
 */
export function describeHandoffTarget(member: string): string {
  if (member === NO_HANDOFF_TARGET) return 'no target'
  if (member === HANDOFF_TARGET_UNAVAILABLE) return 'unavailable'
  const parsed = parseDisposition(member) as { kind?: string; name?: string } | undefined
  if (parsed?.name) return parsed.kind ? `${parsed.name} (${parsed.kind})` : parsed.name
  // A rendering this sheet cannot decompose is still the runtime's own text,
  // so it is shown rather than replaced with a guess about what it meant.
  return member
}

/** A probe name, split only where its grammar fixes the meaning of a part. */
export interface ParsedProbe {
  /** `outcome`, `boundary`, `node`, `edge`, or `reason` for a bare reason name. */
  family: string
  /** The graph node a `node:` probe is namespaced to, where there is one. */
  node?: string
  /** The probe as it reads once the namespace prefix is removed. */
  rest: string
}

const BARE_REASONS = new Set([
  'not-applicable',
  'missing-required-evidence',
  'unknown',
  'conflict',
  'exception-escalation',
  'no-match'
])

/**
 * Classify one probe name.
 *
 * The grammar is `outcome:<id>`, `boundary:<pointer>:<literal>`, a bare reason
 * name, and — inside a graph — `node:<nodeId>:<packProbe>` and
 * `edge:<index>:resolved|unresolved`. Node ids are lowercase kebab and carry
 * no colon, so the namespace split is exact. A boundary pointer may itself
 * contain colons and may be truncated with a digest tail, so it is never split
 * further: the name after the family is kept whole, as a label.
 */
export function parseProbe(probe: string): ParsedProbe {
  if (probe.startsWith('node:')) {
    const rest = probe.slice('node:'.length)
    const cut = rest.indexOf(':')
    if (cut > 0) {
      const inner = parseProbe(rest.slice(cut + 1))
      return { family: inner.family, node: rest.slice(0, cut), rest: inner.rest }
    }
  }
  if (probe.startsWith('edge:')) return { family: 'edge', rest: probe.slice('edge:'.length) }
  if (probe.startsWith('outcome:')) return { family: 'outcome', rest: probe.slice('outcome:'.length) }
  if (probe.startsWith('boundary:')) return { family: 'boundary', rest: probe.slice('boundary:'.length) }
  if (BARE_REASONS.has(probe)) return { family: 'reason', rest: probe }
  return { family: 'other', rest: probe }
}

/**
 * The graph nodes a coverage report names, in the order it names them.
 *
 * The report derives one block of probes per node in the walk's own evaluation
 * order, so reading the order out of it reports the runtime's ordering rather
 * than inventing one. This is the only account of a graph's nodes that reaches
 * the wire: see the README's upstream gaps.
 */
export function nodesInWalkOrder(coverage: readonly { probe: string }[] | undefined): string[] {
  const seen: string[] = []
  for (const probe of coverage ?? []) {
    const node = parseProbe(probe.probe).node
    if (node !== undefined && !seen.includes(node)) seen.push(node)
  }
  return seen
}

/** How many edges the coverage report accounts for, by their reported indices. */
export function edgeIndices(coverage: readonly { probe: string }[] | undefined): number[] {
  const seen = new Set<number>()
  for (const probe of coverage ?? []) {
    const parsed = parseProbe(probe.probe)
    if (parsed.family !== 'edge') continue
    const index = Number(parsed.rest.split(':')[0])
    if (Number.isInteger(index)) seen.add(index)
  }
  return [...seen].sort((a, b) => a - b)
}
