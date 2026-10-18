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
 * and before research is confirmed. Entries are shown exactly as registered.
 * The label names whose scope this is where two can show at once. An empty
 * scope, or a profile with neither a scope nor an endpoint, shows nothing it
 * could be mistaken for: Runner refuses both at registration. */
export function PageProfileScope({profile,label}:{profile?:InputProfile;label?:string}) {
 const scope=profile?.endpointScope
 if(!profile||(scope?!scope.length:!profile.endpoint))return <Notice tone="warning">{msg('The scope of this page profile cannot be shown: it is not among the installed profiles.')}</Notice>
 if(!scope)return <p className={styles.note}>{msg('No page scope. Runner accepts a page only at this exact endpoint: {{endpoint}}',{endpoint:profile.endpoint??''})}</p>
 const title=label??msg('Page scope')
 return <div className={styles.pageScope}>
  <span className={styles.note}>{title}</span>
  <ul aria-label={title}>{scope.map(entry=><li key={entry}><code>{entry}</code></li>)}</ul>
  {scope.includes(wildcard)&&<Notice tone="warning">{msg('This scope includes https://*: any HTTPS page may be read.')}</Notice>}
  <p className={styles.note}>{msg('Requests outside these prefixes are refused.')}</p>
 </div>
}
