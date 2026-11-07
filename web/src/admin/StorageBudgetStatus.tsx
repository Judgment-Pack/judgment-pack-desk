import {StorageBudgetPolicyEditor} from './StorageBudgetPolicyEditor'
import {msg, useLocale} from '../i18n'
import {Button} from '../ui/Button'
import {SettingsSection} from '../ui/SettingsSection'
import {SettingsFeedback} from '../ui/SettingsFeedback'
import {formatStorageUInt, type StorageAmounts, type StorageCategory, type StorageVector} from './storageBudgetModel'
import {useStorageBudgetStatus} from './useStorageBudgetStatus'

function Vector({value}: {value: StorageVector | null}) {
  return value === null ? <>{msg('Unknown')}</> : <>{msg('{{bytes}} bytes · {{files}} files · {{directories}} directories', {
    bytes: formatStorageUInt(value.bytes), files: formatStorageUInt(value.files), directories: formatStorageUInt(value.directories),
  })}</>
}
function Amounts({value}: {value: StorageAmounts}) {
  return <dl className="fields">
    <dt>{msg('Charged logical usage')}</dt><dd><Vector value={value.chargedLogical}/></dd>
    <dt>{msg('Logical limit')}</dt><dd><Vector value={value.limit}/></dd>
    <dt>{msg('Additional peak promises')}</dt><dd><Vector value={value.promisedPeak}/></dd>
    <dt>{msg('Settlement promises')}</dt><dd><Vector value={value.promisedSettlement}/></dd>
    <dt>{msg('Logical remaining')}</dt><dd><Vector value={value.logicalRemaining}/></dd>
    <dt>{msg('Remaining within installation limits')}</dt><dd><Vector value={value.effectiveLogicalRemaining}/></dd>
    <dt>{msg('Over budget')}</dt><dd>{value.overBudget === null ? msg('Unknown') : value.overBudget ? msg('Yes') : msg('No')}</dd>
  </dl>
}
function categoryLabel(category: StorageCategory) {
  switch (category) {
    case 'history': return msg('History')
    case 'retained': return msg('Retained')
    case 'attachments': return msg('Attachments')
    case 'memory': return msg('Memory')
    case 'control': return msg('Control')
    case 'disposable': return msg('Disposable')
    case 'binaries': return msg('Binaries')
    case 'transient': return msg('Transient')
    case 'settlement': return msg('Settlement')
  }
}
export function StorageBudgetStatus() {
  useLocale()
  const query = useStorageBudgetStatus(), status = query.data, snapshot = status?.snapshot
  const child = status?.childObservation
  const childStale = query.isError || child?.state === 'stale' || Boolean(child && Date.now() - Date.parse(child.observedAt) > 15_000)
  const measurement = snapshot?.measurement
  const measurementStale = query.isError || status?.availability !== 'available' || measurement?.freshness !== 'fresh' || Boolean(measurement?.at && Date.now() - Date.parse(measurement.at) > 15_000)
  const stale = query.isError || status?.availability !== 'available' || snapshot?.maintenance || Boolean(snapshot && Date.now() - Date.parse(snapshot.observedAt) > 15_000)
  const refresh = <Button variant="quiet" disabled={query.isFetching} onClick={() => void query.refetch()}>{msg('Refresh storage status')}</Button>
  return <SettingsSection title={msg('Storage accounting')} variant="plain" description={msg('Registered storage scopes only. Logical charges are not physical disk occupation.')}>
    {!status && !query.isError && <p>{msg('Reading storage status…')}</p>}
    {(query.isError || status?.availability === 'unavailable') && <SettingsFeedback tone="warning" actions={!status && refresh}>{msg('Storage status unavailable. Previous observations, if present, remain visible.')}</SettingsFeedback>}
    {snapshot && <>
      <p role="status">{stale ? msg('Stale storage observation') : msg('Storage observation')} · {msg('As of')} <time dateTime={snapshot.observedAt}>{snapshot.observedAt}</time></p>
      {status?.readOnly && <SettingsFeedback tone="warning">{msg('Storage is read only. Existing data remains visible; status refresh does not resume paused work.')}</SettingsFeedback>}
      {measurement?.at ? <>
       <h3>{msg('Measured Desk-owned usage')}</h3>
       <p role="status">{measurementStale ? msg('Stale usage measurement') : msg('Usage measurement')} · {msg('Measurement as of')} <time dateTime={measurement.at}>{measurement.at}</time> · {msg('Measurement generation')}: {measurement.generation}</p>
       <p>{msg('These metadata observations exclude provisioned control floors. They do not authorize a write.')}</p>
       <dl className="fields"><dt>{msg('Measured logical usage')}</dt><dd><Vector value={measurement.logical.value}/></dd><dt>{msg('Workspace attribution overlap')}</dt><dd><Vector value={snapshot.sharedAttribution.value}/></dd></dl>
      </> : <p>{msg('Measurement time is unknown. Promises are additional logical obligations. Remaining capacity is a policy estimate and does not authorize a write.')}</p>}
      <h3>{msg('Current workspace')}</h3>
      {snapshot.currentWorkspace ? <Amounts value={snapshot.currentWorkspace}/> : <p>{msg('No current workspace accounting scope is available.')}</p>}
      <h3>{msg('Installation')}</h3><Amounts value={snapshot.installation}/>
      <p>{msg('Workspace charges are attributed usage and can share stored content. They must not be added to calculate installation usage.')}</p>
      <details><summary>{msg('Category accounting')}</summary>
        {snapshot.categories.map((row, i) => <section key={row.category}>
          <h4>{categoryLabel(row.category)}</h4>
          {snapshot.currentWorkspaceCategories && <><h5>{msg('Current workspace')}</h5><Amounts value={snapshot.currentWorkspaceCategories[i]}/></>}
          <h5>{msg('Installation')}</h5><Amounts value={row}/>
        </section>)}
      </details>
    </>}
    {status?.readOnly && !snapshot && <p>{msg('Storage is read only.')}</p>}
    <h3>{msg('Child storage observations')}</h3>
    <p>{msg('Warning only. Child storage growth is measured where available and is not prevented; it reserves no capacity and changes no Desk limit.')}</p>
    {!child && <SettingsFeedback tone="warning">{msg('Child storage measurements are unavailable. Unknown usage is not zero.')}</SettingsFeedback>}
    {child && <>
      <p role="status">{childStale ? msg('Stale child storage observation') : child.state === 'partial' ? msg('Partial child storage observation') : msg('Child storage observation')} · {msg('As of')} <time dateTime={child.observedAt}>{child.observedAt}</time></p>
      {child.roots.some(row => row.lowSpace === true) && <SettingsFeedback tone="warning">{msg('Low filesystem space observed for child storage. Available space is below 1 GiB. Child growth is not prevented.')}</SettingsFeedback>}
      <p>{msg('Selected child roots only; shared roots and objects count once.')}</p>
      <p>{msg('File lengths are metadata, not allocated disk bytes; partial counts are lower bounds. Never add them to Desk charges.')}</p>
      <ul>{child.roots.map((row, i) => <li key={`${row.component}:${row.role}:${i}`}>
        {row.component} · {row.role} · {row.state === 'inactive' ? msg('Not active or configured') : row.state === 'unavailable' ? msg('Unavailable') : row.state === 'partial' ? msg('Partial') : msg('Measured')}
        {' · '}{msg('File-length bytes')}: {row.logicalBytes === null ? msg('Unknown') : formatStorageUInt(row.logicalBytes)}
        {' · '}{msg('Filesystem available bytes')}: {row.availableBytes === null ? msg('Unknown') : formatStorageUInt(row.availableBytes)}
      </li>)}</ul>
    </>}
    <h3>{msg('Physical allocated storage')}</h3>
    <dl className="fields">
      <dt>{msg('Reported allocated storage')}</dt><dd>{snapshot?.physicalAllocated.unknown === 'not-measured' || !snapshot ? msg('Not measured') : <Vector value={snapshot.physicalAllocated.value}/>}</dd>
      <dt>{msg('Shared and unshared storage')}</dt><dd>{msg('Not measured')}</dd>
      <dt>{msg('Reclaimable storage')}</dt><dd>{msg('Not measured')}</dd>
      <dt>{msg('Retained originals across all categories')}</dt><dd>{msg('Not measured')}</dd>
    </dl>
    <p>{msg('Allocation is reported filesystem metadata. It does not establish exclusive storage or reclaimable bytes.')}</p>
    {snapshot && snapshot.filesystems.length > 0 && <details><summary>{msg('Measured usage by filesystem')}</summary>{snapshot.filesystems.map(row => <section key={row.id}>
      <h4>{msg('Filesystem {{id}}', {id: row.id})}</h4>
      <dl className="fields"><dt>{msg('Measured logical usage')}</dt><dd><Vector value={row.measuredLogical}/></dd><dt>{msg('Reported allocated storage')}</dt><dd><Vector value={row.physicalAllocated}/></dd></dl>
    </section>)}</details>}
    <p>{msg('Context compaction changes what the model sees. It does not delete local history or free this storage.')}</p>
    {/* Limits and a refresh are offered once a status has been read: until
        then Desk cannot say whether storage is read only, and a failed read
        offers its refresh beside the failure. */}
    {status && <StorageBudgetPolicyEditor readOnly={status.readOnly === true}/>}
    {status && refresh}
  </SettingsSection>
}
