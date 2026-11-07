import {expect, it} from 'vitest'
import {decodeStorageBudgetStatus, formatStorageUInt, readStorageBudgetStatus, retainStorageSnapshot, storageUInt, STORAGE_STATUS_MAX_BYTES} from './storageBudgetModel'
import {storageStatusFixture, storageMeasuredFixture} from './storageBudgetModel.fixture'
import measurementVectors from './storageOwnedMeasurement.vectors.json'
it('preserves exact uint64 decimal counters and distinguishes measured zero from unknown', () => {
  const status = decodeStorageBudgetStatus(storageStatusFixture())
  expect(status.snapshot?.installation.chargedLogical?.bytes).toBe('9007199254740993')
  expect(status.snapshot?.policyRevision).toBe('18446744073709551615')
  expect(formatStorageUInt('18446744073709551615')).toBe('18,446,744,073,709,551,615')
  expect(status.snapshot?.installation.logicalRemaining?.bytes).toBe('0')
  const unknown = storageStatusFixture()
  const installation = unknown.snapshot.installation as Record<string, unknown>
  installation.known = false; installation.chargedLogical = null; installation.logicalRemaining = null; installation.effectiveLogicalRemaining = null; installation.overBudget = null
  expect(decodeStorageBudgetStatus(unknown).snapshot?.installation.chargedLogical).toBeNull()
})
it.each(['18446744073709551616', '01', '-1', '1.0', '1e3', '', 9007199254740992])('rejects invalid decimal %s', value => {
  expect(() => storageUInt(value)).toThrow('Storage status unavailable')
})
it('rejects unknown pressure codes, unordered categories and oversized collections', () => {
  const code = storageStatusFixture(); code.pressure.codes = ['unreviewed-action']
  expect(() => decodeStorageBudgetStatus(code)).toThrow()
  const category = storageStatusFixture(); category.snapshot.categories.reverse()
  expect(() => decodeStorageBudgetStatus(category)).toThrow()
  const rows = storageStatusFixture(); rows.snapshot.workspaces = Array(1025).fill(rows.snapshot.currentWorkspace)
  expect(() => decodeStorageBudgetStatus(rows)).toThrow()
})
it('bounds streamed response bytes before JSON decoding and cancels the oversized stream', async () => {
  let cancelled = false
  const body = new ReadableStream<Uint8Array>({start(controller) {controller.enqueue(new Uint8Array(STORAGE_STATUS_MAX_BYTES)); controller.enqueue(new Uint8Array(1))}, cancel() {cancelled = true}})
  await expect(readStorageBudgetStatus(new Response(body))).rejects.toThrow()
  expect(cancelled).toBe(true)
})
it('retains the original as-of on unavailable but replaces successful identities wholesale', () => {
  const first = decodeStorageBudgetStatus(storageStatusFixture())
  const unavailable = decodeStorageBudgetStatus({...storageStatusFixture(), availability: 'unavailable', snapshot: null, reason: 'storage-busy'})
  const retained = retainStorageSnapshot(unavailable, first)
  expect(retained.availability).toBe('stale'); expect(retained.snapshot).toBe(first.snapshot)
  expect(retained.snapshot?.observedAt).toBe('2026-10-31T12:00:00Z')
  const next = storageStatusFixture(); next.snapshot.observationSession = '44444444444444444444444444444444'; next.snapshot.currentWorkspace.id = '55555555555555555555555555555555'
  const decoded = decodeStorageBudgetStatus(next)
  expect(retainStorageSnapshot(decoded, first)).toBe(decoded)
  expect(retainStorageSnapshot(unavailable).snapshot).toBeNull()
})

function unknownAmounts(row: Record<string, unknown>) {
  row.known = false; row.chargedLogical = null; row.logicalRemaining = null; row.effectiveLogicalRemaining = null; row.overBudget = null
}
it('refuses logical remaining when installation charged usage is unknown', () => {
  const value = storageStatusFixture(), row = value.snapshot.installation as Record<string, unknown>
  unknownAmounts(row); row.logicalRemaining = {bytes:'1', files:'0', directories:'0'}
  expect(() => decodeStorageBudgetStatus(value)).toThrow('Storage status unavailable')
})
it('refuses effective remaining when installation charged usage is unknown', () => {
  const value = storageStatusFixture(), row = value.snapshot.installation as Record<string, unknown>
  unknownAmounts(row); row.effectiveLogicalRemaining = {bytes:'1', files:'0', directories:'0'}
  expect(() => decodeStorageBudgetStatus(value)).toThrow('Storage status unavailable')
})
it('refuses over-budget false when installation charged usage is unknown', () => {
  const value = storageStatusFixture(), row = value.snapshot.installation as Record<string, unknown>
  unknownAmounts(row); row.overBudget = false
  expect(() => decodeStorageBudgetStatus(value)).toThrow('Storage status unavailable')
})
it('refuses over-budget true when installation charged usage is unknown', () => {
  const value = storageStatusFixture(), row = value.snapshot.installation as Record<string, unknown>
  unknownAmounts(row); row.overBudget = true
  expect(() => decodeStorageBudgetStatus(value)).toThrow('Storage status unavailable')
})
it('refuses definite dependent fields in unknown category rows', () => {
  const logical = storageStatusFixture(), logicalRow = logical.snapshot.categories[0] as Record<string, unknown>
  unknownAmounts(logicalRow); logicalRow.logicalRemaining = {bytes:'1', files:'0', directories:'0'}
  expect(() => decodeStorageBudgetStatus(logical)).toThrow()
  const effective = storageStatusFixture(), effectiveRow = effective.snapshot.categories[0] as Record<string, unknown>
  unknownAmounts(effectiveRow); effectiveRow.effectiveLogicalRemaining = {bytes:'1', files:'0', directories:'0'}
  expect(() => decodeStorageBudgetStatus(effective)).toThrow()
  const under = storageStatusFixture(), underRow = under.snapshot.categories[0] as Record<string, unknown>
  unknownAmounts(underRow); underRow.overBudget = false
  expect(() => decodeStorageBudgetStatus(under)).toThrow()
  const over = storageStatusFixture(), overRow = over.snapshot.categories[0] as Record<string, unknown>
  unknownAmounts(overRow); overRow.overBudget = true
  expect(() => decodeStorageBudgetStatus(over)).toThrow()
})
it('accepts null dependent fields in unknown installation and category rows', () => {
  const value = storageStatusFixture()
  unknownAmounts(value.snapshot.installation); unknownAmounts(value.snapshot.categories[0])
  const decoded = decodeStorageBudgetStatus(value)
  expect(decoded.snapshot?.installation.logicalRemaining).toBeNull()
  expect(decoded.snapshot?.installation.effectiveLogicalRemaining).toBeNull()
  expect(decoded.snapshot?.installation.overBudget).toBeNull()
  expect(decoded.snapshot?.categories[0].logicalRemaining).toBeNull()
  expect(decoded.snapshot?.categories[0].effectiveLogicalRemaining).toBeNull()
  expect(decoded.snapshot?.categories[0].overBudget).toBeNull()
})
it('accepts known workspace residuals with unknown installation effective remaining', () => {
  const value = storageStatusFixture()
  unknownAmounts(value.snapshot.installation)
  const workspace = value.snapshot.currentWorkspace as Record<string, unknown>
  workspace.effectiveLogicalRemaining = null
  const decoded = decodeStorageBudgetStatus(value)
  expect(decoded.snapshot?.currentWorkspace?.known).toBe(true)
  expect(decoded.snapshot?.currentWorkspace?.logicalRemaining).toEqual({bytes:'0', files:'0', directories:'0'})
  expect(decoded.snapshot?.currentWorkspace?.effectiveLogicalRemaining).toBeNull()
  expect(decoded.snapshot?.currentWorkspace?.overBudget).toBe(true)
})
it('decodes the independently handwritten nine-category wire order', () => {
  const decoded = decodeStorageBudgetStatus(storageStatusFixture())
  expect(decoded.snapshot?.categories.map(row => row.category)).toEqual(['history', 'retained', 'attachments', 'memory', 'control', 'disposable', 'binaries', 'transient', 'settlement'])
  expect(decoded.snapshot?.currentWorkspaceCategories?.map(row => row.category)).toEqual(['history', 'retained', 'attachments', 'memory', 'control', 'disposable', 'binaries', 'transient', 'settlement'])
})

// Handwritten display vectors. They confer no native qualification or admission.
type ChildWireRoot = {
  component: string; role: string; state: string; reason: string
  logicalBytes: string | null; files: string | null; directories: string | null; availableBytes: string | null; lowSpace: boolean | null
}
function childVector(): {coverage: string; enforcement: string; state: string; observedAt: string; lowSpaceThresholdBytes: string; roots: ChildWireRoot[]} {
  return {coverage: 'selected-child-roots-only', enforcement: 'warning-only', state: 'partial', observedAt: '2026-11-01T12:00:00Z', lowSpaceThresholdBytes: '1073741824', roots: [
    {component: 'runner', role: 'workspace', state: 'measured', reason: 'none', logicalBytes: '9007199254740993', files: '1', directories: '1', availableBytes: '0', lowSpace: true},
    {component: 'native', role: 'profile', state: 'unavailable', reason: 'unknown', logicalBytes: null, files: null, directories: null, availableBytes: null, lowSpace: null},
  ]}
}
it('decodes warning-only child observations independently of unavailable Desk accounting', () => {
  const childObservation = childVector()
  const status = decodeStorageBudgetStatus({...storageStatusFixture(), availability: 'unavailable', snapshot: null, reason: 'storage-busy', childObservation})
  expect(status.snapshot).toBeNull()
  expect(status.childObservation?.roots[0].logicalBytes).toBe('9007199254740993')
  expect(status.childObservation?.roots[0].availableBytes).toBe('0')
  expect(status.childObservation?.roots[1].availableBytes).toBeNull()
  expect(status.childObservation?.enforcement).toBe('warning-only')
  expect(decodeStorageBudgetStatus(storageStatusFixture()).childObservation).toBeNull()
})
it('keeps child DTO closed and rejects false completeness, quota language, mismatched warnings and zero substitution', () => {
  const invalid: unknown[] = []
  invalid.push({...childVector(), path: '/private'})
  invalid.push({...childVector(), state: 'measured'})
  invalid.push({...childVector(), enforcement: 'enforced'})
  invalid.push({...childVector(), coverage: 'all-child-storage'})
  invalid.push({...childVector(), lowSpaceThresholdBytes: '2147483648'})
  invalid.push({...childVector(), observedAt: 'unknown'})
  invalid.push({...childVector(), roots: Array(65).fill(childVector().roots[0])})
  for (const change of [{credential: 'private'}, {role: 'audit'}, {logicalBytes: null}, {availableBytes: '1073741824', lowSpace: true}, {availableBytes: null, lowSpace: false}, {reason: 'unreviewed'}, {state: 'inactive'}]) {
    const value = childVector(); value.roots[0] = {...value.roots[0], ...change} as typeof value.roots[0]; invalid.push(value)
  }
  const unknown = childVector(); unknown.roots[1].availableBytes = '0'; invalid.push(unknown)
  for (const childObservation of invalid) expect(() => decodeStorageBudgetStatus({...storageStatusFixture(), childObservation})).toThrow('Storage status unavailable')
})
it('accepts each bounded child state and selector without inventing observed bytes', () => {
  const value = childVector()
  value.state = 'stale'
  value.roots = [
    {component:'runtime', role:'audit', state:'partial', reason:'bounded', logicalBytes:'0', files:'0', directories:'1', availableBytes:null, lowSpace:null},
    {component:'runtime', role:'signing-metadata', state:'partial', reason:'shared', logicalBytes:null, files:null, directories:null, availableBytes:null, lowSpace:null},
    {component:'gateway', role:'runtime', state:'unavailable', reason:'changed', logicalBytes:null, files:null, directories:null, availableBytes:null, lowSpace:null},
    {component:'gateway', role:'connections', state:'inactive', reason:'none', logicalBytes:null, files:null, directories:null, availableBytes:null, lowSpace:null},
    {component:'gateway', role:'handoffs', state:'unavailable', reason:'unknown', logicalBytes:null, files:null, directories:null, availableBytes:null, lowSpace:null},
    {component:'git', role:'active-scratch', state:'inactive', reason:'none', logicalBytes:null, files:null, directories:null, availableBytes:null, lowSpace:null},
  ]
  expect(decodeStorageBudgetStatus({...storageStatusFixture(), childObservation:value}).childObservation?.roots).toHaveLength(6)
})

it('accepts the exact child row boundary and measured warning threshold without raising any storage allowance', () => {
  const healthy={...childVector().roots[0],availableBytes:'1073741824',lowSpace:false}
  const value={...childVector(),state:'measured',roots:[healthy]}
  expect(decodeStorageBudgetStatus({...storageStatusFixture(),childObservation:value}).childObservation?.roots[0].lowSpace).toBe(false)
  const full={...value,roots:Array(64).fill({...healthy,component:'native',role:'profile'})}
  expect(decodeStorageBudgetStatus({...storageStatusFixture(),childObservation:full}).childObservation?.roots).toHaveLength(64)
})

// This JSON is handwritten and shared with the Go output assertion. It is not
// a recording and carries no native qualification.
for (const row of measurementVectors) {
  it(`decodes shared measurement vector ${row.name}`, () => {
    const wire=storageStatusFixture()
    const status=decodeStorageBudgetStatus({...wire,snapshot:{...wire.snapshot,...row.facet}})
    expect(status.snapshot?.measurement).toEqual(row.facet.measurement)
    expect(status.snapshot?.physicalAllocated).toEqual(row.facet.physicalAllocated)
    expect(status.snapshot?.sharedAttribution).toEqual(row.facet.sharedAttribution)
  })
}
it('keeps original measured time and sparse zero distinct from new status time and unknown', () => {
  const first=decodeStorageBudgetStatus(storageMeasuredFixture())
  const next=storageMeasuredFixture();next.snapshot.observedAt='2026-10-31T12:00:01Z'
  const decoded=decodeStorageBudgetStatus(next)
  expect(decoded.snapshot?.measurement.at).toBe('2026-10-31T11:59:59Z')
  expect(decoded.snapshot?.measurement.at).toBe(first.snapshot?.measurement.at)
  expect(decoded.snapshot?.physicalAllocated.value?.bytes).toBe('0')
  expect(decoded.snapshot?.measurement.logical.value?.bytes).toBe('9007199254740993')
})
for (const name of ['missing-logical','unknown-key','unknown-basis','bad-allocation-basis','bad-shared-basis','unknown-state','zero-generation-with-time','time-with-not-measured','no-time-with-value','null-as-zero','fresh-unknown-logical','allocation-null-known','shared-null-measured','retained-reclaimable-zero','maintenance-state','active-fresh','fresh-generation-mismatch','filesystem-without-time'] as const) {
 it(`refuses measurement ${name}`, () => {
  const fixture=storageMeasuredFixture()
  const value: Record<string, unknown>=fixture.snapshot
  const m=value.measurement as Record<string,unknown>
  switch(name) {
   case 'missing-logical': delete m.logical; break
   case 'unknown-key': m.path='/private'; break
   case 'unknown-basis': m.basis='charged'; break
   case 'bad-allocation-basis': m.allocationBasis='exclusive'; break
   case 'bad-shared-basis': m.sharedBasis='reflinks'; break
   case 'unknown-state': m.freshness='live'; break
   case 'zero-generation-with-time': m.generation='0'; break
   case 'time-with-not-measured': m.logical={value:null,unknown:'not-measured'}; break
   case 'no-time-with-value': m.at=null; break
   case 'null-as-zero': m.logical={value:null,unknown:''}; break
   case 'fresh-unknown-logical': m.logical={value:null,unknown:'unavailable'};m.freshness='fresh'; break
   case 'allocation-null-known': value.physicalAllocated={value:null,unknown:''}; break
   case 'shared-null-measured': value.sharedAttribution={state:'measured',value:null}; break
   case 'retained-reclaimable-zero': value.reclaimable={state:'not-measured',eligibleLogical:{bytes:'0',files:'0',directories:'0'},allocatedEstimate:null}; break
   case 'maintenance-state': value.maintenance=true; break
   case 'active-fresh': m.freshness='fresh';break
   case 'fresh-generation-mismatch': m.freshness='fresh';m.generation='2';fixture.snapshot.recovery.activeTicketCount=0;fixture.snapshot.recovery.uncertainTicketCount=0;break
   case 'filesystem-without-time': Object.assign(value,storageStatusFixture().snapshot,{filesystems:[{id:'55555555555555555555555555555555',generation:'1',chargedLogical:null,completeness:'unknown',measuredLogical:{bytes:'0',files:'0',directories:'0'},physicalAllocated:null,constraintIds:[]}]});break
  }
  expect(()=>decodeStorageBudgetStatus(fixture)).toThrow('Storage status unavailable')
 })
}
it('accepts fresh idle and maintenance measurements while preserving unavailable allocation', () => {
 const value=storageMeasuredFixture();value.snapshot.measurement.freshness='fresh'
 value.snapshot.recovery.activeTicketCount=0;value.snapshot.recovery.uncertainTicketCount=0
 expect(decodeStorageBudgetStatus(value).snapshot?.measurement.freshness).toBe('fresh')
 value.snapshot.maintenance=true;value.snapshot.measurement.freshness='maintenance'
 value.snapshot.physicalAllocated={value:null,unknown:'unavailable'}
 value.snapshot.sharedAttribution={state:'unknown',value:null}
 expect(decodeStorageBudgetStatus(value).snapshot?.physicalAllocated.value).toBeNull()
 expect(decodeStorageBudgetStatus(value).snapshot?.sharedAttribution.state).toBe('unknown')
})
it('decodes per-filesystem measured values without replacing charged scope values', () => {
 const v=storageMeasuredFixture()
 const file={id:'55555555555555555555555555555555',generation:'1',chargedLogical:null,completeness:'unknown',measuredLogical:{bytes:'7',files:'1',directories:'0'},physicalAllocated:{bytes:'0',files:'1',directories:'0'},constraintIds:[]}
 const decoded=decodeStorageBudgetStatus({...v,snapshot:{...v.snapshot,filesystems:[file]}})
 expect(decoded.snapshot?.filesystems[0].measuredLogical?.bytes).toBe('7')
 expect(decoded.snapshot?.filesystems[0].physicalAllocated?.bytes).toBe('0')
 expect(()=>decodeStorageBudgetStatus({...v,snapshot:{...v.snapshot,filesystems:[file,file]}})).toThrow()
 expect(()=>decodeStorageBudgetStatus({...v,snapshot:{...v.snapshot,filesystems:[{...file,source:'/private'}]}})).toThrow()
})
