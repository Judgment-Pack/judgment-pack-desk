import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { answer, deskFetch } from '../files/client'
import { msg, useLocale } from '../i18n'
import { Section } from '../components/primitives'
import type { BuildIdentity, ComponentBuilds } from '../config/deskConfig'
import { Button } from '../ui/Button'
import styles from './Updates.module.css'

export interface UpdateStatus {
  components?: Record<string, {version: string; revision: string; channel: string}>
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
type GatewayBuild = {version?: string; revision: string; unverified?: boolean}
/**
 * Compares a running component's recorded commit with the one this Desk build
 * pins. It says nothing about newer releases. A missing identity, or a Gateway
 * revision admitted by an operator digest rather than checked, stays unknown;
 * a modified build is never called matching.
 */
export function componentMatch(build: (BuildIdentity & {unverified?: boolean}) | undefined, revision: string): 'matching'|'different'|'unknown' {
  if (!build?.revision || build.unverified) return 'unknown'
  return build.revision === revision && !build.modified ? 'matching' : 'different'
}
export function Updates({builds, gateway}: {builds?: ComponentBuilds; gateway?: GatewayBuild} = {}) {
  const locale = useLocale(), client=useQueryClient()
  const status=useQuery({queryKey:key, queryFn:async()=>answer<UpdateStatus>(await deskFetch('/api/updates')), staleTime:60_000, retry:false})
  const action=useMutation({mutationFn:async(action:string)=>answer<UpdateStatus>(await deskFetch('/api/updates',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action})})),onSuccess:value=>client.setQueryData(key,value)})
  const data=status.data
  const autoInstall = action.isPending && (action.variables === 'auto-on' || action.variables === 'auto-off')
    ? action.variables === 'auto-on' : data?.autoInstall === true
  const canStage=!!data?.managed && !data.development && !!data.latest?.asset && !data.error && newer(data.latest.version,data.current?.version??data.installedVersion) && !data.pending
  // A Runner this Desk reports as not configured has no row, rather than "Unknown".
  const runner = !builds || !!builds.runner
  const rows = data?.components ? [
    {key:'runtime', label:msg('Runtime'), build:builds?.runtime, shown:true},
    {key:'runner', label:msg('Runner'), build:builds?.runner, shown:runner},
    {key:'runner', label:msg('Source worker'), build:builds?.sourceWorker, shown:runner},
    {key:'gateway', label:msg('Gateway'), build:gateway, shown:true},
  ].filter(row => row.shown).flatMap(row => {
    const expected = data.components?.[row.key]
    return expected ? [{...row, expected, match:componentMatch(row.build, expected.revision)}] : []
  }) : []
  return <Section title={msg('Updates')}>
    {data ? <>
      <dl className={styles.versions}>
        <dt>{msg('Installed version')}</dt><dd>{data.development ? msg('Local development build') : data.installedVersion}</dd>
        <dt>{msg('Latest stable release')}</dt><dd>{data.latest?.version ?? msg('Not checked')}</dd>
        <dt>{msg('Last checked')}</dt><dd>{data.checkedAt ? new Date(data.checkedAt*1000).toLocaleString(locale) : msg('Not checked')}</dd>
      </dl>
      {rows.some(row=>row.match==='different') && <p role="alert">{msg('Some components differ from this Desk build. Restart using the development launcher or reinstall the complete release bundle.')}</p>}
      {rows.length>0 && <details className="disclosure">
        <summary>{msg('Component versions')}</summary>
        <p className="quiet">{msg('Build information recorded when Desk started. Development builds are identified by their source commit.')}</p>
        <div className={styles.components}><table>
          <thead><tr><th scope="col">{msg('Component')}</th><th scope="col">{msg('Installed version')}</th><th scope="col">{msg('Expected')}</th><th scope="col">{msg('Status')}</th></tr></thead>
          <tbody>{rows.map(row=>{
            // A development pin names a commit, not a published release.
            const release = row.expected.channel !== 'development'
            return <tr key={row.label}>
              <th scope="row">{row.label}</th>
              <td><code>{row.match==='matching' && release ? row.expected.version : row.build?.revision?.slice(0,12) || msg('Unknown')}</code></td>
              <td><code>{row.expected.version}</code></td>
              <td>{row.match==='matching' ? (release ? msg('Matches release') : msg('Matches pinned commit')) : row.match==='different' ? msg('Different build') : msg('Unknown')}</td>
            </tr>
          })}</tbody>
        </table></div>
      </details>}
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
