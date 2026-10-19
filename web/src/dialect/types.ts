/** spec/dialect.md §§2–10. Numeric/string bounds and closed forms are checked by
 * the preparer; these types also describe the Desk's in-progress declarations. */
import type { JSONValue } from './canon'

export type Digest = string
export type Pointer = string
export type Timestamp = string
export type SourceClass = 'asserted' | 'record' | 'generated'
export type ParameterType = 'string' | 'integer' | 'timestamp'
export interface CaseParameter { pointer: Pointer; type: ParameterType; from?: never }
export interface SourceParameter { from: string; pointer: Pointer; type: ParameterType }
export type Parameter = CaseParameter | SourceParameter
export interface FactCopy { target: Pointer; source: Pointer }
export interface EvidenceCopy { requirement: string; source: Pointer }
export interface CopyMapping { facts?: FactCopy[]; evidence?: EvidenceCopy[] }
export interface DialectCase { parameters?: Record<string, CaseParameter>; facts: FactCopy[]; evidence: EvidenceCopy[] }
export type ParameterTemplate = { $param: string }
export type StringTemplate = string | ParameterTemplate | { $text: string }
export type Template = JSONValue

export type AcquisitionStatus = 'resolved' | 'absent' | 'unknown'
export type EvidenceAvailability = 'present' | 'absent' | 'unknown'
export type Condition =
  | { op: 'always' }
  | { op: 'exists' | 'isTrue' | 'isDecimalString'; field: Pointer }
  | { op: 'equals'; field: Pointer; to: JSONValue }
  | { op: 'equalsParam'; field: Pointer; param: string }
  | { op: 'freshWithin'; field: Pointer; asOf: string; maxAge: string }
  | { op: 'all' | 'any'; of: Condition[] }
  | { op: 'not'; of: Condition }
export interface ClaimForm {
  facts: { pointer: Pointer; from: Pointer }[]
  evidence: Record<string, EvidenceAvailability>
  acquisitionStatus: AcquisitionStatus
}
export interface DerivationRule {
  ruleVersion: '1'; parameters: Record<string, ParameterType>
  clauses: { when: Condition; claim: ClaimForm; reason: string }[]
}
export interface Read { sources?: string[]; unwrap?: Pointer[]; copy?: CopyMapping; rule?: DerivationRule }
export interface Claim {
  facts: Record<string, JSONValue>; evidenceAvailability: Record<string, EvidenceAvailability>
  acquisitionStatus: AcquisitionStatus; reason: string; basis: Pointer[]
}
export interface CalculatorPin { name: string; version: string }
export interface CalculationBinding { inputs: Record<string, string>; tables: Record<string, number> }
export type CalculationStatus = 'computed' | 'input-missing' | 'cannot-compute'
export interface CalculationResult {
  calculator: CalculatorPin; status: CalculationStatus
  inputs: Record<string, JSONValue>; asOf: Record<string, Timestamp>
}
export interface CalculationLineage {
  calculator: CalculatorPin; status: CalculationStatus
  inputs: { name: string; parameter: string; source: string; pointer: Pointer }[]
  asOf: Record<string, Timestamp>
}
export interface SourceBase { name: string; parameters?: Record<string, SourceParameter>; read: Read; provider?: 'local-file'|'google-drive'; arguments?:Record<string,unknown>; calculation?:CalculationBinding; profile?:string; profileDigest?:Digest }
/** §4.1/§4.2: pins and request are required for acquired sources, absent for a
 * local file. Optional here so the existing editor can retain incomplete drafts. */
export interface DialectSourceV2 extends SourceBase {
  kind: 'operation' | 'selected-file'; provider?: 'local-file' | 'google-drive'
  profile?: string; profileDigest?: Digest; maxAge?: number
  arguments?: Record<string, unknown>; calculation?: CalculationBinding
  // §4.4 is forbidden in version 2, even when a source is assigned by variable.
  platform?: never; operation?: never; shape?: never; record?: never
}
export interface ProfileBinding { profile: string; profileDigest: Digest }
export interface AcquiredSource extends SourceBase { profile:string;profileDigest:Digest;maxAge:number }
/** Version 3, proposed: §4.3. Exact request bytes await implementation vectors. */
export type HTTPSource = AcquiredSource & {
  kind: 'http'; path: Template; query?: Record<string, StringTemplate>; headers?: string[]
} & ({ method: 'GET'; body?: never } | { method: 'POST'; body?: Template })
/** Version 3, proposed: §4.3. A statement is fixed; only values are bound. */
export interface SQLSource extends AcquiredSource {
  kind: 'sql'; statement: string
  bindings: { name: string; type: ParameterType; value: ParameterTemplate }[]
}
/** Version 3, proposed: §4.3. */
export interface WebSource extends AcquiredSource { kind: 'web'; url: string | ParameterTemplate }
export type ResearchCapability = 'answer'|'search'|'page'
export interface ResearchField {name:string;type:'integer'|'decimal'|'boolean'|'text'|'text-list'|'range';required:boolean;description?:string}
/** Portable research recipes are independent of an installation's AI/web bindings. */
export interface ResearchSource extends SourceBase {
 kind:'research';maxAge:number;question:StringTemplate;constraints:Record<string,JSONValue>
 model:string;evidence:JSONValue;fields:ResearchField[];capabilities:ResearchCapability[]
 webProfiles?:Partial<Record<'search'|'page',ProfileBinding>>
}
/** Version 3, proposed: §4.4. These catalog names replace the profile pin. */
export interface UnboundOperation extends SourceBase {
  kind: 'operation'; platform: string; operation: string; shape: 'airbyte' | 'mcp' | 'http'
  record: string; maxAge: number; arguments: Record<string, unknown>; calculation?: CalculationBinding
  profile?: never; profileDigest?: never
}
/** Version 3, proposed: §4.4; distinct from a record's preparation binding. */
export interface BindingDocument { dialect: Digest; sources: Record<string, ProfileBinding & {webProfiles?:Partial<Record<'search'|'page',ProfileBinding>>}> }
export type DialectSourceV3 = DialectSourceV2 | HTTPSource | SQLSource | WebSource | ResearchSource | UnboundOperation
export interface Admission {
  // The authoring editor retains unvalidated class names; §8.2 permits only SourceClass.
  facts?: Record<Pointer, string[]>; evidence?: Record<string, string[]>
}
interface DialectMembers {
  case?: DialectCase; admits?: Admission; unmapped?: Pointer[]; unmappedEvidence?: string[]
  provider?: ''; facts?: null; evidence?: null
}
export interface DialectDocumentV2 extends DialectMembers { version: 2; sources?: DialectSourceV2[] }
/** Version 3: portable research, acquisitions and source reconciliation. */
export interface DialectDocumentV3 extends DialectMembers { version: 3; sources?: DialectSourceV3[] }
export type DialectDocument = DialectDocumentV2 | DialectDocumentV3

export type EndpointScope = string[]
export interface InputProfile {
  id: string; publicKey: string; class: 'record' | 'generated'; source: string; authority: string
  shape: 'airbyte' | 'mcp' | 'http' | 'command'
  adapter: { name: string; version: string; digest: Digest }; endpoint: string | null
  endpointScope?: EndpointScope
  tools?: string[]; calculator?: CalculatorPin; research?:{capabilities:ResearchCapability[];model?:string}
}
/** §5.2: receipt internals belong to gateway §1.2a, not the dialect's schema. */
export interface Acquisition { response: { result: JSONValue; receipt: Record<string, JSONValue>; salts: { args: string; statement?: string } } }
export type Acquisitions = Record<string, Acquisition>
export interface Citation { callIndex: number; sessionId: string; signature: string }
export type OutcomeStatus = AcquisitionStatus | 'dependency-unavailable'
export interface Outcome {
  name: string; parameters: Record<string, string | number>; arguments?: Record<string, JSONValue>
  claim?: Claim; status: OutcomeStatus; reason: string
}
export interface Lineage {
  source: string; kind: 'fact' | 'evidence'; target: string; class: SourceClass
  generatedInfluence: boolean; present: boolean; status: OutcomeStatus; reason: string
  basis: Pointer[]; candidateFrom?: Pointer[]; readDigest: Digest
  receipt?: Citation; calculation?: CalculationLineage
}
export const VERIFICATION = 'verified acquisitions; session sealing and input truth are not established' as const
interface PreparationMembers {
  verifiedAt: Timestamp; mappingDigest: Digest; verification: typeof VERIFICATION
  cites: Citation[]; lineage: Lineage[]; outcomes: Outcome[]
}
export interface PreparationV2 extends PreparationMembers { version: 2 }
/** Version 3, proposed: §9.1, for the sources in §§4.3–4.4. */
export interface PreparationV3 extends PreparationMembers { version: 3 }
export type Preparation = PreparationV2 | PreparationV3
export interface PreparationBinding { kind: 'judgment-pack-runner/preparation/1'; digest: Digest }

/** §10 delegates the policy's detailed form to the gateway's engine configuration. */
export interface DecisionPolicy {
  outcomes: string[]; packs?: string[]; reviewed?: boolean; requireSignedRecord?: boolean
  bind: Record<Pointer, Pointer>; fix: Record<Pointer, JSONValue>; closed: boolean
}
export interface OutputDialect { outputVersion: '1'; platform: string; policies: Record<string, DecisionPolicy> }
