import {useEffect,useState} from 'react'
import {msg,useLocale} from '../i18n'
import {whenSessionEnds,sessionEnded} from '../mcp/session'
import {Button} from '../ui/Button'
import {CodeDiff} from '../ui/CodeDiff'
import {Disclosure} from '../ui/Disclosure'
import {LoadingState} from '../ui/LoadingState'
import {Notice} from '../ui/Notice'
import {IconAssistant,IconClose,IconRetry} from '../shell/icons'
import {resolvePolicyComparison,type PolicyComparison as Comparison,type PolicyComparisonRequest,type PolicyComparisonSelector} from './comparison'
import styles from './PolicyComparison.module.css'

export type DiscussPolicyComparison=(request:PolicyComparisonRequest)=>void
export function comparisonSelectorLabel(selector:PolicyComparisonSelector){
 return selector.kind==='revision'?msg('Saved revision {{id}}',{id:selector.revisionId}):selector.kind==='publication'?msg('Local publication {{id}}',{id:selector.publicationId}):msg('Retained observation {{id}}',{id:selector.observationId})
}
const fieldLabel=(field:string)=>({title:msg('Title'),artifactVersion:msg('Artifact version'),workflow:msg('Review workflow'),workflowDigest:msg('Workflow digest'),candidateDigest:msg('Candidate digest'),content:msg('Retained content identity'),source:msg('Source identity'),original:msg('Original bytes'),extraction:msg('Extraction and processor'),processing:msg('Page processing'),completeness:msg('Completeness'),observedAt:msg('Observation time'),retainedAt:msg('Retention time'),bindingId:msg('Source binding'),sourceRevision:msg('Source revision')})[field]??field
function limitationLabel(code:string){switch(code){
 case 'informational-only':return msg('This comparison is informational. It does not approve, publish or establish requirement coverage.')
 case 'snapshot-only':return msg('Retained snapshots do not prove that all intervening source revisions were captured or establish provider approval.')
 case 'incomplete-extraction':return msg('The source extraction is incomplete. Missing pages are not deletions; review the retained original before deciding.')
 case 'unavailable-extraction':return msg('Source text is unavailable. An empty display does not mean the source is empty or unchanged.')
 case 'mixed-projections':return msg('This compares extracted source text with an administrator-edited Policy narrative. They are different projections, not identical source bytes.')
 case 'different-source-binding':return msg('The selected observations have different source bindings or identities. Review each source identity explicitly.')
 case 'equal-extracted-text':return msg('Equal extracted text does not prove equal original bytes or unchanged policy meaning.')
 default:return code
}}
const valueLabel=(value:unknown)=>value===undefined?msg('Unavailable'):typeof value==='string'?value:JSON.stringify(value)
/** Each opening authenticates both immutable selections. Polling never changes this pair. */
export function PolicyComparison({policyId,request,newerState=false,onDiscuss,onClose}:{policyId:string;request:PolicyComparisonRequest;newerState?:boolean;onDiscuss?:DiscussPolicyComparison;onClose?:()=>void}){
 useLocale()
 const key=JSON.stringify([policyId,request]),[attempt,setAttempt]=useState(0)
 const [state,setState]=useState<{key:string;result?:Comparison;error?:Error}>()
 useEffect(()=>{
  const controller=new AbortController();let current=true
  setState({key})
  const end=()=>{controller.abort();if(current)setState({key,error:new Error(msg('Policy comparison unavailable'))})}
  const unsubscribe=whenSessionEnds(end)
  if(sessionEnded())end()
  else void resolvePolicyComparison(policyId,request,controller.signal).then(result=>{if(current&&!controller.signal.aborted)setState({key,result})},()=>{if(current&&!controller.signal.aborted)setState({key,error:new Error(msg('Policy comparison unavailable'))})})
  return()=>{current=false;controller.abort();unsubscribe()}
  // The key contains the complete, closed immutable selection; object identity is irrelevant.
 },[key,attempt])
 const result=state?.key===key?state.result:undefined,error=state?.key===key?state.error:undefined
 const sideLabel=(selector:PolicyComparisonSelector,side?:Comparison['before'])=>[selector.kind==='publication'?msg('Local publication'):selector.kind==='revision'?msg('Saved revision'):msg('Retained source'),side?.metadata.title,side?.metadata.artifactVersion].filter(Boolean).join(' · ')
 const left=sideLabel(request.before,result?.before),right=sideLabel(request.after,result?.after)
 const complete=result?.before.metadata.completeness==='complete'&&result.after.metadata.completeness==='complete'
 const textAvailable=result&&result.before.text!==null&&result.after.text!==null
 const metadata=result?.differences.filter(change=>change.field!=='text')??[]
 return <section className={styles.root} aria-label={msg('Policy comparison')}>
  <div className={styles.toolbar}><h3>{msg('Compare Policy changes')}</h3>{onClose&&<Button variant="quiet" size="small" onClick={onClose}><IconClose/>{msg('Close comparison')}</Button>}</div>
  <div className={styles.selections}><p><strong>{msg('Before')}</strong> · {left}</p><p><strong>{msg('After')}</strong> · {right}</p></div>
  {newerState&&<p role="status">{msg('Newer saved or source state is available. This comparison keeps the exact selected pair.')}</p>}
  {error&&<Notice tone="warning" actions={<Button size="small" onClick={()=>setAttempt(value=>value+1)}><IconRetry/>{msg('Retry')}</Button>}>{msg('Policy comparison unavailable')}</Notice>}
  {!result&&!error&&<LoadingState>{msg('Loading exact comparison…')}</LoadingState>}
  {result&&<>
   {result.limitations.includes('unavailable-extraction')?<Notice tone="warning">{limitationLabel('unavailable-extraction')}</Notice>:result.limitations.includes('incomplete-extraction')&&<Notice tone="warning">{limitationLabel('incomplete-extraction')}</Notice>}
   {metadata.filter(change=>['title','artifactVersion'].includes(change.field)).map(change=><p className={styles.summary} key={change.field}><strong>{fieldLabel(change.field)}</strong>: {valueLabel(change.before)} → {valueLabel(change.after)}</p>)}
   {result.before.metadata.original&&result.after.metadata.original&&JSON.stringify(result.before.metadata.original)===JSON.stringify(result.after.metadata.original)&&(JSON.stringify(result.before.metadata.extraction)!==JSON.stringify(result.after.metadata.extraction)||JSON.stringify(result.before.metadata.processing)!==JSON.stringify(result.after.metadata.processing))&&<p role="status">{msg('Original bytes are identical; extraction or processing changed. This alone does not establish a Policy change.')}</p>}
   {textAvailable?<><CodeDiff before={result.before.text!} after={result.after.text!} beforeLabel={left} afterLabel={right} title={msg('Compare Policy changes')} ariaLabel={msg('Policy text changes')} language="text"/>{result.before.text===result.after.text&&<p role="status">{complete?msg('Visible text is identical. Review identity and metadata changes separately.'):msg('Readable text is identical; extraction is incomplete. Missing content cannot be compared.')}</p>}{result.before.text!==result.after.text&&result.before.text!.replace(/\r\n?/g,'\n')===result.after.text!.replace(/\r\n?/g,'\n')&&<p role="status">{msg('Stored line endings differ. The display may render those lines alike; retained text is unchanged by this view.')}</p>}</>:<p role="status">{msg('Both text projections are required for a diff. Unavailable content is not shown as an empty deletion.')}</p>}
   <div className={styles.toolbar}>{onDiscuss&&<Button size="small" variant="quiet" onClick={()=>onDiscuss(request)}><IconAssistant/>{msg('Discuss changes')}</Button>}</div>
   {metadata.length>0&&<Disclosure title={msg('Metadata changes')}><div className={styles.metadata}>{metadata.map(change=><div key={change.field}><h4>{fieldLabel(change.field)}</h4><dl className={styles.facts}><dt>{msg('Before')}</dt><dd><pre>{valueLabel(change.before)}</pre></dd><dt>{msg('After')}</dt><dd><pre>{valueLabel(change.after)}</pre></dd></dl></div>)}</div></Disclosure>}
   <Disclosure title={msg('Identity and processing details')}><div className={styles.identities}>{[result.before,result.after].map((side,index)=><div key={index}><h4>{index===0?left:right}</h4><p className={styles.exact}>{comparisonSelectorLabel(side.selector)}</p><dl className={styles.facts}><dt>{msg('Content')}</dt><dd>{side.metadata.kind==='narrative'?msg('Saved Policy narrative'):msg('Extracted source text')}</dd><dt>{msg('Completeness')}</dt><dd>{side.metadata.completeness==='complete'?msg('Complete'):side.metadata.completeness==='partial'?msg('Incomplete extraction'):msg('Unavailable')}</dd><dt>{msg('Retained identity')}</dt><dd><pre>{JSON.stringify(side.identity,null,2)}</pre></dd>{side.metadata.source&&<><dt>{msg('Source identity')}</dt><dd><pre>{JSON.stringify(side.metadata.source,null,2)}</pre></dd></>}{side.metadata.original&&<><dt>{msg('Original bytes')}</dt><dd><pre>{JSON.stringify(side.metadata.original,null,2)}</pre></dd></>}{side.metadata.extraction&&<><dt>{msg('Extraction and processor')}</dt><dd><pre>{JSON.stringify(side.metadata.extraction,null,2)}</pre></dd></>}</dl>{side.metadata.processing&&<><p>{msg('Page processing')}: {side.metadata.processing.status}{side.metadata.processing.truncated?' · '+msg('Truncated'):''}</p><ul>{side.metadata.processing.pages.map(page=><li key={page.number}>{msg('Page {{number}}',{number:page.number})}: {page.status} · {page.extraction} · {page.chars} {msg('characters')}{page.unmapped?' · '+msg('Unmapped content'):''}</li>)}</ul></>}</div>)}</div></Disclosure>
   <Disclosure title={msg('Comparison scope')}><div className={styles.notices}>{result.limitations.filter(code=>!['incomplete-extraction','unavailable-extraction'].includes(code)).map(code=><p key={code}>{limitationLabel(code)}</p>)}</div></Disclosure>
  </>}
 </section>
}
