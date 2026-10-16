import {usePublishDialectEditing} from './recipeEditing'
import {useEffect,useRef,useState} from 'react'
import {useInfiniteQuery,useQuery,useQueryClient} from '@tanstack/react-query'
import {formatDate,msg} from '../i18n'
import {privateQuery} from '../data/privateQuery'
import {ArtifactImpactFields} from '../artifacts/ArtifactImpactFields'
import {reviewMatches,type ArtifactReview,type ArtifactReference,type ArtifactImpact} from '../artifacts/review'
import {ArtifactVersionOptions} from '../artifacts/ArtifactVersionOptions'
import {Field,FieldGroup} from '../ui/Field'
import {Input} from '../ui/Input'
import {Select} from '../ui/Select'
import {Button} from '../ui/Button'
import {Notice} from '../ui/Notice'
import {LoadingState} from '../ui/LoadingState'
import {Disclosure} from '../ui/Disclosure'
import {CodeBlock} from '../ui/CodeBlock'
import {useRegisteredChanges} from '../shell/UnsavedChanges'
import {RecipeSection} from './RecipeSection'
import {dialectAPI,type PreparationReply} from './client'
import styles from './DialectsView.module.css'

export interface DialectPublicationAnswer {
 publication:{id:string;at:string;dialect:{preparationId:string;revision:string;variant:string;request:string};documents:{id:string;version:string}[]}
 document:string;digest:string;saved:boolean;basePublication:string;review?:ArtifactReference
}
/** Publishing is separate from recipe autosave. Checks bind the exact saved
 * revision, selected execution kind and version; confirmation never runs a job. */
export function DialectPublication({saved,disabled=false}:{saved:PreparationReply;disabled?:boolean}) {
 const cache=useQueryClient(),inFlight=useRef(false)
 const [variant,setVariant]=useState('asserted'),[version,setVersion]=useState('1.0.0')
 const [impact,setImpact]=useState<ArtifactImpact>({classification:'unknown',rationale:''})
 const [restored,setRestored]=useState('')
 const [result,setResult]=useState<{identity:string;answer:DialectPublicationAnswer}>(),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const path='/'+saved.document.id+'/publications',key=['dialect-publications',saved.document.id]
 const history=useInfiniteQuery({...privateQuery,queryKey:[...key,variant],initialPageParam:0,getNextPageParam:(last:{items:DialectPublicationAnswer[];next?:number})=>last.next,queryFn:({signal,pageParam})=>dialectAPI<{items:DialectPublicationAnswer[];next?:number}>(`${path}?variant=${variant}${pageParam?'&before='+pageParam:''}`,'GET',undefined,undefined,signal)})
 const recovery=useQuery({...privateQuery,queryKey:[...key,variant,saved.sha256,'review'],queryFn:({signal})=>dialectAPI<{review:ArtifactReview<DialectPublicationAnswer>|null}>(`${path}?variant=${variant}&review=latest`,'GET',undefined,undefined,signal)})
 const [edited,setEdited]=useState(false)
 // Guard competing chat writes without marking a busy operation as unsaved.
 usePublishDialectEditing(saved.document.id,edited||busy)
 const scope=saved.document.id+':'+saved.sha256+':'+variant
 useEffect(()=>{
  if(edited||recovery.isPending||recovery.isFetching||recovery.isPaused||!recovery.isFetchedAfterMount||recovery.error||restored===scope)return
  setRestored(scope)
  const retained=recovery.data?.review
  if(!retained||!reviewMatches(retained,`urn:judgmentpack:dialect:${saved.document.id}:${variant}`,saved.sha256))return
  const r=retained.review
  setVersion(r.proposedVersion);setImpact(r.impact)
  setResult({identity:JSON.stringify([saved.document.id,saved.sha256,variant,r.proposedVersion,r.impact]),answer:{...r.preview,review:retained.reference}})
 },[edited,scope,restored,recovery.data,recovery.error,recovery.isPending,recovery.isFetching,recovery.isPaused,recovery.isFetchedAfterMount,saved.document.id,saved.sha256,variant])
 const restoring=restored!==scope
 const items=history.data?.pages.flatMap(page=>page.items)??[]
 const latest=items[0]
 const identity=JSON.stringify([saved.document.id,saved.sha256,variant,version,impact])
 const answer=result?.identity===identity?result.answer:undefined
 useRegisteredChanges(edited,msg('Discard unsaved compatibility review?'),{label:msg('Compatibility impact'),busy,kind:'edit'})
 async function act(action:'check'|'publish') {
  if(inFlight.current||disabled||restoring||recovery.isPending||!!recovery.error||!impact.rationale.trim()||impact.classification==='unknown'||!version.trim()||(action==='publish'&&(!answer||answer.saved)))return
  inFlight.current=true;setBusy(true);setError('')
  try {
   const next=await dialectAPI<DialectPublicationAnswer>(path,'POST',{action,revision:saved.sha256,variant,version,impact,...(action==='publish'?{reviewed:true,checkDigest:answer!.digest,basePublication:answer!.basePublication,review:answer!.review}:{})})
   setResult({identity,answer:next});setEdited(false)
   await cache.invalidateQueries({queryKey:next.saved?key:[...key,variant,saved.sha256,'review']})
  } catch(e){setError((e as Error).message);setResult(undefined)} finally {inFlight.current=false;setBusy(false)}
 }
 return <RecipeSection title={msg('Published dialect')}>
  <p className={styles.note}>{msg('Publish a checked mapping version for jobs. Existing jobs keep their reviewed version.')}</p>
  {disabled&&<p className={styles.note}>{msg('Save recipe changes before publishing.')}</p>}
  <FieldGroup>
   <Field label={msg('Records')} >{props=><Select {...props} value={variant} disabled={disabled||busy||restoring||edited} onValueChange={setVariant} options={[{value:'asserted',label:msg('Imported records')},{value:'generated',label:msg('Reviewed research')}]}/>}</Field>
   <Field label={msg('Version')}>{props=><Input {...props} value={version} maxLength={256} disabled={disabled||busy||restoring} onChange={event=>{if(!disabled&&!busy&&!restoring){setVersion(event.target.value);setEdited(true)}}}/>}</Field>
  </FieldGroup>
  <ArtifactImpactFields value={impact} onChange={value=>{if(!disabled&&!busy&&!restoring){setImpact(value);setEdited(true)}}} disabled={disabled||busy||restoring||recovery.isPending}/>
  <ArtifactVersionOptions base={(answer?.basePublication?items.find(item=>item.publication.id===answer.basePublication):latest)?.publication.documents[0]?.version} onSelect={value=>{setVersion(value);setEdited(true)}} disabled={disabled||busy||restoring}/>
  {history.isPending?<LoadingState inline>{msg('Loading versions…')}</LoadingState>:history.error?<Notice tone="warning" actions={<Button size="small" onClick={()=>void history.refetch()}>{msg('Retry')}</Button>}>{history.error.message}</Notice>:latest&&<p className={styles.note}>{msg('Saved version {{version}}',{version:latest.publication.documents[0]?.version??''})}</p>}
  {recovery.error&&<Notice tone="warning" actions={<Button size="small" onClick={()=>void recovery.refetch()}>{msg('Retry')}</Button>}>{recovery.error.message}</Notice>}
  {error&&<Notice tone="warning">{error}</Notice>}
  {answer&&<><p className={styles.note} role="status">{answer.saved?msg('Version published'):msg('Checked · Ready to publish')}</p><Disclosure title={msg('Review proposed changes')}><CodeBlock text={answer.document} label={msg('Dialect')}/><p className={styles.note}>{answer.digest}</p></Disclosure></>}
  {edited&&restoring&&<Notice tone="warning">{msg('The dialect changed. Refresh and review the saved recipe again.')}</Notice>}
  <div className={styles.formActions}>
   {edited&&<Button size="small" disabled={busy} onClick={()=>{setEdited(false);setRestored('');setVersion('1.0.0');setImpact({classification:'unknown',rationale:''});setResult(undefined)}}>{msg('Discard')}</Button>}
   <Button size="small" disabled={disabled||busy||restoring||recovery.isPending||!!recovery.error||!impact.rationale.trim()||impact.classification==='unknown'||!version.trim()} onClick={()=>void act('check')}>{busy?<LoadingState inline>{msg('Working…')}</LoadingState>:msg('Check proposal')}</Button>
   <Button size="small" variant="primary" disabled={disabled||busy||restoring||recovery.isPending||!!recovery.error||!answer||answer.saved||!answer.review} onClick={()=>void act('publish')}>{msg('Publish version')}</Button>
  </div>
  {!!items.length&&<Disclosure title={msg('Version history')}><div className={styles.stack}>{items.map(item=><Disclosure key={item.publication.id} title={`${item.publication.documents[0]?.version} · ${item.publication.dialect.variant==='generated'?msg('Reviewed research'):msg('Imported records')}`}><p className={styles.note}>{formatDate(new Date(item.publication.at),{dateStyle:'medium',timeStyle:'short'})}</p><CodeBlock text={item.document} label={msg('Dialect')}/></Disclosure>)}</div>{history.hasNextPage&&<Button size="small" disabled={history.isFetchingNextPage} onClick={()=>void history.fetchNextPage()}>{msg('Load more')}</Button>}</Disclosure>}
 </RecipeSection>
}
