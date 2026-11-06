/** Diagnostic values remain decimal strings. They never grant admission. */
export const storageReasons = ['storage-quota-exceeded', 'storage-headroom-low', 'storage-disk-full', 'storage-inodes-low', 'storage-measurement-unknown', 'storage-busy', 'storage-outcome-uncertain', 'storage-writer-unbounded'] as const
export type StorageReason = typeof storageReasons[number]
export const storageCategories = ['history', 'retained', 'attachments', 'memory', 'control', 'disposable', 'binaries', 'transient', 'settlement'] as const
export type StorageCategory = typeof storageCategories[number]
export type StorageVector = { bytes: string; files: string; directories: string }
export type StorageAmounts = {
  known: boolean; chargedLogical: StorageVector | null; promisedPeak: StorageVector; promisedSettlement: StorageVector
  limit: StorageVector | null; logicalRemaining: StorageVector | null; effectiveLogicalRemaining: StorageVector | null; overBudget: boolean | null
}
export type StorageScope = StorageAmounts & { id: string; generation: string }
export type CategoryAmounts = StorageAmounts & { category: StorageCategory }
export type StorageMeasurement = {
 at: string | null; generation: string; freshness: 'fresh' | 'stale' | 'unknown' | 'maintenance'
 logical: {value: StorageVector | null; unknown: '' | 'not-measured' | 'unavailable'}
 basis: 'registered-metadata'; allocationBasis: 'reported-blocks'; sharedBasis: 'workspace-reference-overlap'
}
export type StorageSnapshot = {
 measurement: StorageMeasurement
 physicalAllocated: {value: StorageVector | null; unknown: '' | 'not-measured' | 'unavailable'}
 sharedAttribution: {state: 'measured' | 'unknown' | 'not-measured'; value: StorageVector | null}
 filesystems: {id: string; generation: string; measuredLogical: StorageVector | null; physicalAllocated: StorageVector | null}[]
  observationSession: string; observationSequence: string; observedAt: string; policyRevision: string; epoch: string; maintenance: boolean
  installation: StorageScope; currentWorkspace: StorageScope | null; categories: CategoryAmounts[]; currentWorkspaceCategories: CategoryAmounts[] | null
}
export type ChildRootObservation = {
  component: 'runtime' | 'runner' | 'gateway' | 'native' | 'git'; role: 'audit' | 'signing-metadata' | 'workspace' | 'runtime' | 'connections' | 'handoffs' | 'profile' | 'active-scratch'
  state: 'measured' | 'partial' | 'unavailable' | 'inactive'; reason: 'none' | 'unknown' | 'changed' | 'bounded' | 'shared'
  logicalBytes: string | null; files: string | null; directories: string | null; availableBytes: string | null; lowSpace: boolean | null
}
export type ChildStorageObservation = {
  coverage: 'selected-child-roots-only'; enforcement: 'warning-only'; state: 'measured' | 'partial' | 'stale'; observedAt: string; lowSpaceThresholdBytes: '1073741824'; roots: ChildRootObservation[]
}
export type StorageBudgetStatus = {
  childObservation: ChildStorageObservation | null
  version: 1; availability: 'available' | 'stale' | 'unavailable'; reason: StorageReason | null; readOnly: boolean; servedAt: string
  pressure: {active: boolean; codes: StorageReason[]; firstSeenAt: string | null; lastSeenAt: string | null; durableState: 'unconfirmed' | 'available' | 'unknown'}
  snapshot: StorageSnapshot | null
}
const invalid = () => { throw new Error('Storage status unavailable') }
function object(value: unknown): Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : invalid() }
function bool(value: unknown): boolean { return typeof value === 'boolean' ? value : invalid() }
function choice<T extends string>(value: unknown, values: readonly T[]): T { return typeof value === 'string' && values.includes(value as T) ? value as T : invalid() }
export function storageUInt(value: unknown): string {
  return typeof value === 'string' && /^(0|[1-9][0-9]{0,19})$/.test(value) && BigInt(value) <= 18446744073709551615n ? value : invalid()
}
function id(value: unknown): string { return typeof value === 'string' && /^[a-f0-9]{32}$/.test(value) ? value : invalid() }
function time(value: unknown): string { return typeof value === 'string' && value.length <= 40 && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$/.test(value) && Number.isFinite(Date.parse(value)) ? value : invalid() }
function nullable<T>(value: unknown, read: (v: unknown) => T): T | null { return value === null ? null : read(value) }
function rows<T>(value: unknown, max: number, read: (v: unknown) => T): T[] { return Array.isArray(value) && value.length <= max ? value.map(read) : invalid() }
function vector(value: unknown): StorageVector { const v = closed(value, ['bytes', 'files', 'directories']); return {bytes: storageUInt(v.bytes), files: storageUInt(v.files), directories: storageUInt(v.directories)} }
function amounts(value: unknown): StorageAmounts {
  const v = object(value), known = bool(v.known), chargedLogical = nullable(v.chargedLogical, vector)
  if (known !== (chargedLogical !== null)) invalid()
  const logicalRemaining = nullable(v.logicalRemaining, vector), effectiveLogicalRemaining = nullable(v.effectiveLogicalRemaining, vector), overBudget = nullable(v.overBudget, bool)
  if (!known && (logicalRemaining !== null || effectiveLogicalRemaining !== null || overBudget !== null)) invalid()
  return {known, chargedLogical, promisedPeak: vector(v.promisedPeak), promisedSettlement: vector(v.promisedSettlement), limit: nullable(v.limit, vector), logicalRemaining, effectiveLogicalRemaining, overBudget}
}
function scope(value: unknown): StorageScope { const v = object(value); return {...amounts(v), id: id(v.id), generation: storageUInt(v.generation)} }
function categories(value: unknown): CategoryAmounts[] {
  const result = rows(value, 9, v => { const row = object(v); return {...amounts(row), category: choice(row.category, storageCategories)} })
  if (result.length !== 9 || result.some((row, i) => row.category !== storageCategories[i])) invalid()
  return result
}
function notMeasured(value: unknown) { const v = closed(value, ['value', 'unknown']); if (v.value !== null || v.unknown !== 'not-measured') invalid() }
function measuredVector(value: unknown) {
 const v = closed(value, ['value', 'unknown']), result = {value: nullable(v.value, vector), unknown: choice(v.unknown, ['', 'not-measured', 'unavailable'])}
 if ((result.value !== null) !== (result.unknown === '')) invalid()
 return result
}
function snapshot(value: unknown): StorageSnapshot {
  const v = object(value), m = closed(v.measurement, ['at', 'generation', 'freshness', 'logical', 'basis', 'allocationBasis', 'sharedBasis'])
  if (v.coverage !== 'registered-scope-only' || m.basis !== 'registered-metadata' || m.allocationBasis !== 'reported-blocks' || m.sharedBasis !== 'workspace-reference-overlap') invalid()
  const measurement: StorageMeasurement = {at: nullable(m.at, time), generation: storageUInt(m.generation), freshness: choice(m.freshness, ['fresh', 'stale', 'unknown', 'maintenance']), logical: measuredVector(m.logical), basis: 'registered-metadata', allocationBasis: 'reported-blocks', sharedBasis: 'workspace-reference-overlap'}
  if (measurement.at === null) {
   if (measurement.generation !== '0' || !['unknown', 'maintenance'].includes(measurement.freshness) || measurement.logical.unknown !== 'not-measured') invalid()
  } else if (measurement.generation === '0' || !['fresh', 'stale', 'maintenance'].includes(measurement.freshness) || measurement.logical.unknown === 'not-measured') invalid()
  if (bool(v.maintenance) !== (measurement.freshness === 'maintenance')) invalid()
  if (measurement.freshness === 'fresh' && measurement.logical.value === null) invalid()
  const workspaces = rows(v.workspaces, 1024, scope), roots = rows(v.roots, 4096, scope)
  for (const group of [workspaces, roots]) if (new Set(group.map(row => row.id)).size !== group.length) invalid()
  const filesystems = rows(v.filesystems, 128, value => {
    const row = closed(value, ['id', 'generation', 'chargedLogical', 'completeness', 'measuredLogical', 'physicalAllocated', 'constraintIds'])
    nullable(row.chargedLogical, vector); choice(row.completeness, ['complete', 'partial', 'unknown']); rows(row.constraintIds, 4, id)
    const measuredLogical = nullable(row.measuredLogical, vector), physicalAllocated = nullable(row.physicalAllocated, vector)
    if (measurement.at === null && (measuredLogical !== null || physicalAllocated !== null)) invalid()
    return {id: id(row.id), generation: storageUInt(row.generation), measuredLogical, physicalAllocated}
  })
  if (new Set(filesystems.map(row => row.id)).size !== filesystems.length) invalid()
  rows(v.constraints, 512, value => {
    const row = object(value); id(row.id); storageUInt(row.generation)
    const resources = rows(row.resources, 2, v => choice(v, ['bytes', 'inodes']))
    if (!resources.length || new Set(resources).size !== resources.length) invalid()
    for (const key of ['headroom', 'promisedPeak']) { const a = object(row[key]); storageUInt(a.bytes); storageUInt(a.inodes) }
    if (row.available !== null || row.remaining !== null || row.observedAt !== null || row.freshness !== 'unknown' || row.basis !== 'declared-policy-demand') invalid()
  })
  notMeasured(v.retainedLogical)
  const physicalAllocated = measuredVector(v.physicalAllocated), shared = closed(v.sharedAttribution, ['state', 'value']), reclaimable = closed(v.reclaimable, ['state', 'eligibleLogical', 'allocatedEstimate']), recovery = object(v.recovery)
  const sharedAttribution = {state: choice(shared.state, ['measured', 'unknown', 'not-measured']), value: nullable(shared.value, vector)}
  if ((sharedAttribution.state === 'measured') !== (sharedAttribution.value !== null)) invalid()
  if (measurement.at === null ? physicalAllocated.unknown !== 'not-measured' || sharedAttribution.state !== 'not-measured' : physicalAllocated.unknown === 'not-measured' || sharedAttribution.state === 'not-measured') invalid()
  if (reclaimable.state !== 'not-measured' || reclaimable.eligibleLogical !== null || reclaimable.allocatedEstimate !== null || recovery.pendingRecoveryCount !== null || recovery.pendingRecoveryState !== 'not-measured') invalid()
  let total = 0
  for (const key of ['uncertainTicketCount', 'activeTicketCount', 'terminalAwaitingCheckpointCount']) {
    const count = recovery[key]; if (typeof count !== 'number' || !Number.isInteger(count) || count < 0 || count > 64) invalid()
    total += count as number
  }
  if (total > 64 || measurement.freshness === 'fresh' && (total !== 0 || measurement.generation !== storageUInt(v.epoch))) invalid()
  const currentWorkspace = nullable(v.currentWorkspace, scope), currentWorkspaceCategories = nullable(v.currentWorkspaceCategories, categories)
  if ((currentWorkspace === null) !== (currentWorkspaceCategories === null)) invalid()
  if (currentWorkspace && !workspaces.some(row => JSON.stringify(row) === JSON.stringify(currentWorkspace))) invalid()
  return {measurement, physicalAllocated, sharedAttribution, filesystems, observationSession: id(v.observationSession), observationSequence: storageUInt(v.observationSequence), observedAt: time(v.observedAt), policyRevision: storageUInt(v.policyRevision), epoch: storageUInt(v.epoch), maintenance: bool(v.maintenance), installation: scope(v.installation), categories: categories(v.categories), currentWorkspace, currentWorkspaceCategories}
}
function closed(value: unknown, keys: readonly string[]): Record<string, unknown> {
  const v = object(value)
  if (Object.keys(v).length !== keys.length || keys.some(key => !Object.prototype.hasOwnProperty.call(v, key))) invalid()
  return v
}
function childObservation(value: unknown): ChildStorageObservation {
  const v = closed(value, ['coverage', 'enforcement', 'state', 'observedAt', 'lowSpaceThresholdBytes', 'roots'])
  if (v.coverage !== 'selected-child-roots-only' || v.enforcement !== 'warning-only' || v.lowSpaceThresholdBytes !== '1073741824') invalid()
  const state = choice(v.state, ['measured', 'partial', 'stale'])
  const roots = rows(v.roots, 64, value => {
    const r = closed(value, ['component', 'role', 'state', 'reason', 'logicalBytes', 'files', 'directories', 'availableBytes', 'lowSpace'])
    const component = choice(r.component, ['runtime', 'runner', 'gateway', 'native', 'git']), role = choice(r.role, ['audit', 'signing-metadata', 'workspace', 'runtime', 'connections', 'handoffs', 'profile', 'active-scratch'])
    const validRoles = {runtime: ['audit', 'signing-metadata'], runner: ['workspace'], gateway: ['runtime', 'connections', 'handoffs'], native: ['profile'], git: ['active-scratch']}
    if (!validRoles[component].includes(role)) invalid()
    const state = choice(r.state, ['measured', 'partial', 'unavailable', 'inactive']), reason = choice(r.reason, ['none', 'unknown', 'changed', 'bounded', 'shared'])
    const logicalBytes = nullable(r.logicalBytes, storageUInt), files = nullable(r.files, storageUInt), directories = nullable(r.directories, storageUInt), availableBytes = nullable(r.availableBytes, storageUInt), lowSpace = nullable(r.lowSpace, bool)
    if ((logicalBytes === null) !== (files === null) || (files === null) !== (directories === null) || (availableBytes === null) !== (lowSpace === null)) invalid()
    if (state === 'measured' && (logicalBytes === null || reason !== 'none')) invalid()
    if (state === 'inactive' && (reason !== 'none' || logicalBytes !== null || availableBytes !== null)) invalid()
    if (state === 'unavailable' && (logicalBytes !== null || availableBytes !== null || reason === 'none')) invalid()
    if (state === 'partial' && reason === 'none') invalid()
    if (availableBytes !== null && lowSpace !== (BigInt(availableBytes) < 1073741824n)) invalid()
    return {component, role, state, reason, logicalBytes, files, directories, availableBytes, lowSpace}
  })
  if (!roots.length || state === 'measured' && roots.some(r => r.state === 'partial' || r.state === 'unavailable')) invalid()
  const identities = roots.filter(r => r.component !== 'git' && r.component !== 'native').map(r => `${r.component}:${r.role}`)
  if (new Set(identities).size !== identities.length) invalid()
  return {coverage: 'selected-child-roots-only', enforcement: 'warning-only', state, observedAt: time(v.observedAt), lowSpaceThresholdBytes: '1073741824', roots}
}
export function decodeStorageBudgetStatus(value: unknown): StorageBudgetStatus {
  const v = object(value), p = object(v.pressure)
  if (v.version !== 1) invalid()
  const availability = choice(v.availability, ['available', 'stale', 'unavailable']), data = nullable(v.snapshot, snapshot)
  if ((availability === 'unavailable') !== (data === null)) invalid()
  const codes = rows(p.codes, 8, v => choice(v, storageReasons))
  if (new Set(codes).size !== codes.length || bool(p.active) !== (codes.length > 0)) invalid()
  return {childObservation: v.childObservation === undefined || v.childObservation === null ? null : childObservation(v.childObservation), version: 1, availability, snapshot: data, reason: nullable(v.reason, value => choice(value, storageReasons)), readOnly: bool(v.readOnly), servedAt: time(v.servedAt), pressure: {active: bool(p.active), codes, firstSeenAt: nullable(p.firstSeenAt, time), lastSeenAt: nullable(p.lastSeenAt, time), durableState: choice(p.durableState, ['unconfirmed', 'available', 'unknown'])}}
}
export const STORAGE_STATUS_MAX_BYTES = 8_388_608
/** Bound bytes before decoding JSON; a large/malformed reply cannot erase cached rows. */
export async function readStorageBudgetStatus(response: Response): Promise<StorageBudgetStatus> {
  if (!response.ok || !response.body) { await response.body?.cancel(); return invalid() }
  const reader = response.body.getReader(), bytes = new Uint8Array(STORAGE_STATUS_MAX_BYTES)
  let size = 0
  try {
    for (;;) {
      const chunk = await reader.read()
      if (chunk.done) break
      if (chunk.value.byteLength > STORAGE_STATUS_MAX_BYTES - size) { await reader.cancel(); return invalid() }
      bytes.set(chunk.value, size)
      size += chunk.value.byteLength
    }
  } finally { reader.releaseLock() }
  try { return decodeStorageBudgetStatus(JSON.parse(new TextDecoder('utf-8', {fatal: true}).decode(bytes.subarray(0, size)))) } catch { return invalid() }
}
export function retainStorageSnapshot(next: StorageBudgetStatus, previous?: StorageBudgetStatus): StorageBudgetStatus {
  return next.snapshot === null && previous?.snapshot ? {...next, availability: 'stale', snapshot: previous.snapshot} : next
}
/** Exact integer grouping, without converting a counter through Number. */
export function formatStorageUInt(value: string): string { return storageUInt(value).replace(/\B(?=(\d{3})+(?!\d))/g, ',') }
