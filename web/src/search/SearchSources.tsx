import type { WorkItem } from '../chat/responseHistory'
import { searchFailureMessage } from './failures'
import { ProviderIcon, providerName } from './ProviderIcon'
import { useQuery } from '@tanstack/react-query'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { ReadingDetails } from '../chat/ReadingDetails'
import { msg, useLocale, formattingLocale, systemMessage } from '../i18n'
import { loadSearch, type SearchReference } from './results'
import { AttributionFrame } from './AttributionFrame'
import { SourceRow } from '../ui/SourceRow'
import { IconLink } from '../shell/icons'
import { Disclosure } from '../ui/Disclosure'
import styles from './SearchSources.module.css'

function useVerifiedSearch(reference:SearchReference){
 const pin=useEffectiveConfig().config.research.gateway
 const query=useQuery({queryKey:['verified-search',reference,pin],enabled:!!pin,retry:false,staleTime:Infinity,queryFn:({signal})=>loadSearch(reference,pin!,signal)})
 return {...query,pin}
}
export function SearchReferenceIcon({reference}:{reference:SearchReference}){
 const query=useVerifiedSearch(reference)
 return <ProviderIcon provider={query.data?.result.provider}/>
}
/** Google supplies attribution markup. Isolate it from Desk, disallow scripts, forms, tracking and top navigation. */
export function SearchAttribution({reference}:{reference:SearchReference}){
 useLocale()
 const query=useVerifiedSearch(reference)
 const html=query.data?.result.attributionHtml
 if(!html)return null
 return <AttributionFrame html={html}/>
}
export function SearchSources({reference}:{reference:SearchReference}){
 useLocale()
 const query=useVerifiedSearch(reference)
 return <ReadingDetails title={msg('Web search')}>
  <div className={styles.reader}>
   <div><h3 className={styles.label}>{msg('Query sent')}</h3><p className={styles.query}>{reference.request.query}</p></div>
   {!query.pin||query.isError?<p role="alert">{msg('Search results could not be verified; no links were authorized.')}</p>:query.isPending?<p role="status">{msg('Loading…')}</p>:<>
    <div className={styles.metadata}><ProviderIcon provider={query.data.result.provider}/><span>{providerName(query.data.result.provider)}</span><span aria-hidden="true">·</span><time dateTime={query.data.result.retrievedAt}>{new Intl.DateTimeFormat(formattingLocale(),{dateStyle:'medium',timeStyle:'short'}).format(new Date(query.data.result.retrievedAt))}</time></div>
    {query.data.result.provider==='google-grounding'&&!!query.data.result.queries?.length&&<section><h3 className={styles.label}>{msg('Google queries')}</h3><ul className={styles.queries}>{query.data.result.queries.map((q,index)=><li key={index}>{q}</li>)}</ul></section>}
    <p className={styles.note}>{msg('Search results are leads. Page text is retained separately when read.')}</p>
    <section className={styles.results} aria-label={msg('Search results')}><h3>{msg('Search results')} · {query.data.result.hits.length}</h3>
     <ol>{query.data.result.hits.map(hit=><li key={hit.url}><SourceRow title={hit.title||new URL(hit.url).hostname} meta={msg('Open source')} icon={<IconLink/>} href={hit.url}/>{hit.snippet&&<p className={styles.note}>{hit.snippet}</p>}</li>)}</ol>
     {!query.data.result.hits.length&&<p className={styles.note}>{msg('No results found.')}</p>}
    </section>
    <SearchAttribution reference={reference}/>
    {query.data.result.generatedAnswer&&<Disclosure className={styles.answer} title={msg('Provider-generated answer')}><p className={styles.answerText}>{query.data.result.generatedAnswer}</p></Disclosure>}
   </>}
  </div>
 </ReadingDetails>
}

/** Failed and legacy calls must not borrow a neighboring call's result. */
export function SearchStepDetails({row}:{row:WorkItem}){
 useLocale()
 if(row.search?.reference)return <SearchSources reference={row.search.reference}/>
 const reason=searchFailureMessage(row.failure)
 return <ReadingDetails title={msg('Web search')}><div className={styles.reader}>
  {row.search?.provider&&<div className={styles.metadata}><ProviderIcon provider={row.search.provider}/><span>{providerName(row.search.provider)}</span></div>}
  <p className={styles.note}>{row.status==='failed'?msg('Failed'):row.status==='complete'?msg('Done'):row.status==='working'?msg('Working…'):msg('Interrupted')}</p>
  {row.search&&<div><h3 className={styles.label}>{row.search.submitted?msg('Query sent'):msg('Requested query')}</h3><p className={styles.query}>{row.search.query}</p></div>}
  {reason?<p className={styles.note}>{systemMessage(reason)}</p>:row.status!=='working'&&<p className={styles.note}>{msg('Details were not recorded for this step.')}</p>}
 </div></ReadingDetails>
}
