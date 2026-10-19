import {useRef,useState} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {deskFetch} from '../files/client'
import {privateQuery} from '../data/privateQuery'
import {adoptionListQuery,adoptionQuery} from '../policies/adoptionClient'
import {Button} from '../ui/Button'
import {Select} from '../ui/Select'
import {msg} from '../i18n'
import styles from './JobsView.module.css'

interface Registration {version:1;state:'unregistered'|'registered';eligible:boolean;reason:string;adoptionId?:string;reviewId?:string;resource?:{jobId:string;jobRevision:string;releaseId:string};registration?:{revision:string;digest:string}}
async function read<T>(path:string,body?:unknown):Promise<T>{
 const response=await deskFetch('/api/operations/'+path,body===undefined?{}:{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
 let value:any
 try{value=await response.json()}catch{throw Error(msg('Execution authority is unavailable. Preserve the original operation identity and inspect its status.'))}
 if(!response.ok)throw Error(typeof value?.message==='string'?value.message:msg('Execution authority refused this request.'))
 return value as T
}
export function useManualExecution(){return useQuery({...privateQuery,queryKey:['policy-execution'],queryFn:async()=>{
 const response=await deskFetch('/api/operations/policy-execution')
 // Earlier Desk versions have no admission surface. No command is attempted.
 if(response.status===404)return {enabled:false}
 const value=await response.json()
 if(!response.ok||value.version!==1||typeof value.enabled!=='boolean'||value.enabled&&value.profile!=='local-admin-manual-pack/1')throw Error(msg('Execution authority status is unavailable.'))
 return value as {enabled:boolean}
}})}
export function useExecutionRegistration(jobId:string,enabled:boolean){return useQuery({...privateQuery,enabled,queryKey:['policy-registration',jobId],queryFn:async()=>{
 const value=await read<Registration>(`jobs/${jobId}/policy-registration`)
 if(value.version!==1||!['registered','unregistered'].includes(value.state)||typeof value.eligible!=='boolean'||value.state==='registered'&&value.resource?.jobId!==jobId)throw Error(msg('The exact job registration could not be verified.'))
 return value
}})}
export function ExecutionRegistration({jobId}:{jobId:string}){
 const status=useManualExecution(),enabled=status.data?.enabled===true
 const registration=useExecutionRegistration(jobId,enabled),client=useQueryClient()
 const choices=useQuery({...adoptionListQuery(),enabled:enabled&&registration.data?.state==='unregistered'})
 const [selected,setSelected]=useState<string>(),[busy,setBusy]=useState(false),[error,setError]=useState<unknown>()
 const detail=useQuery({...adoptionQuery(selected),enabled:enabled&&!!selected&&registration.data?.state==='unregistered'})
 const pending=useRef<{operationId:string;adoptionId:string;reviewId:string}|undefined>(undefined)
 const review=detail.data?.document.reviews.find(r=>r.id===detail.data?.document.currentReviewId&&r.decision==='approve')
 async function register(){
  if(!selected||!review)return
  setBusy(true);setError(undefined)
  try{
   if(pending.current&&(pending.current.adoptionId!==selected||pending.current.reviewId!==review.id))throw Error(msg('Inspect the pending registration before selecting another review.'))
   pending.current??={operationId:crypto.randomUUID(),adoptionId:selected,reviewId:review.id}
   await read(`jobs/${jobId}/policy-registration`,pending.current)
   await client.invalidateQueries({queryKey:['policy-registration',jobId]})
  }catch(e){setError(e)}finally{setBusy(false)}
 }
 if(!enabled)return status.error?<p role="alert" className={styles.note}>{msg('Execution authority status is unavailable. Sign in with a current local administrator session.')}</p>:null
 const saved=registration.data
 return <section className={styles.stack}><h2>{msg('Manual execution admission')}</h2><p className={styles.note}>{msg('This workspace requires an exact job release and approved Policy adoption. Triggers, mapped inputs, workflow runs and upgrades are unavailable.')}</p>
 {registration.error&&<p role="alert">{msg('Registration status is unavailable. No new execution permission is inferred.')}</p>}
 {saved?.state==='registered'?<><p>{saved.eligible?msg('Registered and currently eligible. Each submission is checked again before starting.'):msg('Registered; execution is currently held.')}</p><p className="quiet">{saved.reason}</p><p><code>{saved.adoptionId}</code> · <code>{saved.reviewId}</code></p></>:saved?.state==='unregistered'?<>
 <Select id={'admission-'+jobId} value={selected} onValueChange={setSelected} disabled={busy||!!pending.current} placeholder={msg('Choose an approved adoption')} options={(choices.data?.items??[]).filter(a=>a.reviewState==='reviewed').map(a=>({value:a.id,label:a.pack.artifactId+' · '+a.pack.version+' · '+a.id}))}/>
 {detail.data&&<p className="quiet">{msg('The complete retained dependency closure will be bound to this immutable release.')}</p>}
 <div><Button disabled={busy||!review||!!detail.error} onClick={()=>{void register()}}>{busy?msg('Registering…'):pending.current?msg('Recover exact registration'):msg('Register this release')}</Button> <Button disabled={busy} onClick={()=>{void registration.refetch()}}>{msg('Inspect registration')}</Button></div>
 {(choices.error||detail.error)&&<p role="alert">{msg('Approved adoption material is unavailable. Open Policy adoptions to review it.')}</p>}
 </>:null}
 {!!error&&<p role="alert">{error instanceof Error?error.message:msg('Registration is unresolved. Inspect its status before continuing.')}</p>}
 </section>
}
