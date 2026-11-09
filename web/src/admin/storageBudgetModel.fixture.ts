// Hand-authored contract fixture: never captured from the implementation.
// Accepted wire order is independent of the production decoder's category list.
const acceptedCategories = ['history', 'retained', 'attachments', 'memory', 'control', 'disposable', 'binaries', 'transient', 'settlement'] as const
export function storageStatusFixture() {
  const zero = {bytes: '0', files: '0', directories: '0'}
  const amounts = {known: true, chargedLogical: {bytes: '9007199254740993', files: '1', directories: '0'}, promisedPeak: zero, promisedSettlement: zero, limit: {bytes: '9007199254740992', files: '10', directories: '10'}, logicalRemaining: zero, effectiveLogicalRemaining: zero, overBudget: true}
  const installation = {...amounts, id: '11111111111111111111111111111111', generation: '1'}
  const workspace = {...amounts, id: '22222222222222222222222222222222', generation: '1'}
  return {
    version: 1, availability: 'available', reason: null, readOnly: false, servedAt: '2026-10-31T12:00:01Z',
    pressure: {active: false, codes: [] as string[], firstSeenAt: null, lastSeenAt: null, durableState: 'unconfirmed'},
    snapshot: {
      observationSession: '33333333333333333333333333333333', observationSequence: '18446744073709551615', observedAt: '2026-10-31T12:00:00Z', policyRevision: '18446744073709551615', epoch: '1', maintenance: false,
      coverage: 'registered-scope-only', measurement: {at: null as string | null, generation: '0', freshness: 'unknown', logical: {value: null as {bytes: string; files: string; directories: string} | null, unknown: 'not-measured'}, basis: 'registered-metadata', allocationBasis: 'reported-blocks', sharedBasis: 'workspace-reference-overlap'},
      installation, currentWorkspace: workspace,
      categories: acceptedCategories.map(category => ({...amounts, category})), currentWorkspaceCategories: acceptedCategories.map(category => ({...amounts, category})),
      workspaces: [workspace], roots: [], filesystems: [], constraints: [],
      retainedLogical: {value: null, unknown: 'not-measured'}, physicalAllocated: {value: null as {bytes: string; files: string; directories: string} | null, unknown: 'not-measured'},
      sharedAttribution: {state: 'not-measured', value: null as {bytes: string; files: string; directories: string} | null}, reclaimable: {state: 'not-measured', eligibleLogical: null, allocatedEstimate: null},
      recovery: {pendingRecoveryCount: null, pendingRecoveryState: 'not-measured', uncertainTicketCount: 1, activeTicketCount: 1, terminalAwaitingCheckpointCount: 0},
    },
  }
}

// Original measured time deliberately differs from the status observation.
export function storageMeasuredFixture() {
 const value=storageStatusFixture()
 value.snapshot.measurement={at:'2026-10-31T11:59:59Z',generation:'1',freshness:'stale',logical:{value:{bytes:'9007199254740993',files:'2',directories:'1'},unknown:''},basis:'registered-metadata',allocationBasis:'reported-blocks',sharedBasis:'workspace-reference-overlap'}
 value.snapshot.physicalAllocated={value:{bytes:'0',files:'2',directories:'1'},unknown:''}
 value.snapshot.sharedAttribution={state:'measured',value:{bytes:'23',files:'1',directories:'0'}}
 return value
}
