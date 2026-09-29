import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { answer, deskFetch } from '../files/client'
import { msg, useLocale } from '../i18n'
import { Section } from '../components/primitives'
import { Button } from '../ui/Button'
import styles from './Updates.module.css'

export interface UpdateStatus {
  managed: boolean
  development: boolean
  installedVersion: string
  checkedAt?: number
  error?: string
  autoInstall?: boolean
  latest?: {version: string; url: string; asset?: string | null}
  current?: {version: string}
  pending?: {version: string} | null
}
const key = ['installation-updates']
export function newer(available: string, installed: string) {
  const parse = (s: string) => /^\d+\.\d+\.\d+$/.test(s) ? s.split('.').map(Number) : null
  const a=parse(available), b=parse(installed)
  if (!a || !b) return false
  for (let i=0; i<3; i++) { if (a[i] !== b[i]) return a[i]! > b[i]! }
  return false
}
export function Updates() {
  const locale = useLocale(), client=useQueryClient()
  const status=useQuery({queryKey:key, queryFn:async()=>answer<UpdateStatus>(await deskFetch('/api/updates')), staleTime:60_000, retry:false})
  const action=useMutation({mutationFn:async(action:string)=>answer<UpdateStatus>(await deskFetch('/api/updates',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action})})),onSuccess:value=>client.setQueryData(key,value)})
  const data=status.data
  const autoInstall = action.isPending && (action.variables === 'auto-on' || action.variables === 'auto-off')
    ? action.variables === 'auto-on' : data?.autoInstall === true
  const canStage=!!data?.managed && !data.development && !!data.latest?.asset && !data.error && newer(data.latest.version,data.current?.version??data.installedVersion) && !data.pending
  return <Section title={msg('Updates')}>
    {data ? <>
      <dl className={styles.versions}>
        <dt>{msg('Installed version')}</dt><dd>{data.development ? msg('Local development build') : data.installedVersion}</dd>
        <dt>{msg('Latest stable release')}</dt><dd>{data.latest?.version ?? msg('Not checked')}</dd>
        <dt>{msg('Last checked')}</dt><dd>{data.checkedAt ? new Date(data.checkedAt*1000).toLocaleString(locale) : msg('Not checked')}</dd>
      </dl>
      {data.development && <p className="quiet">{msg('Local changes are preserved. Updates apply only to managed installations.')}</p>}
      {(data.error || action.isError) && <p role="alert">{msg('Update check or download failed. The active installation is unchanged.')}</p>}
      {data.pending && <p role="status">{msg('Version {{version}} is ready for the next launch.',{version:data.pending.version})}</p>}
      {data.managed && !data.development && <>
        <label className={styles.option}><input type="checkbox" checked={autoInstall} disabled={action.isPending} onChange={e=>action.mutate(e.target.checked?'auto-on':'auto-off')}/>{msg('Automatically update on launch')}</label>
        <p className="quiet">{msg('Updates are applied before Desk starts. Running work is never restarted automatically.')}</p>
        {data.latest && data.latest.asset===null && <p className="quiet">{msg('No release bundle is available for this platform.')}</p>}
      </>}
    </> : <p role="status">{status.isError ? msg('Update information is unavailable.') : msg('Loading…')}</p>}
    <div className={styles.actions}>
      <Button disabled={action.isPending||status.isPending} onClick={()=>action.mutate('check')}>{action.isPending ? msg('Working…') : msg('Check for updates')}</Button>
      {canStage && <Button disabled={action.isPending} onClick={()=>action.mutate('stage')}>{msg('Prepare update')}</Button>}
      {data?.managed && data.pending && <Button variant="quiet" disabled={action.isPending} onClick={()=>action.mutate('cancel')}>{msg('Cancel prepared update')}</Button>}
      <a href="https://github.com/Judgment-Pack/judgment-pack-desk/releases" target="_blank" rel="noreferrer">{msg('Release notes and downloads')}</a>
    </div>
  </Section>
}
