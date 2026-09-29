import { useQuery } from '@tanstack/react-query'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { ReadingDetails } from '../chat/ReadingDetails'
import { msg, useLocale } from '../i18n'
import { loadSearch, type SearchReference } from './results'
import styles from './WebSearchSettings.module.css'

function useVerifiedSearch(reference:SearchReference){
 const pin=useEffectiveConfig().config.research.gateway
 const query=useQuery({queryKey:['verified-search',reference,pin],enabled:!!pin,retry:false,staleTime:Infinity,queryFn:({signal})=>loadSearch(reference,pin!,signal)})
 return {...query,pin}
}
/** Google supplies attribution markup. Isolate it from Desk, disallow scripts, forms, tracking and top navigation. */
export function SearchAttribution({reference}:{reference:SearchReference}){
 useLocale()
 const query=useVerifiedSearch(reference)
 const html=query.data?.result.attributionHtml
 if(!html)return null
 const policy="default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'"
 return <iframe className={styles.attribution} title={msg('Search provider attribution')} sandbox="allow-popups allow-popups-to-escape-sandbox" referrerPolicy="no-referrer" srcDoc={`<!doctype html><meta http-equiv="Content-Security-Policy" content="${policy}"><base target="_blank">${html}`}/>
}
export function SearchSources({reference}:{reference:SearchReference}){
 useLocale()
 const query=useVerifiedSearch(reference)
 return <ReadingDetails title={msg('Web search')}>
  <p>{reference.request.query}</p>
  {!query.pin||query.isError?<p role="alert">{msg('Search results could not be verified; no links were authorized.')}</p>:query.isPending?<p role="status">{msg('Loading…')}</p>:<>
   <p className={styles.caption}>{query.data.result.provider} · {new Date(query.data.result.retrievedAt).toLocaleString()}</p>
   <p className={styles.caption}>{msg('Search results are leads. Page text is retained separately when read.')}</p>
   <ul>{query.data.result.hits.map(hit=><li key={hit.url}><a href={hit.url} target="_blank" rel="noreferrer">{hit.title||hit.url}</a>{hit.snippet&&<p>{hit.snippet}</p>}</li>)}</ul>
   {query.data.result.generatedAnswer&&<details><summary>{msg('Provider-generated answer')}</summary><p>{query.data.result.generatedAnswer}</p></details>}
   <SearchAttribution reference={reference}/>
  </>}
 </ReadingDetails>
}
