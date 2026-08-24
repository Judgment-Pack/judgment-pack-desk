/**
 * Types for the documents the runtime serves.
 *
 * These mirror the Judgment Pack Specification 0.2.0-draft schema the runtime
 * bundles and the runtime's own (non-normative) inventory convention. Every
 * member below appears in one of those two; nothing here is invented. Optional
 * members really are optional, so the views must render a document that omits
 * them rather than assume the shape of any one project's packs.
 */

/** A condition tree. `op` selects the shape; the rest varies by operator. */
export interface Condition {
  op: string
  [member: string]: unknown
}

export interface Decision {
  intent: string
  question: string
}

export interface Outcome {
  id: string
  label: string
  description?: string
}

export interface EvidenceRequirement {
  id: string
  description: string
  required: boolean
  kind?: string
}

export interface Source {
  id: string
  title: string
  publisher?: string
  publishedAt?: string
  locator: { kind: string; value: string }
  citation?: { location: string; excerpt: string }
  rights?: string
}

export interface Rule {
  id: string
  description: string
  when: Condition
  outcome: string
  onUnknown: string
  evidenceRequirementRefs?: string[]
  sourceRefs?: string[]
  rationale?: string
}

export interface Exception {
  id: string
  description: string
  when: Condition
  effect: string
  outcome?: string
  onUnknown: string
  targetRule?: string
  sourceRefs?: string[]
}

export interface Escalation {
  triggers: string[]
  target: { kind: string; name: string }
  message?: string
}

export interface PackMetadata {
  authors?: string[]
  createdAt?: string
  license?: string
  requiredExtensions?: string[]
  reviews?: unknown
}

export interface PackDocument {
  specVersion: string
  id: string
  version: string
  title: string
  description?: string
  decision: Decision
  applicability?: Condition
  evidenceRequirements?: EvidenceRequirement[]
  sources?: Source[]
  outcomes: Outcome[]
  rules: Rule[]
  exceptions?: Exception[]
  fallbackOutcome?: string
  escalation?: Escalation
  metadata?: PackMetadata
  extensions?: Record<string, unknown>
}

/** One entry of the runtime's `list_packs` inventory. */
export interface PackSummary {
  id: string
  packId?: string
  packVersion?: string
  path?: string
  matrixPath?: string
  matrix?: boolean
  description?: string
  expectedVersionStatus?: string
  evidenceRequirements?: string[]
  consultedFactPaths?: string[]
  facts?: { key: string; source?: string; hint?: string }[]
  evidence?: { key: string; source?: string; hint?: string }[]
}

export interface PackInventory {
  outputVersion?: string
  tool?: { name: string; version: string }
  status?: string
  configPath?: string
  configVersion?: string
  note?: string
  packs?: PackSummary[]
}

/** The metadata `get_pack` returns beside the document itself. */
export interface PackFileMeta {
  id?: string
  packId?: string
  packVersion?: string
  specVersion?: string
  path?: string
  description?: string
  bytes?: number
  sha256?: string
}

export interface LoadedPack {
  document: PackDocument
  meta: PackFileMeta
  /** The document exactly as the runtime served it, for the Raw JSON view. */
  raw: string
}
