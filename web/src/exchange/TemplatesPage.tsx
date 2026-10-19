import {TemplateConfiguration} from './TemplateConfiguration'
import {useRef,useState} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {FileRequestError,StaleWrite} from '../files/client'
import {msg,useLocale} from '../i18n'
import {PageHeader,PageBody} from '../ui/PageLayout'
import {Button} from '../ui/Button'
import {Field,FieldGroup} from '../ui/Field'
import {Input} from '../ui/Input'
import {Select} from '../ui/Select'
import {Disclosure} from '../ui/Disclosure'
import {CodeBlock} from '../ui/CodeBlock'
import {CodeDiff} from '../ui/CodeDiff'
import {LoadingState} from '../ui/LoadingState'
import {Notice} from '../ui/Notice'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {useRegisteredChanges} from '../shell/UnsavedChanges'
import {exchangeAPI,exchangeImportsQuery,exchangeImportQuery,EXCHANGE_KEY,type CatalogReview,type Candidate} from './client'
import styles from './TemplatesPage.module.css'

/** A catalog review is a frozen candidate, never a polling query. Import saves
 * its exact server-retained bytes. It does not publish, bind or execute them. */
const releaseTagExample='v1.0.0'
export function TemplatesPage(){
 useLocale()
 const cache=useQueryClient(),history=useQuery(exchangeImportsQuery())
 const [location,setLocation]=useState('https://github.com/Judgment-Pack/judgment-pack-exchange.git'),[tag,setTag]=useState(''),[review,setReview]=useState<CatalogReview>(),[selection,setSelection]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState<unknown>(),[saved,setSaved]=useState(''),[installed,setInstalled]=useState('')
 const active=useRef(false)
 useRegisteredChanges(false,'',{kind:'activity',busy})
 const candidate=review?.review.release.candidates.find(c=>c.row.id===selection)
 const priorRecord=review?.previous[selection]
 const prior=useQuery({...exchangeImportQuery(priorRecord??''),enabled:!!priorRecord})
 const retained=useQuery({...exchangeImportQuery(installed),enabled:!!installed})
 function change(update:()=>void){update();setReview(undefined);setSelection('');setSaved('');setError(undefined)}
 async function check(){if(active.current)return;active.current=true;setBusy(true);setError(undefined);setReview(undefined);setSaved('');try{const next=await exchangeAPI<CatalogReview>('review',{location:location.trim(),tag:tag.trim()});setReview(next);setSelection(next.review.release.candidates[0]?.row.id??'')}catch(e){setError(e)}finally{active.current=false;setBusy(false)}}
 async function save(){if(active.current||!review||!candidate)return;active.current=true;setBusy(true);setError(undefined);try{const next=await exchangeAPI<{id:string}>('imports',{reviewId:review.review.id,entryId:candidate.row.id,base:review.review.base,reviewed:true});setSaved(candidate.row.id);setInstalled(next.id);setReview(undefined);setSelection('');await cache.invalidateQueries({queryKey:EXCHANGE_KEY})}catch(e){setError(e);if(e instanceof StaleWrite||e instanceof FileRequestError&&e.status>=400&&e.status<500){setReview(undefined);setSelection('')}}finally{active.current=false;setBusy(false)}}
 const unavailable=review?.review.release.unavailable??[]
 return <><PageHeader title={msg('Dialects')} titleHref="/dialects" context={msg('Exchange templates')}/>
  <QueryStatusNotice id="exchange-history" label={msg('Templates unavailable')} error={history.error} onRetry={()=>history.refetch()} retrying={history.isFetching}/>
  <PageBody width="wide"><div className={styles.stack}>
   <p className={styles.note}>{msg('Import a tagged Exchange release for review. Imported definitions stay separate from local connections and run approvals.')}</p>
   <FieldGroup><Field label={msg('Git catalog')} >{props=><Input {...props} value={location} disabled={busy} onChange={e=>change(()=>setLocation(e.target.value))}/>}</Field><Field label={msg('Release tag')}>{props=><Input {...props} value={tag} placeholder={releaseTagExample} disabled={busy} onChange={e=>change(()=>setTag(e.target.value))}/>}</Field></FieldGroup>
   <div><Button size="small" disabled={busy||!location.trim()||!tag.trim()} onClick={()=>void check()}>{busy?<LoadingState inline>{msg('Working…')}</LoadingState>:msg('Review release')}</Button></div>
   {!!error&&<Notice tone="warning" actions={<Button size="small" disabled={busy} onClick={()=>void(review&&candidate?save():check())}>{review&&candidate?msg('Retry'):msg('Review release')}</Button>}>{error instanceof Error?error.message:msg('Request failed')}</Notice>}
   {saved&&<p role="status" className={styles.note}>{msg('Template imported')}</p>}
   {review&&<section className={styles.stack} aria-label={msg('Review release')}><p className={styles.note}>{review.review.release.tag} · <code>{review.review.release.commit}</code></p>
    {!!review.review.release.candidates.length&&<Field label={msg('Template')}>{props=><Select {...props} value={selection} disabled={busy} options={review.review.release.candidates.map(c=>({value:c.row.id,label:c.entry.title}))} onValueChange={value=>{setSelection(value);setSaved('');setError(undefined)}}/>}</Field>}
    {!review.review.release.candidates.length&&!unavailable.length&&<p>{msg('This release has no templates.')}</p>}
    {candidate&&<><TemplateDetails candidate={candidate}/>
     {priorRecord&&<>{prior.isPending&&<LoadingState inline>{msg('Loading changes…')}</LoadingState>}{prior.error&&<Notice tone="warning" actions={<Button size="small" onClick={()=>void prior.refetch()}>{msg('Retry')}</Button>}>{prior.error.message}</Notice>}{prior.data&&<Disclosure title={msg('Review proposed changes')}>{[...new Set([...Object.keys(prior.data.candidate.files),...Object.keys(candidate.files)])].filter(name=>prior.data.candidate.files[name]!==candidate.files[name]).map(name=><Disclosure key={name} title={name}><CodeDiff before={prior.data.candidate.files[name]??''} after={candidate.files[name]??''}/></Disclosure>)}{review.changes[selection]?.files.length===0&&<p className={styles.note}>{msg('No changes')}</p>}</Disclosure>}</>}

     <div className={styles.actions}><Button size="small" variant="primary" disabled={busy||saved===selection||!!priorRecord&&(prior.isPending||!!prior.error)} onClick={()=>void save()}>{msg('Import template')}</Button></div>
    </>}
    {Object.entries(review.changes).filter(([,c])=>c.withdrawn).map(([id])=><Notice key={id} tone="warning">{msg('Withdrawn from this release; the retained import is unchanged.')} {id}</Notice>)}
    {!!unavailable.length&&<Disclosure title={msg('Unavailable templates')}>{unavailable.map(item=><p key={item.id}><strong>{item.id}</strong> · {item.reason}</p>)}</Disclosure>}
   </section>}
   <section className={styles.stack} aria-label={msg('Imported templates')}><h2>{msg('Imported templates')}</h2>
    {history.isPending?<LoadingState>{msg('Loading templates…')}</LoadingState>:history.data?.items.length?<Field label={msg('Version history')}>{props=><Select {...props} value={installed} placeholder={msg('Choose a template')} options={[...history.data.items].reverse().map(r=>({value:r.id,label:`${r.entryId} · ${r.tag}`}))} onValueChange={setInstalled}/>}</Field>:!history.error&&<p className={styles.note}>{msg('No imported templates.')}</p>}
    {installed&&<>{retained.isPending&&<LoadingState>{msg('Loading template…')}</LoadingState>}{retained.error&&<Notice tone="warning" actions={<Button size="small" onClick={()=>void retained.refetch()}>{msg('Retry')}</Button>}>{retained.error.message}</Notice>}{retained.data&&<><TemplateDetails candidate={retained.data.candidate}/><TemplateConfiguration key={retained.data.import.id} candidate={retained.data.candidate} origin={retained.data.import}/><p className={styles.note}>{retained.data.import.tag} · <code>{retained.data.import.commit}</code></p></>}</>}


   </section>
  </div></PageBody></>
}
function TemplateDetails({candidate}:{candidate:Candidate}){return <div className={styles.stack}><div><h3>{candidate.entry.title}</h3>{candidate.entry.description&&<p>{candidate.entry.description}</p>}<p className={styles.note}>{candidate.entry.author} · {candidate.entry.licence}</p></div><Disclosure title={msg('Sources')}><p>{candidate.entry.sourceKinds.join(', ')}</p><CodeBlock text={candidate.files['dialect.json']} label={msg('Dialect')}/></Disclosure><Disclosure title={msg('Documentation')}><pre className={styles.documentation}>{candidate.files['README.md']}</pre></Disclosure><Disclosure title={msg('Technical details')}><CodeBlock text={candidate.files['entry.json']} label={msg('Template')}/><code className={styles.note}>{candidate.digest}</code></Disclosure></div>}
