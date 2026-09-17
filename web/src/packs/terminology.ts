import { msg } from '../i18n'
/** Presentation only. Keys, pointers, enum values and author text stay exact. */
export const PACK_TERMS = {
  applicability: { get label() { return msg("When this pack applies") }, get description() { return msg("Defines which cases this pack covers.") } },
  evidenceRequirements: { get label() { return msg("Evidence needed") }, get description() { return msg("Information or supporting material the case needs.") } },
  rules: { get label() { return msg("Decision rules") }, get description() { return msg("Conditions that contribute possible outcomes. Rule order does not set priority.") } },
  exceptions: { get label() { return msg("Special cases") }, get description() { return msg("Conditions that exclude rules, force an outcome, or request a handoff.") } },
  resolution: { get label() { return msg("Result handling") }, get description() { return msg("Fallback outcome and handoff settings.") } },
  outcomes: { get label() { return msg("Possible outcomes") }, get description() { return msg("The outcomes this pack can select.") } },
  sources: { get label() { return msg("Source references") }, get description() { return msg("References supplied by the author to support the pack.") } },
  fallbackOutcome: { get label() { return msg("Fallback outcome") }, get description() { return msg("Used when no rule selects an outcome and nothing blocks a result.") } },
  escalation: { get label() { return msg("Handoff settings") }, get description() { return msg("When to request a handoff, and who or what should receive it. A request does not confirm delivery.") } },
  onUnknown: { get label() { return msg("If this condition is unknown") }, get description() { return msg("Choose whether an unknown condition blocks the result or contributes nothing.") } },
  specVersion: { get label() { return msg("JPS version") }, get description() { return msg("The version of the Judgment Pack specification used by this document.") } }
} as const
export type PackTerm = keyof typeof PACK_TERMS

const FIELDS: Record<string, string> = {
  id: 'ID', get title() { return msg("Title") }, get label() { return msg("Label") }, get description() { return msg("Description") }, get version() { return msg("Pack version") },
  get decision() { return msg("Decision") }, get question() { return msg("Decision question") }, get intent() { return msg("Purpose") }, get when() { return msg("Condition") },
  get outcome() { return msg("Outcome") }, get effect() { return msg("Effect") }, get targetRule() { return msg("Rule to exclude") }, get rationale() { return msg("Reasoning") },
  get evidenceRequirementRefs() { return msg("Evidence references") }, get sourceRefs() { return msg("Source references") },
  get evidenceRequirement() { return msg("Evidence item") }, get required() { return msg("Required") }, get kind() { return msg("Type") },
  get path() { return msg("Fact path") }, get operator() { return msg("Comparison") }, get value() { return msg("Value") }, get triggers() { return msg("Handoff triggers") },
  get target() { return msg("Recipient") }, get name() { return msg("Name") }, get message() { return msg("Handoff message") }, get locator() { return msg("Location") },
  get citation() { return msg("Citation") }, get publisher() { return msg("Publisher") }, get publishedAt() { return msg("Publication date") },
  get location() { return msg("Location") }, get excerpt() { return msg("Excerpt") }, get rights() { return msg("Rights") }, get metadata() { return msg("Metadata") }, get extensions() { return msg("Extensions") }
}
export function packTerm(key: string) {
  return Object.hasOwn(PACK_TERMS, key) ? PACK_TERMS[key as PackTerm] : undefined
}
export function fieldLabel(key: string, fallback = key): string {
  return packTerm(key)?.label ?? (Object.hasOwn(FIELDS, key) ? FIELDS[key]! : fallback)
}
const CONTEXT_FIELDS: Readonly<Record<string, string>> = {
  get '/locator/value'() { return msg("Source location") }, get '/locator/kind'() { return msg("Location type") },
  get '/target/name'() { return msg("Recipient name") }, get '/target/kind'() { return msg("Recipient type") }
}
export function pointerLabel(pointer: string, fallback: string): string {
  const contextual = Object.entries(CONTEXT_FIELDS).find(([suffix]) => pointer.endsWith(suffix))
  return contextual?.[1] ?? fieldLabel(pointer.split('/').at(-1) ?? '', fallback)
}

const VALUES: Record<string, Readonly<Record<string, string>>> = {
  operator: { get equals() { return msg("is equal to") }, get 'not-equals'() { return msg("is not equal to") }, get 'greater-than'() { return msg("is greater than") },
    get 'greater-than-or-equal'() { return msg("is greater than or equal to") }, get 'less-than'() { return msg("is less than") },
    get 'less-than-or-equal'() { return msg("is less than or equal to") }, get in() { return msg("is one of") } },
  op: { get all() { return msg("All conditions") }, get any() { return msg("Any condition") }, get not() { return msg("Not") }, get fact() { return msg("Compare a fact") },
    get 'evidence-present'() { return msg("Evidence is present") }, get literal() { return msg("Fixed condition") } },
  onUnknown: { get ignore() { return msg("Continue without a contribution") }, get escalate() { return msg("Keep the result unresolved") } },
  effect: { get 'suppress-rule'() { return msg("Exclude rule") }, get 'force-outcome'() { return msg("Force outcome") }, get escalate() { return msg("Request handoff") } },
  condition: { get true() { return msg("Met") }, get false() { return msg("Not met") }, get unknown() { return msg("Cannot determine") }, get 'not-evaluated'() { return msg("Not evaluated") } },
  triggers: { get 'not-applicable'() { return msg("Outside this pack’s scope") }, get 'missing-required-evidence'() { return msg("Required evidence is missing") },
    get unknown() { return msg("A condition cannot be determined") }, get conflict() { return msg("Conflicting outcomes") }, get 'no-match'() { return msg("No outcome selected") } }
}
export function valueLabel(key: string, value: string): string {
  const labels = Object.hasOwn(VALUES, key) ? VALUES[key] : undefined
  return labels && Object.hasOwn(labels, value) ? labels[value]! : value
}

export const TERM_HELP = {
  get onUnknown() { return msg("Continue without a contribution leaves this condition unknown and adds no outcome or effect. Keep the result unresolved blocks both normal outcomes and the fallback. Handoff settings separately determine whether that reason requests a handoff.") },
  get fallbackOutcome() { return msg("For example, a pack can use “Request more information” when no rule selects an outcome. Missing required evidence, conflicting outcomes, and blocking unknown conditions prevent fallback. A fallback does not itself request a handoff.") }
} as const
