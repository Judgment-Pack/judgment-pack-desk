import {useRef,useState} from 'react'
import {useInfiniteQuery,useQueryClient} from '@tanstack/react-query'
import {formatDate,msg} from '../i18n'
import {privateQuery} from '../data/privateQuery'
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
 publication:{id:string;at:string;dialect:{preparationId:string;revision:string;variant:string};documents:{id:string;version:string}[]}
 document:string;digest:string;saved:boolean
}
/** Publishing is separate from recipe autosave. Checks bind the exact saved
 * revision, selected execution kind and version; confirmation never runs a job. */
export function DialectPublication({saved,disabled=false}:{saved:PreparationReply;disabled?:boolean}) {
 const cache=useQueryClient(),inFlight=useRef(false)
 const [variant,setVariant]=useState('asserted'),[version,setVersion]=useState('1.0.0')
 const [result,setResult]=useState<{identity:string;answer:DialectPublicationAnswer}>(),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const path='/'+saved.document.id+'/publications',key=['dialect-publications',saved.document.id]
 const history=useInfiniteQuery({...privateQuery,queryKey:[...key,variant],initialPageParam:0,getNextPageParam:(last:{items:DialectPublicationAnswer[];next?:number})=>last.next,queryFn:({signal,pageParam})=>dialectAPI<{items:DialectPublicationAnswer[];next?:number}>(`${path}?variant=${variant}${pageParam?'&before='+pageParam:''}`,'GET',undefined,undefined,signal)})
 const items=history.data?.pages.flatMap(page=>page.items)??[]
 const latest=items[0]
 const identity=JSON.stringify([saved.document.id,saved.sha256,variant,version])
 const answer=result?.identity===identity?result.answer:undefined
 useRegisteredChanges(false,'',{name:saved.document.name,busy,kind:'activity'})
 async function act(action:'check'|'publish') {
  if(inFlight.current||disabled||!version.trim()||(action==='publish'&&(!answer||answer.saved)))return
  inFlight.current=true;setBusy(true);setError('')
  try {
   const next=await dialectAPI<DialectPublicationAnswer>(path,'POST',{action,revision:saved.sha256,variant,version,...(action==='publish'?{reviewed:true,checkDigest:answer!.digest}:{})})
   setResult({identity,answer:next})
   if(next.saved)await cache.invalidateQueries({queryKey:key})
  } catch(e){setError((e as Error).message);setResult(undefined)} finally {inFlight.current=false;setBusy(false)}
 }
 return <RecipeSection title={msg('Published dialect')}>
  <p className={styles.note}>{msg('Publish a checked mapping version for jobs. Existing jobs keep their reviewed version.')}</p>
  {disabled&&<p className={styles.note}>{msg('Save recipe changes before publishing.')}</p>}
  <FieldGroup>
   <Field label={msg('Records')} >{props=><Select {...props} value={variant} disabled={disabled||busy} onValueChange={setVariant} options={[{value:'asserted',label:msg('Imported records')},{value:'generated',label:msg('Reviewed research')}]}/>}</Field>
   <Field label={msg('Version')}>{props=><Input {...props} value={version} maxLength={256} disabled={disabled||busy} onChange={event=>setVersion(event.target.value)}/>}</Field>
  </FieldGroup>
  <ArtifactVersionOptions base={latest?.publication.documents[0]?.version} onSelect={setVersion} disabled={disabled||busy}/>
  {history.isPending?<LoadingState inline>{msg('Loading versions…')}</LoadingState>:history.error?<Notice tone="warning" actions={<Button size="small" onClick={()=>void history.refetch()}>{msg('Retry')}</Button>}>{history.error.message}</Notice>:latest&&<p className={styles.note}>{msg('Saved version {{version}}',{version:latest.publication.documents[0]?.version??''})}</p>}
  {error&&<Notice tone="warning">{error}</Notice>}
  {answer&&<><p className={styles.note} role="status">{answer.saved?msg('Version published'):msg('Checked · Ready to publish')}</p><Disclosure title={msg('Review proposed changes')}><CodeBlock text={answer.document} label={msg('Dialect')}/><p className={styles.note}>{answer.digest}</p></Disclosure></>}
  <div className={styles.formActions}>
   <Button size="small" disabled={disabled||busy||!version.trim()} onClick={()=>void act('check')}>{busy?<LoadingState inline>{msg('Working…')}</LoadingState>:msg('Check proposal')}</Button>
   <Button size="small" variant="primary" disabled={disabled||busy||!answer||answer.saved} onClick={()=>void act('publish')}>{msg('Publish version')}</Button>
  </div>
  {!!items.length&&<Disclosure title={msg('Version history')}><div className={styles.stack}>{items.map(item=><Disclosure key={item.publication.id} title={`${item.publication.documents[0]?.version} · ${item.publication.dialect.variant==='generated'?msg('Reviewed research'):msg('Imported records')}`}><p className={styles.note}>{formatDate(new Date(item.publication.at),{dateStyle:'medium',timeStyle:'short'})}</p><CodeBlock text={item.document} label={msg('Dialect')}/></Disclosure>)}</div>{history.hasNextPage&&<Button size="small" disabled={history.isFetchingNextPage} onClick={()=>void history.fetchNextPage()}>{msg('Load more')}</Button>}</Disclosure>}
 </RecipeSection>
}
