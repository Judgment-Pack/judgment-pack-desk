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

/**
 * The eight `$defs` that may carry `extensions` do so on the same terms as the
 * root: a namespaced object the spec does not interpret, which the runtime
 * carries and this desk renders rather than drops. Mirrored from the bundled
 * schema at `internal/artifacts/jps/0.2.0-draft/schema.json` — `decision`,
 * `evidenceRequirement`, `source`, `outcome`, `rule`, `exception`,
 * `escalation` and `metadata`, plus the root.
 */
export type Extensions = Record<string, unknown>

export interface Decision {
  intent: string
  question: string
  extensions?: Extensions
}

export interface Outcome {
  id: string
  label: string
  description?: string
  extensions?: Extensions
}

export interface EvidenceRequirement {
  id: string
  description: string
  required: boolean
  kind?: string
  extensions?: Extensions
}

export interface Source {
  id: string
  title: string
  publisher?: string
  publishedAt?: string
  locator: { kind: string; value: string }
  citation?: { location: string; excerpt: string }
  rights?: string
  extensions?: Extensions
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
  extensions?: Extensions
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
  extensions?: Extensions
}

export interface Escalation {
  triggers: string[]
  target: { kind: string; name: string }
  message?: string
  extensions?: Extensions
}

/**
 * One entry of `metadata.reviews`.
 *
 * The schema requires `reviewer`, `reviewedAt` and `disposition`, and the
 * disposition is one of exactly three words. The desk **renders** these and
 * never writes one: this surface has no reviewer identity, so a review it
 * wrote would be signed by nobody.
 */
export interface PackReview {
  reviewer: string
  reviewedAt: string
  disposition: 'approved' | 'changes-requested' | 'rejected'
  note?: string
}

export interface PackMetadata {
  authors?: string[]
  createdAt?: string
  license?: string
  requiredExtensions?: string[]
  reviews?: PackReview[]
  extensions?: Extensions
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

/* Evaluation --------------------------------------------------------------- */

/**
 * The payload the runtime's `experimental_evaluate` tool returns.
 *
 * Every member below is one that tool emits; nothing here is invented, and the
 * views render only what a payload actually carries. The surface is
 * experimental and may change or be removed without a compatibility promise —
 * which is why the payload carries `experimental` and the views say so.
 */

/** The JPS Core §8.3 handoff object, carried inside the disposition. */
export interface Handoff {
  state: string
  triggeredBy?: string[]
}

/**
 * The §8.3 portable disposition: the authoritative part of the payload. It
 * carries these members and no others, and `outcomeId` is present exactly when
 * `kind` is `outcome`.
 */
export interface Disposition {
  kind: string
  outcomeId?: string
  reasons: string[]
  handoff: Handoff
}

/**
 * Where the pack configures a handoff to go. §8.3 keeps it outside the
 * disposition and this runtime reports it beside one, so the views show it
 * beside the disposition and never inside it. No delivery is observed.
 */
export interface HandoffTarget {
  kind: string
  name: string
}

/**
 * One applicability, exception, or rule evaluation. The trace is informative:
 * it exists so that an unknown resolution ignored stays visible. A pack's
 * applicability is one unnamed condition, so its entry carries no `id`.
 */
export interface TraceEntry {
  stage: string
  id?: string
  condition: string
  effect?: string
  outcome?: string
  suppressed?: boolean
  onUnknown?: string
  skipped?: boolean
}

/** Present exactly when the evaluation ran under a draft-RFC grammar. */
export interface DraftPrototype {
  rfc: string
  status: string
  operators: string[]
  packValidUnderSpecVersion: boolean
  note: string
}

/** The bundled specification artifacts the evaluation ran against. */
export interface EvaluationArtifact {
  specVersion: string
  bundleDigest: string
  provenance: string
}

export interface Evaluation {
  outputVersion: string
  tool: { name: string; version: string }
  command: string
  status: string
  experimental: boolean
  /**
   * Present exactly when the run was declared a rehearsal (ADR-0028): no audit
   * record was appended and no reviewed set was consulted, and the payload says
   * so in band. Absent on runtimes that predate the declaration.
   */
  rehearsal?: boolean
  /** A locator for the file that states the claim; not itself a claim. */
  conformanceClaimReference: string
  /** The version the evaluated pack declares. */
  specVersion: string
  /** The version of the evaluator contract applied to it. */
  evaluatorSpecVersion: string
  packId: string
  packVersion: string
  draftPrototype?: DraftPrototype
  disposition: Disposition
  handoffTarget?: HandoffTarget
  trace: TraceEntry[]
  artifact?: EvaluationArtifact
}

/**
 * The §8.4 envelope a refused evaluation reports. A refusal carries no
 * disposition at all, so nothing in it may be read as an answer.
 */
export interface EvaluationErrorEnvelope {
  class: string
  phase: string
  evaluatorSpecVersion: string
}

export interface Diagnostic {
  code?: string
  codeStability?: string
  layer?: string
  severity?: string
  instancePath?: string
  message?: string
}

/**
 * One layer of the validation ladder, as the payload reports it
 * (`internal/result/result.go:68-71`).
 *
 * **A layer the payload does not list is one that did not run.** The ladder
 * short-circuits: a carrier failure returns `[carrier failed]` alone, a
 * structural failure appends `structural failed` and returns before semantic,
 * and semantic runs only after structural passed. So the rows are the whole of
 * what may be said about which layers ran, and nothing may be assumed from a
 * `status` word alone.
 */
export interface ValidationLayer {
  name?: string
  status?: string
}

/** Which layers the caller asked for, and whether the whole document was in scope. */
export interface ValidationScope {
  requestedThrough?: string
  fullDocumentConformance?: boolean
}

/** The extensions a document required, and which of them this runtime knows. */
export interface ValidationExtensions {
  required?: string[]
  supported?: string[]
  unsupported?: string[]
}

/** The bundled specification artifacts a check ran against. */
export interface ValidationArtifact {
  specVersion?: string
  bundleDigest?: string
  provenance?: string
}

/**
 * The `validate` payload (`internal/result/result.go:90-101`).
 *
 * Every member is optional on read. The desk is built against one runtime and
 * must render an older runtime's answer without inventing members it did not
 * send: an absent `layers` is "this runtime said nothing about layers", which
 * is not "no layer ran".
 *
 * `diagnosticsTruncated` means the runtime stopped at its own limit
 * (`internal/validation/validator.go:21`, `MaxDiagnostics = 100`), so the list
 * is not all of them and "no diagnostic names this member" is unsafe to say.
 */
export interface ValidationReport {
  outputVersion?: string
  tool?: { name: string; version: string }
  command?: string
  status?: string
  specVersion?: string
  validationScope?: ValidationScope
  layers?: ValidationLayer[]
  extensions?: ValidationExtensions
  diagnostics?: Diagnostic[]
  diagnosticsTruncated?: boolean
  artifact?: ValidationArtifact
}

/** The structured content the runtime returns beside a refusal message. */
export interface RefusalEnvelope {
  status?: string
  command?: string
  evaluationError?: EvaluationErrorEnvelope
  diagnostics?: Diagnostic[]
}

/** One completed run: the payload, and the documents that produced it. */
export interface EvaluationRun {
  payload: Evaluation
  /** The payload exactly as the runtime served it, for the Raw JSON tab. */
  raw: string
  facts: string
  /** Absent when the run supplied no evidence document at all. */
  evidence?: string
}

/* Matrices and coverage ----------------------------------------------------- */

/**
 * The payloads `experimental_test_packs` and `experimental_test_graphs` return.
 *
 * Both surfaces are experimental and share a vocabulary deliberately: the same
 * `summary` counts, the same probe record, and rows whose expected and actual
 * dispositions are *strings* — the RFC 8785 canonical bytes the comparator
 * compared, not objects. The views parse them to render structure and diff the
 * strings for the verdict, because byte equality is what decided the row.
 */

/** The row counts a suite reports. There is no skipped count: see `status`. */
export interface SuiteSummary {
  total: number
  passed: number
  mismatched: number
}

/**
 * One derived coverage probe (ADR-0014, ADR-0016, ADR-0023).
 *
 * `status` is `covered` or `missing` and never a third value: a behaviour the
 * declarations cannot reach is not derived at all, because "skipped" in this
 * field would be a reachability claim. A missing probe is therefore a fact
 * about what the rows *state*, and `detail` is the runtime's own sentence
 * naming what none of them said. Coverage informs; it never gates.
 */
export interface MatrixProbe {
  probe: string
  status: string
  detail?: string
}

/**
 * One pack matrix row's result.
 *
 * `expected` and `actual` hold canonical JSON *text*, so a view that wants the
 * disposition's members parses them. `expectedHandoffTarget` and
 * `actualHandoffTarget` (ADR-0025) appear together exactly when the row asserts
 * a handoff-target state — a row that omits the assertion carries neither,
 * while a row asserting "no target at all" carries both, each as the literal
 * `null` — and each is a canonical target rendering, that literal `null`, or
 * `unavailable` where the report cannot state one. They are display values a
 * view must never compare: the comparator decides equality on decoded targets,
 * because a long rendering is truncated with a digest tail.
 */
export interface MatrixRow {
  id: string
  origin?: string
  specSection?: string
  packId?: string
  packVersion?: string
  status: string
  expected: string
  actual: string
  expectedErrorClass?: string
  actualErrorClass?: string
  expectedErrorPhase?: string
  actualErrorPhase?: string
  expectedHandoffTarget?: string
  actualHandoffTarget?: string
  detail?: string
}

/** How many of one pack's rows declare one origin (ADR-0024). Moves no status. */
export interface OriginCount {
  origin: string
  rows: number
}

/** One pack's matrix run inside the project walk. */
export interface PackTestEntry {
  id: string
  packId?: string
  packVersion?: string
  path?: string
  matrixPath?: string
  status: string
  summary: SuiteSummary
  rows?: MatrixRow[]
  coverage?: MatrixProbe[]
  origins?: OriginCount[]
  detail?: string
}

/** The `experimental_test_packs` payload: the project's declared matrices. */
export interface PackTest {
  outputVersion?: string
  tool?: { name: string; version: string }
  command?: string
  /** `passed`, `mismatch`, or `skipped`. Zero rows is never `passed`. */
  status: string
  experimental?: boolean
  evaluatorSpecVersion?: string
  conformanceClaimReference?: string
  label?: string
  kind?: string
  configPath?: string
  configVersion?: string
  summary: SuiteSummary
  packs?: PackTestEntry[]
}

/**
 * One node comparison a graph row asked for. A row names the nodes it checks.
 *
 * `trace` (ADR-0031) is that node evaluation's own trace under ADR-0027's
 * pinned contract, present exactly when the run was *asked* for traces and the
 * walk evaluated this node — `[]` at minimum, never `null`, so an empty array
 * is a trace with no entries and absence is "not asked, or not evaluated". A
 * comparison naming a node the graph does not declare was never evaluated and
 * carries none even when asked; a row that failed before node comparisons
 * exist carries no comparisons at all.
 *
 * The comparisons are listed **lexicographically by node name** and each trace
 * is **walk-ordered** inside itself. Those are two different orders on
 * purpose: the report's, and the evaluator's. Neither may be read off the
 * other, and nothing here reorders either.
 *
 * `expectedHandoffTarget` and `actualHandoffTarget` (ADR-0032) are **one pair**
 * spelled as two flat optionals, because the wire types mirror the JSON and the
 * JSON carries them flat. The rule the pair obeys is that the two appear
 * *together*, exactly when the row's well-formed assertion named a node this
 * run performed; a row whose assertion was itself defective — undecodable, or
 * naming a node the graph does not declare — reports that defect in its detail
 * and carries no pair here. Read them through `handoffTargetPair`, which is the
 * one place that rule is applied.
 *
 * On a node comparison the values are a capped rendering or the literal `null`,
 * and never `unavailable`: a comparison exists only because the walk evaluated
 * the node, so there is always a reported target or an honest absence of one.
 * `unavailable` belongs to the row, where a refused run leaves nothing to
 * state. Neither value is ever an equality key — the comparator decided on
 * decoded values, and this client only shows what it decided about.
 */
export interface GraphTestNode {
  node: string
  status: string
  expected: string
  actual: string
  expectedHandoffTarget?: string
  actualHandoffTarget?: string
  trace?: TraceEntry[]
}

/**
 * One graph matrix row: the composite headline, and the nodes the row named.
 *
 * `expectedHandoffTarget` and `actualHandoffTarget` (ADR-0032) are the
 * composite's pair, on the pack row's own vocabulary and semantics: present
 * together exactly when a **well-formed** assertion rode a run this walk
 * **performed**, and absent where a row defect is reported in `detail`
 * instead. The composite's target is the result node's own — the value that
 * run reported beside the disposition the headline compares, which is a named
 * target exactly when that disposition requested a handoff and the literal
 * `null` otherwise.
 *
 * This row is the only carrier on which `unavailable` is reachable: a run
 * refused where the row expected a disposition leaves no target to state. A
 * §8.4-classed refusal sets `actualErrorClass` beside it; a graph-layer
 * refusal carries no class at all and is told by the `detail`.
 *
 * A composite **target** mismatch is decided after the headline and before the
 * node comparisons are built, so a row that fails on the target alone reports
 * no `nodes` — the two are not seen together on one row.
 */
export interface GraphTestRow {
  id: string
  status: string
  expected: string
  actual: string
  expectedErrorClass?: string
  actualErrorClass?: string
  expectedErrorPhase?: string
  actualErrorPhase?: string
  expectedHandoffTarget?: string
  actualHandoffTarget?: string
  nodes?: GraphTestNode[]
  detail?: string
}

/**
 * One configured graph's matrix run inside the project walk.
 *
 * `graphSha256` (ADR-0030) is bare hex — the payload-member convention, not the
 * `sha256:` prefixed spelling the lock and audit records use — and it is the
 * digest of the exact bytes *this run* decoded. It is present exactly when the
 * document loaded: an entry whose document could not be read carries none,
 * beside the `detail` that says why, while a rows failure *after* a successful
 * load keeps it, because the bytes the digest names did load.
 *
 * What it enables is a binding and not a verdict. Equality with the `sha256`
 * `experimental_get_graph` reports beside a served document proves the two
 * answers describe one revision; inequality proves the file was edited between
 * the two calls. Neither says anything about whether either revision is any
 * good. Absent on jpack 0.18.0 and older, where nothing binds the two at all.
 */
export interface GraphSuiteEntry {
  id: string
  path?: string
  rowsPath?: string
  graphId?: string
  graphVersion?: string
  graphSha256?: string
  status: string
  summary: SuiteSummary
  rows?: GraphTestRow[]
  coverage?: MatrixProbe[]
  detail?: string
}

/**
 * One inventory row from `experimental_list_graphs` (ADR-0029).
 *
 * `id` is the configured id and `graphId` is the document's own; they are two
 * members because they are two names, exactly as a `PackSummary` reports them.
 * Listing is not validating: a document that could not be read or decoded is
 * still a row, with `detail` saying why and the identity members *empty rather
 * than guessed* — so an empty `graphId` means "not read off the bytes", never
 * "the document declares an empty id".
 *
 * `nodeCount` and `edgeCount` exist exactly when identity decoding succeeded
 * and each member had its declared shape, and are absent — never zero —
 * otherwise, so a malformed document cannot look honestly empty. A view must
 * therefore distinguish absent from 0 and never coerce.
 */
export interface GraphSummary {
  id: string
  graphId: string
  graphVersion: string
  formatVersion: string
  resultNode?: string
  path: string
  rowsPath?: string
  rowsDeclared: boolean
  description?: string
  nodeCount?: number
  edgeCount?: number
  detail?: string
}

/**
 * The `experimental_list_graphs` payload (ADR-0029).
 *
 * A project with no configuration answers `none` with a `note` saying where
 * the runtime looked, rather than refusing: an absent configuration is an
 * answer on this surface.
 */
export interface GraphInventory {
  outputVersion?: string
  tool?: { name: string; version: string }
  command?: string
  status: string
  experimental?: boolean
  kind?: string
  configPath?: string
  configVersion?: string
  note?: string
  graphs?: GraphSummary[]
}

/**
 * The metadata half of `experimental_get_graph` — the structured content
 * beside the served bytes (ADR-0029).
 *
 * `status` is `valid` when the served bytes decoded and the identity members
 * were read off them, and `undecodable` when they did not, in which case
 * `detail` says why and those members are empty rather than guessed. Neither
 * value is a verdict against the graph schema: serving is not validating.
 *
 * `formatVersion` here is the version the *document* declares, not the format
 * version a walk applied — the member means the latter on an evaluation
 * payload, and the two must not be read as one.
 *
 * `sha256` is bare hex, the payload-member convention, not the `sha256:`
 * prefixed digest the lock and audit records use.
 */
export interface GraphDocumentMeta {
  outputVersion?: string
  tool?: { name: string; version: string }
  command?: string
  status: string
  experimental?: boolean
  kind?: string
  configPath?: string
  id: string
  graphId: string
  graphVersion: string
  formatVersion: string
  resultNode?: string
  path: string
  rowsPath?: string
  description?: string
  bytes: number
  sha256: string
  detail?: string
}

/** One node of a served graph document: a reference to one pack by decision id. */
export interface GraphDocumentNode {
  pack: string
  description?: string
}

/**
 * One edge's outcome-as-evidence device: the downstream requirement id, and
 * the tri-state an upstream disposition that is *not* an outcome contributes.
 */
export interface GraphDocumentEvidenceFeed {
  id: string
  onUnresolved?: string
}

/**
 * One edge of a served graph document. It feeds an upstream node's disposition
 * to a downstream node as a fact, as evidence availability, or both — the
 * format requires at least one of the two devices.
 */
export interface GraphDocumentEdge {
  from: string
  to: string
  fact?: string
  evidence?: GraphDocumentEvidenceFeed
  description?: string
}

/**
 * A served graph document, as the text half of `experimental_get_graph`
 * carries it. This is the project's own file, unaltered — the one artifact
 * that states the composition.
 */
export interface GraphDocument {
  formatVersion?: string
  id?: string
  version?: string
  description?: string
  nodes?: Record<string, GraphDocumentNode>
  edges?: GraphDocumentEdge[]
  result?: string
}

/**
 * A served graph document this client has read: every member the views draw
 * from, checked to be what the format declares it to be.
 *
 * `nodes` and `edges` are required here where they are optional above, because
 * a document missing either is one the reader declines rather than one a view
 * renders with a gap. Reading is what turns the first type into this one; no
 * cast may, which is the point of there being two.
 */
export interface ReadGraphDocument extends GraphDocument {
  nodes: Record<string, GraphDocumentNode>
  edges: GraphDocumentEdge[]
}

/**
 * One `experimental_get_graph` answer: the exact bytes, the metadata beside
 * them, and this client's own read of those bytes where it made one.
 *
 * The read is subordinate to the metadata, never a second opinion on it.
 * `meta.status` is the runtime's verdict on its own decode and it is the
 * stricter reader of the two, so a document it reports `undecodable` is never
 * read here — `document` is undefined and `unreadable` carries the runtime's
 * own sentence. `document` is undefined too where the runtime decoded the bytes
 * and they did not carry what the views draw from, and `unreadable` then says
 * which member. Exactly one of the two is ever present.
 */
export interface ServedGraph {
  meta: GraphDocumentMeta
  raw: string
  document?: ReadGraphDocument
  /** Why no document was read, where none was. */
  unreadable?: string
}

/** The `experimental_test_graphs` payload: the project's configured graphs. */
export interface GraphSuite {
  outputVersion?: string
  tool?: { name: string; version: string }
  command?: string
  status: string
  experimental?: boolean
  conformanceClaimReference?: string
  label?: string
  kind?: string
  /** The graph format's version, which is not the graph document's version. */
  formatVersion?: string
  evaluatorSpecVersion?: string
  configPath?: string
  configVersion?: string
  summary: SuiteSummary
  graphs?: GraphSuiteEntry[]
}
