/** Presentation only. Keys, pointers, enum values and author text stay exact. */
export const PACK_TERMS = {
  applicability: { label: 'When this pack applies', description: 'Defines which cases this pack covers.' },
  evidenceRequirements: { label: 'Evidence needed', description: 'Information or supporting material the case needs.' },
  rules: { label: 'Decision rules', description: 'Conditions that contribute possible outcomes. Rule order does not set priority.' },
  exceptions: { label: 'Special cases', description: 'Conditions that exclude rules, force an outcome, or request a handoff.' },
  resolution: { label: 'Result handling', description: 'Fallback outcome and handoff settings.' },
  outcomes: { label: 'Possible outcomes', description: 'The outcomes this pack can select.' },
  sources: { label: 'Source references', description: 'References supplied by the author to support the pack.' },
  fallbackOutcome: { label: 'Fallback outcome', description: 'Used when no rule selects an outcome and nothing blocks a result.' },
  escalation: { label: 'Handoff settings', description: 'When to request a handoff, and who or what should receive it. A request does not confirm delivery.' },
  onUnknown: { label: 'If this condition is unknown', description: 'Choose whether an unknown condition blocks the result or contributes nothing.' },
  specVersion: { label: 'JPS version', description: 'The version of the Judgment Pack specification used by this document.' }
} as const
export type PackTerm = keyof typeof PACK_TERMS

const FIELDS: Record<string, string> = {
  id: 'ID', title: 'Title', label: 'Label', description: 'Description', version: 'Pack version',
  decision: 'Decision', question: 'Decision question', intent: 'Purpose', when: 'Condition',
  outcome: 'Outcome', effect: 'Effect', targetRule: 'Rule to exclude', rationale: 'Reasoning',
  evidenceRequirementRefs: 'Evidence references', sourceRefs: 'Source references',
  evidenceRequirement: 'Evidence item', required: 'Required', kind: 'Type',
  path: 'Fact path', operator: 'Comparison', value: 'Value', triggers: 'Handoff triggers',
  target: 'Recipient', name: 'Name', message: 'Handoff message', locator: 'Location',
  citation: 'Citation', publisher: 'Publisher', publishedAt: 'Publication date',
  location: 'Location', excerpt: 'Excerpt', rights: 'Rights', metadata: 'Metadata', extensions: 'Extensions'
}
export function packTerm(key: string) {
  return Object.hasOwn(PACK_TERMS, key) ? PACK_TERMS[key as PackTerm] : undefined
}
export function fieldLabel(key: string, fallback = key): string {
  return packTerm(key)?.label ?? (Object.hasOwn(FIELDS, key) ? FIELDS[key]! : fallback)
}
const CONTEXT_FIELDS: Readonly<Record<string, string>> = {
  '/locator/value': 'Source location', '/locator/kind': 'Location type',
  '/target/name': 'Recipient name', '/target/kind': 'Recipient type'
}
export function pointerLabel(pointer: string, fallback: string): string {
  const contextual = Object.entries(CONTEXT_FIELDS).find(([suffix]) => pointer.endsWith(suffix))
  return contextual?.[1] ?? fieldLabel(pointer.split('/').at(-1) ?? '', fallback)
}

const VALUES: Record<string, Readonly<Record<string, string>>> = {
  operator: { equals: 'is equal to', 'not-equals': 'is not equal to', 'greater-than': 'is greater than',
    'greater-than-or-equal': 'is greater than or equal to', 'less-than': 'is less than',
    'less-than-or-equal': 'is less than or equal to', in: 'is one of' },
  op: { all: 'All conditions', any: 'Any condition', not: 'Not', fact: 'Compare a fact',
    'evidence-present': 'Evidence is present', literal: 'Fixed condition' },
  onUnknown: { ignore: 'Continue without a contribution', escalate: 'Keep the result unresolved' },
  effect: { 'suppress-rule': 'Exclude rule', 'force-outcome': 'Force outcome', escalate: 'Request handoff' },
  condition: { true: 'Met', false: 'Not met', unknown: 'Cannot determine', 'not-evaluated': 'Not evaluated' },
  triggers: { 'not-applicable': 'Outside this pack’s scope', 'missing-required-evidence': 'Required evidence is missing',
    unknown: 'A condition cannot be determined', conflict: 'Conflicting outcomes', 'no-match': 'No outcome selected' }
}
export function valueLabel(key: string, value: string): string {
  const labels = Object.hasOwn(VALUES, key) ? VALUES[key] : undefined
  return labels && Object.hasOwn(labels, value) ? labels[value]! : value
}

export const TERM_HELP = {
  onUnknown: 'Continue without a contribution leaves this condition unknown and adds no outcome or effect. Keep the result unresolved blocks both normal outcomes and the fallback. Handoff settings separately determine whether that reason requests a handoff.',
  fallbackOutcome: 'For example, a pack can use “Request more information” when no rule selects an outcome. Missing required evidence, conflicting outcomes, and blocking unknown conditions prevent fallback. A fallback does not itself request a handoff.'
} as const
