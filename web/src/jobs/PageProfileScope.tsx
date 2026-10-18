import {msg} from '../i18n'
import type {BindingDocument,InputProfile,ProfileBinding} from '../dialect/types'
import {Notice} from '../ui/Notice'
import type {ProfileEntry} from './dialectTypes'
import styles from './JobsView.module.css'

const wildcard='https://*'

/** The installed profile a page assignment pins, matched by id and digest. */
export function pinnedProfile(profiles:ProfileEntry[],pin?:ProfileBinding):InputProfile|undefined {
 return pin?profiles.find(entry=>entry.profile.id===pin.profile&&entry.digest===pin.profileDigest)?.profile:undefined
}

/** The page assignment a reviewed run froze, read as the Desk reads it: the
 * import's binding when the input carries one, otherwise the mapping's source. */
export function frozenPagePin(inputText:string,sourceName:string):ProfileBinding|undefined {
 try {
  const input=JSON.parse(inputText) as {source?:{import?:{binding?:BindingDocument};mapping?:{sources?:{name?:string;webProfiles?:Partial<Record<'search'|'page',ProfileBinding>>}[]}}}
  const page=input.source?.import?input.source.import.binding?.sources?.[sourceName]?.webProfiles?.page:input.source?.mapping?.sources?.find(source=>source.name===sourceName)?.webProfiles?.page
  return typeof page?.profile==='string'&&typeof page.profileDigest==='string'?page:undefined
 } catch {return undefined}
}

/** The bound page profile's reviewed scope, shown where the profile is chosen
 * and before research is confirmed. Entries are shown exactly as registered. */
export function PageProfileScope({profile}:{profile?:InputProfile}) {
 if(!profile)return <Notice tone="warning">{msg('The scope of this page profile cannot be shown: it is not among the installed profiles.')}</Notice>
 const scope=profile.endpointScope
 if(!scope)return <p className={styles.note}>{msg('No page scope. Runner accepts a page only at this exact endpoint: {{endpoint}}',{endpoint:profile.endpoint??''})}</p>
 return <div className={styles.pageScope}>
  <span className={styles.note}>{msg('Page scope')}</span>
  <ul aria-label={msg('Page scope')}>{scope.map(entry=><li key={entry}><code>{entry}</code></li>)}</ul>
  {scope.includes(wildcard)&&<Notice tone="warning">{msg('This scope includes https://*: any HTTPS page may be read.')}</Notice>}
  <p className={styles.note}>{msg('Requests outside these prefixes are refused.')}</p>
 </div>
}
