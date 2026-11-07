import {useState} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {msg} from '../i18n'
import {Button} from '../ui/Button'
import {Field, FieldGroup} from '../ui/Field'
import {Input} from '../ui/Input'
import {SettingsFeedback} from '../ui/SettingsFeedback'
import {useDirtyGuard} from '../shell/useDirtyGuard'
import {storageCategories, type StorageVector} from './storageBudgetModel'
import {storageBudgetStatusKey} from './useStorageBudgetStatus'
import {cloneStoragePolicyLimits, decodeStoragePolicyLimits, editStorageBudgetPolicy, prepareStorageBudgetPolicy, readStorageBudgetPolicy, storagePolicyScopeLimits, StoragePolicyError, type StoragePolicyLimits, type StoragePolicyScope, type StoragePolicySnapshot} from './storageBudgetPolicy'

export function StorageBudgetPolicyEditor({readOnly}: {readOnly: boolean}) {
  const client = useQueryClient()
  const categoryLabels = [msg('History'), msg('Retained'), msg('Attachments'), msg('Memory'), msg('Control'), msg('Disposable'), msg('Binaries'), msg('Transient'), msg('Settlement')]
  const units = {bytes: msg('bytes'), files: msg('files'), directories: msg('directories')}
  const [base, setBase] = useState<StoragePolicySnapshot | null>(null), [draft, setDraft] = useState<StoragePolicyLimits | null>(null)
  const [scope, setScope] = useState<StoragePolicyScope>('workspace'), [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<'invalid' | 'conflict' | 'uncertain' | null>(null), [saved, setSaved] = useState(false)
  const dirty = Boolean(base && draft && JSON.stringify(draft) !== JSON.stringify(storagePolicyScopeLimits(base, scope)))
  const blocked = problem === 'conflict' || problem === 'uncertain'
  useDirtyGuard(dirty, msg('Discard these storage limit changes?'), {busy})
  async function load(prepare: boolean) {
    if (busy) return
    setBusy(true); setSaved(false)
    try {
      if (prepare) {await prepareStorageBudgetPolicy(); void client.invalidateQueries({queryKey: storageBudgetStatusKey})}
      const next = await readStorageBudgetPolicy()
      setBase(next); setDraft(cloneStoragePolicyLimits(storagePolicyScopeLimits(next, scope))); setProblem(null)
    } catch {setProblem('uncertain')} finally {setBusy(false)}
  }
  async function save() {
    if (!base || !draft || busy || readOnly || blocked) return
    try {decodeStoragePolicyLimits(draft)} catch {setProblem('invalid'); return}
    setBusy(true); setSaved(false)
    try {
      const next = await editStorageBudgetPolicy(base, scope, draft)
      setBase(next); setDraft(cloneStoragePolicyLimits(storagePolicyScopeLimits(next, scope))); setProblem(null); setSaved(true)
      void client.invalidateQueries({queryKey: storageBudgetStatusKey})
    } catch (error) {setProblem(error instanceof StoragePolicyError ? error.kind : 'uncertain')} finally {setBusy(false)}
  }
  function change(index: number, key: keyof StorageVector, value: string) {
    if (!draft) return
    const next = cloneStoragePolicyLimits(draft)
    if (index < 0) next.total[key] = value
    else next.categories[index][key] = value
    setDraft(next); setSaved(false)
    if (problem === 'invalid') setProblem(null)
  }
  function fields(index: number, label: string, v: StorageVector) {
    return <FieldGroup>{(['bytes', 'files', 'directories'] as const).map(key => <Field key={key} label={msg('{{scope}} {{unit}} limit', {scope: label, unit: units[key]})}>{wiring => <Input {...wiring} inputMode="numeric" autoComplete="off" spellCheck={false} value={v[key]} disabled={busy || readOnly} onChange={e => change(index, key, e.target.value)}/>}</Field>)}</FieldGroup>
  }
  return <section aria-label={msg('Storage limits')}>
    <h3>{msg('Storage limits')}</h3>
    <p>{msg('Workspaces keep their own limits; the default is for new ones. Lower limits keep existing charges and promises. In this release limits are warnings: growth past a limit is reported, not refused.')}</p>
    {!base && <Button disabled={busy || readOnly} onClick={() => void load(true)}>{busy ? msg('Preparing storage limits…') : msg('Edit storage limits')}</Button>}
    {base && draft && <form onSubmit={e => {e.preventDefault(); void save()}}>
      <Field label={msg('Limits to edit')}>{wiring => <select {...wiring} value={scope} disabled={busy || dirty || readOnly} onChange={e => {const next = e.target.value as StoragePolicyScope; setScope(next); setDraft(cloneStoragePolicyLimits(storagePolicyScopeLimits(base, next))); setSaved(false)}}>
        <option value="workspace">{msg('Current workspace')}</option><option value="installation">{msg('Installation')}</option><option value="default">{msg('Default for future workspaces')}</option>
      </select>}</Field>
      {fields(-1, msg('Total'), draft.total)}
      <details><summary>{msg('Category limits')}</summary>{storageCategories.map((category, i) => <section key={category}><h4>{categoryLabels[i]}</h4>{fields(i, categoryLabels[i], draft.categories[i])}</section>)}</details>
      {scope === 'workspace' && <><Button disabled={busy || dirty || readOnly} onClick={() => {setDraft(cloneStoragePolicyLimits(base.defaultLimits)); setSaved(false)}}>{msg('Copy default once')}</Button><p>{msg('Copying the default changes this draft only. Later default changes will not update this workspace.')}</p></>}
      <Button type="submit" variant="primary" disabled={busy || readOnly || !dirty || blocked}>{busy ? msg('Saving storage limits…') : msg('Save storage limits')}</Button>
      <Button disabled={busy} onClick={() => void load(false)}>{msg('Discard draft and reload policy')}</Button>
    </form>}
    {problem && <SettingsFeedback tone="error">{problem === 'invalid' ? msg('Enter positive whole decimal limits within the supported bounds.') : problem === 'conflict' ? msg('Storage policy changed or is busy. Your draft is preserved. Discard and reload current policy before another save.') : msg('Storage policy outcome is not confirmed. Your draft is preserved. Review storage recovery and current policy before another save.')}</SettingsFeedback>}
    {saved && <SettingsFeedback tone="neutral">{msg('Storage limits saved.')}</SettingsFeedback>}
  </section>
}
