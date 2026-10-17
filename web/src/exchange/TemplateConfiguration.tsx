import {useMemo,useRef,useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {useNavigate} from 'react-router-dom'
import {msg} from '../i18n'
import {privateQuery} from '../data/privateQuery'
import {usePack,usePacks} from '../mcp/queries'
import {Field,FieldGroup} from '../ui/Field'
import {Input} from '../ui/Input'
import {Select} from '../ui/Select'
import {Button} from '../ui/Button'
import {Notice} from '../ui/Notice'
import {LoadingState} from '../ui/LoadingState'
import {Disclosure} from '../ui/Disclosure'
import {jobsAPI} from '../jobs/client'
import {saveJobDraft} from '../jobs/drafts'
import {parseMapping} from '../jobs/mappedInputs'
import {MappedCaseFields} from '../jobs/MappedCaseFields'
import type {ProfileEntry} from '../jobs/dialectTypes'
import {PageBindingControls} from '../jobs/PageBindingControls'
import type {BindingDocument} from '../dialect/types'
import {profileMatches} from '../dialect/bindings'
import {useRegisteredChanges} from '../shell/UnsavedChanges'
import {exchangeAPI,type Candidate,type ImportRecord} from './client'
import styles from './TemplatesPage.module.css'

/** Definitions remain immutable. Only installation assignments and case values
 * belong to this form; the ordinary Job review owns validation and execution. */
export function TemplateConfiguration({candidate,origin}:{candidate:Candidate;origin:ImportRecord}) {
 const navigate=useNavigate(),packs=usePacks()
 const profiles=useQuery({...privateQuery,queryKey:['job-input-profiles'],queryFn:({signal})=>jobsAPI<ProfileEntry[]>('input-profiles',undefined,undefined,signal)})
 const [packId,setPackId]=useState(''),pack=usePack(packId)
 const [name,setName]=useState(candidate.entry.title),[caseText,setCaseText]=useState('{}'),[valid,setValid]=useState(true)
 const [selection,setSelection]=useState<Record<string,string>>({}),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const flight=useRef(false),draftKey=useRef(crypto.randomUUID())
 const parsed=useMemo(()=>{try{return {mapping:parseMapping(candidate.document)}}catch(e){return {error:(e as Error).message}}},[candidate.document])
 const mapping=parsed.mapping
 const sources=(mapping?.sources??[]).filter(source=>(source.kind==='research'||source.kind==='operation')&&!source.profile)
 const unsupported=(mapping?.sources??[]).filter(source=>!['operation','selected-file','research'].includes(source.kind))
 const assignments=sources.flatMap(source=>[{source,role:'answer' as const},...(source.kind==='research'?source.capabilities.filter((c):c is 'search'|'page'=>c==='search'||c==='page').map(role=>({source,role})):[])])
 const chosen=(key:string)=>profiles.data?.find(p=>p.profile.id===selection[key])
 const complete=!unsupported.length&&!!mapping&&!!pack.data&&!pack.error&&!profiles.error&&!profiles.isPending&&!!name.trim()&&valid&&assignments.every(({source,role})=>{const p=chosen(source.name+':'+role);return !!p&&profileMatches(source,p.profile,role)})
 const clearGuard=useRegisteredChanges(!!packId||caseText!=='{}'||Object.keys(selection).length>0,msg('Discard unsaved template configuration?'),{busy,name})
 async function save(){
  if(flight.current||!complete||!mapping)return
  flight.current=true;setBusy(true);setError('')
  try{
   await exchangeAPI('imports/'+origin.id+'/check',{pack:pack.data!.raw,document:candidate.document})
   const binding:BindingDocument={dialect:candidate.digest,sources:{}}
   for(const source of sources){
    const ai=chosen(source.name+':answer')!
    const pin={profile:ai.profile.id,profileDigest:ai.digest}
    if(source.kind==='research'){
     const webProfiles:NonNullable<BindingDocument['sources'][string]['webProfiles']>={}
     for(const capability of source.capabilities){if(capability==='answer')continue;const p=chosen(source.name+':'+capability)!;webProfiles[capability]={profile:p.profile.id,profileDigest:p.digest}}
     binding.sources[source.name]={...pin,webProfiles}
    }else binding.sources[source.name]=pin
   }
   const saved=await saveJobDraft(draftKey.current,{name:name.trim(),packId,inputMode:'mapped',facts:'{}',supplied:false,evidence:'',mapped:{text:candidate.document,caseText,...(sources.length?{importText:JSON.stringify({dialect:mapping,binding})}:{}),exchangeImportId:origin.id}})
   clearGuard();navigate('/jobs/new?draft='+saved.draft.id)
  }catch(e){setError((e as Error).message)}finally{flight.current=false;setBusy(false)}
 }
 return <section className={styles.stack} aria-label={msg('Configure template')}><h3>{msg('Configure template')}</h3>
  <FieldGroup><Field label={msg('Name')}>{props=><Input {...props} value={name} disabled={busy} onChange={e=>setName(e.target.value)}/>}</Field><Field label={msg('Pack')}>{props=><Select {...props} value={packId} disabled={busy} placeholder={msg('Choose a saved pack')} options={(packs.data?.packs??[]).map(p=>({value:p.id,label:p.id}))} onValueChange={setPackId}/>}</Field></FieldGroup>
  {!!unsupported.length&&<Notice tone="warning">{msg('These source kinds are not supported by the current Runner.')} {unsupported.map(source=>source.kind).join(', ')}</Notice>}
  {profiles.isPending&&<LoadingState inline>{msg('Loading connections…')}</LoadingState>}
  {assignments.map(({source,role})=>{const options=(profiles.data??[]).filter(p=>profileMatches(source,p.profile,role));const key=source.name+':'+role;return <div key={key} className={styles.stack}><Field label={`${source.name} · ${source.kind==='research'?role:msg('Connection')}`}>{props=><Select {...props} value={selection[key]??''} disabled={busy} placeholder={msg('Choose a connection')} options={options.map(p=>({value:p.profile.id,label:p.profile.id}))} onValueChange={id=>setSelection(old=>({...old,[key]:id}))}/>}</Field>{role==='page'&&<PageBindingControls disabled={busy||profiles.isPending||!!profiles.error} context={JSON.stringify([candidate.digest,source.name,selection[key]??''])} onBind={pin=>setSelection(old=>({...old,[key]:pin.profile}))}/>}</div>})}
  {sources.filter(s=>s.kind==='research').map(source=><Disclosure key={source.name} title={source.name}>{source.kind==='research'&&<><p>{source.model} · {source.capabilities.join(', ')}</p><dl>{source.fields.map(field=><div key={field.name}><dt>{field.name} · {field.type}{field.required?' · '+msg('Required'):''}</dt><dd>{field.description}</dd></div>)}</dl></>}</Disclosure>)}
  {pack.data&&mapping&&<MappedCaseFields doc={pack.data.document} mapping={mapping} text={caseText} disabled={busy} onChange={setCaseText} onValid={setValid}/>}
  <p className={styles.note}>{msg('The template stays unchanged. Connection assignments and case values are saved in a job draft for review.')}</p>
  {sources.some(s=>s.kind==='research')&&<p className={styles.note}>{msg('Research sources currently require retained response files. Runner verifies their receipts before preparing inputs.')}</p>}
  {(error||parsed.error||profiles.error||packs.error||pack.error)&&<Notice tone="warning">{error||parsed.error||profiles.error?.message||packs.error?.message||pack.error?.message}</Notice>}
  <div><Button size="small" disabled={busy||!complete} onClick={()=>void save()}>{busy?msg('Saving…'):msg('Create job draft')}</Button></div>
 </section>
}
