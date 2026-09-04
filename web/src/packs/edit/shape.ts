/**
 * What the schema says a member may hold — offered, never enforced.
 *
 * Mirrored from `internal/artifacts/jps/0.2.0-draft/schema.json` the way
 * `document/members.ts` mirrors the root's property order, with the `$def` it
 * comes from named at each list. Nothing here is invented and nothing here is
 * a second validator.
 *
 * **It shapes and it never refuses.** A form that would not let an author
 * write an empty `in` list, or `5000` where the schema wants `"5000"`, is a
 * form that decides what a document may say — and the desk does not decide
 * that. `validate` does, by name, at the pointer, and the diagnostic lands on
 * the field. So these lists are what a control *offers*: the enum a Select
 * shows, the control an operand gets, whether blanking a field removes the
 * member or writes an empty string. What the author types is what is written.
 *
 * The one place the shape does change the bytes is a blanked
 * `nonEmptyString`, and that is not a refusal either: writing `""` produces a
 * document the runtime refuses by name, while omitting the member produces a
 * document that is merely smaller — and where the member was required, the
 * refusal that follows names the absent member at its own pointer, which is an
 * address the form already has.
 */

/**
 * The members whose `$ref` is `#/$defs/nonEmptyString`, by the pointer shape
 * that reaches them.
 *
 * Blanking one of these removes the member. Every other string member is
 * written as the author typed it, empty included: `source.publishedAt` is a
 * `date`-formatted string and an empty one is a diagnostic, not an omission
 * the desk should decide on the author's behalf.
 */
const NON_EMPTY_STRINGS: readonly RegExp[] = [
  // root
  /^\/title$/,
  /^\/description$/,
  // $defs/decision
  /^\/decision\/(intent|question)$/,
  // $defs/evidenceRequirement
  /^\/evidenceRequirements\/\d+\/description$/,
  // $defs/source, and its locator and citation objects
  /^\/sources\/\d+\/(title|publisher|rights)$/,
  /^\/sources\/\d+\/locator\/value$/,
  /^\/sources\/\d+\/citation\/(location|excerpt)$/,
  // $defs/outcome
  /^\/outcomes\/\d+\/(label|description)$/,
  // $defs/rule
  /^\/rules\/\d+\/(description|rationale)$/,
  // $defs/exception
  /^\/exceptions\/\d+\/description$/,
  // $defs/escalation
  /^\/escalation\/message$/,
  /^\/escalation\/target\/name$/,
  // $defs/metadata
  /^\/metadata\/license$/,
  /^\/metadata\/reviews\/\d+\/(reviewer|note)$/
]

/** Whether blanking this member removes it rather than writing `""`. */
export function isNonEmptyString(pointer: string): boolean {
  return NON_EMPTY_STRINGS.some((shape) => shape.test(pointer))
}

/**
 * `$defs/localId`: `^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`.
 *
 * A **shape offered** and not a gate: an id that does not match is written and
 * `JPS-STRUCTURE-…` names it at its own pointer. The pattern is here so a
 * field can say what the schema asks for before the check answers, which is a
 * hint and not a verdict.
 */
export const LOCAL_ID = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/

/** `$defs/decimalString`: `^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`. Also a hint. */
export const DECIMAL_STRING = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$/

/** Every closed list the schema declares, at the `$def` it declares it in. */
export const ENUMS = {
  /** `$defs/rule` and `$defs/exception`, both. */
  onUnknown: ['ignore', 'escalate'],
  /** `$defs/exception`. */
  effect: ['suppress-rule', 'force-outcome', 'escalate'],
  /** `$defs/escalation.triggers.items`. Five reason words, not ids. */
  triggers: ['not-applicable', 'missing-required-evidence', 'unknown', 'conflict', 'no-match'],
  /** `$defs/escalation.target.kind`. */
  targetKind: ['human-role', 'queue', 'system'],
  /** `$defs/source.locator.kind`. */
  locatorKind: ['uri', 'repository', 'path', 'other'],
  /** `$defs/evidenceRequirement.kind`. */
  evidenceKind: ['document', 'fact', 'measurement', 'attestation'],
  /** `$defs/metadata.reviews.items.disposition`. Rendered, never written. */
  reviewDisposition: ['approved', 'changes-requested', 'rejected'],
  /** `$defs/condition`, the five node kinds, by their `op`. */
  conditionOp: ['literal', 'all', 'any', 'not', 'fact', 'evidence-present'],
  /** `$defs/condition`, the `fact` node's `operator`. */
  factOperator: [
    'equals',
    'not-equals',
    'greater-than',
    'greater-than-or-equal',
    'less-than',
    'less-than-or-equal',
    'in'
  ]
} as const

/** Which control a `fact` node's operand gets, given the operator. */
export type OperandControl = 'decimal' | 'list' | 'json'

/**
 * The operand rule, from the `fact` node's own `allOf`.
 *
 * The four ordered comparisons take `#/$defs/decimalString` — a **string**, so
 * `"5000"` and not `5000`, which is the difference between a document the
 * runtime accepts and one it refuses by name. `in` takes an array with
 * `minItems: 1`. `equals` and `not-equals` are under `"value": true`, which
 * admits any JSON at all.
 */
export function operandControl(operator: string): OperandControl {
  switch (operator) {
    case 'greater-than':
    case 'greater-than-or-equal':
    case 'less-than':
    case 'less-than-or-equal':
      return 'decimal'
    case 'in':
      return 'list'
    default:
      return 'json'
  }
}

/**
 * The member names a new condition node of each kind carries, in the schema's
 * own order, so a kind change writes the members that kind requires.
 *
 * The author's own operand survives a kind change wherever the new kind has
 * somewhere to put it: retyping a value the author wrote would be the form
 * making a decision about the policy.
 */
export const CONDITION_MEMBERS: Record<string, readonly string[]> = {
  literal: ['op', 'value'],
  all: ['op', 'conditions'],
  any: ['op', 'conditions'],
  not: ['op', 'condition'],
  fact: ['op', 'path', 'operator', 'value'],
  'evidence-present': ['op', 'evidenceRequirement']
}

/**
 * The order each container declares its members in, from the schema's own
 * `properties` order — the same mirroring `document/members.ts` does for the
 * root, extended to the eight `$defs` a form writes into.
 *
 * It answers one question and only one: **where a member that is not there yet
 * goes**. A form field for an absent member has to write it somewhere, and the
 * two alternatives are both worse than asking the schema. Appending puts
 * `onUnknown` after `extensions`, which is a document no author would write.
 * Guessing from the neighbours reproduces whatever order the file already has,
 * which is fine until the file has one member.
 *
 * It is **not** a rule about existing members. A document that writes `rules`
 * before `outcomes` keeps that order for ever — nothing here re-sorts anything
 * — because a page that re-ordered what somebody wrote would be showing a
 * document nobody has.
 */
export const MEMBER_ORDER: readonly { shape: RegExp; members: readonly string[] }[] = [
  // the root's own `properties`
  {
    shape: /^$/,
    members: [
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
  },
  { shape: /^\/decision$/, members: ['intent', 'question', 'extensions'] },
  {
    shape: /^\/evidenceRequirements\/\d+$/,
    members: ['id', 'description', 'required', 'kind', 'extensions']
  },
  {
    shape: /^\/sources\/\d+$/,
    members: ['id', 'title', 'publisher', 'publishedAt', 'locator', 'citation', 'rights', 'extensions']
  },
  { shape: /^\/sources\/\d+\/locator$/, members: ['kind', 'value'] },
  { shape: /^\/sources\/\d+\/citation$/, members: ['location', 'excerpt'] },
  { shape: /^\/outcomes\/\d+$/, members: ['id', 'label', 'description', 'extensions'] },
  {
    shape: /^\/rules\/\d+$/,
    members: [
      'id',
      'description',
      'when',
      'outcome',
      'onUnknown',
      'evidenceRequirementRefs',
      'sourceRefs',
      'rationale',
      'extensions'
    ]
  },
  {
    shape: /^\/exceptions\/\d+$/,
    members: [
      'id',
      'description',
      'when',
      'effect',
      'targetRule',
      'outcome',
      'onUnknown',
      'sourceRefs',
      'extensions'
    ]
  },
  { shape: /^\/escalation$/, members: ['triggers', 'target', 'message', 'extensions'] },
  { shape: /^\/escalation\/target$/, members: ['kind', 'name'] },
  {
    shape: /^\/metadata$/,
    members: ['authors', 'createdAt', 'license', 'requiredExtensions', 'reviews', 'extensions']
  }
]

/** The schema's member order for one container, or nothing where it declares none. */
export function memberOrder(containerPointer: string): readonly string[] {
  return MEMBER_ORDER.find((entry) => entry.shape.test(containerPointer))?.members ?? []
}

/**
 * What an *add* writes for a member the document omits — the schema's required
 * members, empty.
 *
 * A line saying a member is not declared needs somewhere for the author to go,
 * and the only thing to write is what the schema says the member must carry.
 * An empty object would render a block with no fields in it, because a field
 * whose container is absent has nothing to splice into; a skeleton renders the
 * fields and lets `validate` name every one of them that is still empty.
 *
 * **Nothing here is a value.** Every string is empty and every list is empty;
 * the one word that is not — `escalation.target.kind` — is the first of a
 * closed enum, offered because a `kind` member has to say one of three things
 * and none of them is nothing. All of it is the author's to change and the
 * runtime's to judge.
 */
export const STARTERS: readonly { shape: RegExp; json: string }[] = [
  { shape: /^\/description$/, json: '""' },
  // $defs/condition's simplest node, which is the one the builder can edit.
  { shape: /^\/applicability$/, json: '{ "op": "literal", "value": true }' },
  { shape: /^\/evidenceRequirements$/, json: '[]' },
  { shape: /^\/sources$/, json: '[]' },
  { shape: /^\/exceptions$/, json: '[]' },
  { shape: /^\/fallbackOutcome$/, json: '""' },
  {
    shape: /^\/escalation$/,
    json: '{ "triggers": [], "target": { "kind": "human-role", "name": "" } }'
  },
  { shape: /^\/metadata$/, json: '{}' },
  { shape: /^\/extensions$/, json: '{}' }
]

/** The bytes an *add* writes at this pointer, where the desk knows a shape. */
export function starterFor(pointer: string): string | undefined {
  return STARTERS.find((entry) => entry.shape.test(pointer))?.json
}
