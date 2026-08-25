/**
 * The §8.3 dispositions the fixtures compare, as RFC 8785 canonical text.
 *
 * Shared, because the comparator decides on these exact bytes: a fixture whose
 * disposition is not canonical is not a fixture of anything the runtime can
 * produce, and a second copy of these strings would be free to drift out of
 * canonical form one member at a time. `canonicalise` below is what holds them
 * to it, and it is exercised over every one of them by a test rather than
 * trusted.
 *
 * The vocabulary is the evaluator's own: `outcome`, `unresolved` and
 * `not-applicable` are the kinds — there is no `unknown` kind — `handoff` is
 * required and never omitted, and a `requested` handoff names what triggered
 * it.
 */

/**
 * Every §8.3 disposition the fixtures use, in one record.
 *
 * A record rather than loose constants so that the canonicality test iterates
 * *all* of them by construction: a new fixture string added here is checked the
 * moment it exists, where one added beside the record could sit unchecked.
 */
export const DISPOSITIONS = {
  /** An outcome that requested no handoff. */
  PROCEED: '{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"proceed","reasons":[]}',
  /** A second outcome that requested no handoff, for an upstream node. */
  CLEAR: '{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"clear","reasons":[]}',
  /** An outcome that did request one, so a named target is reported beside it. */
  REVIEW:
    '{"handoff":{"state":"requested","triggeredBy":["exception-escalation"]},"kind":"outcome","outcomeId":"review","reasons":["exception-escalation"]}',
  /** An unresolved disposition, which always requests a handoff. */
  UNRESOLVED:
    '{"handoff":{"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["unknown"]}',
  /** A pack that did not apply. It carries no outcomeId. */
  NOT_APPLICABLE:
    '{"handoff":{"state":"none"},"kind":"not-applicable","reasons":["not-applicable"]}'
} as const

/**
 * Rendered handoff targets, kept apart from the dispositions above.
 *
 * They are canonical JSON too and are checked for it, but they are not
 * dispositions: their `kind` is a target kind, and holding them to the
 * evaluator's disposition vocabulary would be checking one thing against
 * another thing's rules.
 */
export const TARGETS = {
  REVIEW_QUEUE: '{"kind":"queue","name":"vendor-review"}',
  EMEA_QUEUE: '{"kind":"queue","name":"vendor-review-emea"}'
} as const

export const { PROCEED, CLEAR, REVIEW, UNRESOLVED, NOT_APPLICABLE } = DISPOSITIONS
export const { REVIEW_QUEUE, EMEA_QUEUE } = TARGETS

/**
 * One JSON text in RFC 8785 canonical form: object members ordered by name and
 * no whitespace.
 *
 * Array order is left alone. The runtime sorts the reason and trigger *sets* at
 * the source rather than at serialization, so re-sorting them here would hide a
 * fixture that listed them wrongly instead of catching it.
 */
export function canonicalise(text: string): string {
  const order = (value: unknown): unknown => {
    if (Array.isArray(value)) return value.map(order)
    if (value === null || typeof value !== 'object') return value
    const source = value as Record<string, unknown>
    const sorted: Record<string, unknown> = {}
    for (const key of Object.keys(source).sort()) sorted[key] = order(source[key])
    return sorted
  }
  return JSON.stringify(order(JSON.parse(text)))
}
