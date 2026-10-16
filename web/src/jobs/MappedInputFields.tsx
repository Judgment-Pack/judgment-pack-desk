import {PortableResearchControls} from './PortableResearchControls'
import {portableResearchAPI,researchContextKey,reviewPortableResearch,type PortableResearchChoice,type PortableResearchReference} from './portableResearch'
import type {ResearchLimits} from '../dialects/research'
import type {ProfileBinding} from '../dialect/types'
import {importBindings} from '../dialect/bindings'
import {RetainedJSON} from './wire'
import {checkDialect} from '../dialect/authoring'
import {LoadingState} from '../ui/LoadingState'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { msg } from '../i18n'
import type { MappedDraft } from './drafts'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { authorizeDrive, ConnectionRequestError, type DriveSelection } from '../connections/client'
import { DriveFilePicker } from '../connections/DriveFilePicker'
import { factFields } from '../packs/test-workspace/model'
import type { PackDocument } from '../mcp/types'
import { Button } from '../ui/Button'
import { TextArea } from '../ui/TextArea'
import { useConnections } from '../connections/catalog'
import { JobIntegrationPicker } from './JobIntegrationPicker'
import { setSourceIntegration } from './jobIntegrations'
import { Disclosure } from '../ui/Disclosure'
import { jobsAPI, type SourceInput, type InputPreview } from './client'
import type { ExecutableDialect, ProfileEntry, SourceV2 } from './dialectTypes'
import { isSourceV2 } from './dialectTypes'
import { parseMapping, parseMappedObject, parseMappedCase, prepareMappedInputs,PortableResearchRequired,type RetainedResearchPrefix } from './mappedInputs'
import { localSnapshot, pointerEscape } from './sourceInputs'
import { DialectEditor } from './DialectEditor'
import { MappedCaseFields } from './MappedCaseFields'
import { DialectReview, InputLineage } from './DialectReview'
import { mappingErrorMessage } from './mappingError'
import styles from './JobsView.module.css'

function starter(doc: PackDocument): ExecutableDialect {
 return {version:2,case:{parameters:{},facts:factFields(doc).map(f => ({target:f.path,source:`/facts${f.path}`})),evidence:(doc.evidenceRequirements ?? []).map(e => ({requirement:e.id,source:`/evidence/${pointerEscape(e.id)}`}))},sources:[]}
}
export function MappedInputFields({doc, packText, fixed, disabled, active=true, onChange, draft, onDraftChange, onPersistResearchDraft, onWorkChange, onPendingChange, onTransientChange}: {doc: PackDocument; packText:string; fixed?: ExecutableDialect; disabled: boolean; active?: boolean; onChange: (source: SourceV2 | undefined) => void; draft?: MappedDraft; onDraftChange?: (draft: MappedDraft) => void; onPersistResearchDraft?:(draft:MappedDraft,signal:AbortSignal)=>Promise<void>; onWorkChange?: (dirty: boolean) => void; onPendingChange?: (pending: boolean) => void; onTransientChange?: () => void}) {
 const effective=useEffectiveConfig(),config=effective.config.research
 const localConnections=effective.desk?.localGateway?.status==='ready'&&!effective.desk?.decoded?.values?.research?.gateway
 const connections=useConnections(localConnections)
 const profiles = useQuery({queryKey:['job-input-profiles'],queryFn:({signal}) => jobsAPI<ProfileEntry[]>('input-profiles',undefined,undefined,signal)})
 const [text,setText] = useState(() => draft?.text ?? JSON.stringify(fixed ?? starter(doc),null,2)), [caseText,setCaseText] = useState(draft?.caseText ?? '{"facts": {}, "evidence": {}}')
 const [researchRuns,setResearchRuns]=useState<Record<string,PortableResearchReference>>(draft?.researchRuns??{})
 const [sourceRevision,setSourceRevision]=useState(draft?.sourceRevision)
 const researchBasis=JSON.stringify([text,caseText,draft?.importText,sourceRevision])
 const [researchIdentity,setResearchIdentity]=useState<{basis:string;value:string}>()
 const researchContext=researchIdentity?.basis===researchBasis?researchIdentity.value:undefined
 useEffect(()=>{let alive=true;void researchContextKey(text,caseText,draft?.importText,sourceRevision).then(value=>{if(alive)setResearchIdentity({basis:researchBasis,value})});return()=>{alive=false}},[researchBasis])
 const imported=!!draft?.importText||!!draft?.exchangeImportId
 const [retainedResearch,setRetainedResearch]=useState<Record<string,RetainedJSON>>({})
 const [reconnect,setReconnect]=useState(false)
 const [choosing,setChoosing] = useState<string>(), pickerOpener=useRef<HTMLElement|null>(null)
 const [files,setFiles] = useState<Record<string,SourceInput['snapshot']>>({}), [selections,setSelections] = useState<Record<string,DriveSelection>>({})
 const [preview,setPreview] = useState<InputPreview>(), [error,setError] = useState(''), [progress,setProgress] = useState('')
 const [editorInvalid,setEditorInvalid]=useState(false),[caseValid,setCaseValid]=useState(true)
 const operation = useRef<AbortController | null>(null), uploads = useRef<Record<string, HTMLInputElement | null>>({})
 const initial = useRef({text,caseText})
 const draftChanged = useRef(onDraftChange); draftChanged.current = onDraftChange
 useEffect(() => { if (text !== initial.current.text || caseText !== initial.current.caseText || draft) draftChanged.current?.({text,caseText,...(draft?.importText?{importText:draft.importText}:{}),...(draft?.exchangeImportId?{exchangeImportId:draft.exchangeImportId}:{}),researchRuns,sourceRevision}) }, [text,caseText,researchRuns,sourceRevision])
 useEffect(() => { onWorkChange?.(text !== initial.current.text || caseText !== initial.current.caseText || editorInvalid || Object.keys(files).length > 0 || Object.keys(selections).length > 0 || Object.keys(retainedResearch).length > 0) }, [text,caseText,editorInvalid,files,selections,retainedResearch,onWorkChange])
 useEffect(() => { onPendingChange?.(editorInvalid || !caseValid) }, [editorInvalid,caseValid,onPendingChange])
 const {mapping,mappingError} = useMemo(() => {try {return {mapping:parseMapping(text)}} catch(error) {return {mappingError:error}}},[text])
 const binding=useMemo(()=>{try{return draft?.importText?importBindings(parseMappedObject(draft.importText).binding):undefined}catch{return undefined}},[draft?.importText])
 const context = JSON.stringify([packText,binding,config.gateway,config.documents,config.managedLocal,profiles.data?.filter(p=>mapping?.sources?.some(s=>s.profile===p.profile.id)),fixed,connections.entries.filter(e=>mapping?.sources?.some(s=>s.provider==='google-drive'&&e.descriptor.id==='google-drive'||profiles.data?.some(p=>p.profile.id===s.profile&&p.profile.source===(e.descriptor.source?.id??e.descriptor.id)))).map(e=>[e.descriptor.id,e.status.data?.state,e.status.data?.account?.id,e.status.data?.resource?.id]),connections.isError])
 const busy = disabled || Boolean(progress)
 function invalidate() {setPreview(undefined); onChange(undefined); setError('')}
 useEffect(() => {setReconnect(false);setChoosing(undefined);setPreview(undefined); onChange(undefined); setSelections({}); setProgress(''); setError(''); return () => {operation.current?.abort(); operation.current=null}},[context,onChange])
 useEffect(() => {if(fixed) {setText(JSON.stringify(fixed,null,2));setFiles({});setSelections({})}},[fixed])
 function updateMapping(next: ExecutableDialect) {
  invalidate(); setText(JSON.stringify(next,null,2))
  // Output-pointer edits do not change the selected artifact. Retire it only
  // when the source's provider or verified profile changes.
  const compatible=(name:string)=>{const a=mapping?.sources?.find(s=>s.name===name),b=next.sources?.find(s=>s.name===name);return a&&b&&a.kind===b.kind&&a.provider===b.provider&&a.profile===b.profile&&a.profileDigest===b.profileDigest}
  setFiles(previous=>Object.fromEntries(Object.entries(previous).filter(([name])=>compatible(name))))
  setSelections(previous=>Object.fromEntries(Object.entries(previous).filter(([name])=>compatible(name))))
 }
 function addSource(profile?: ProfileEntry) {
  if(!mapping)return
  try {updateMapping(setSourceIntegration(mapping,profile))} catch {setError(msg('The inputs could not be mapped.'))}
 }
 async function select(name: string, file: File) {
  if(operation.current || disabled) return
  const active = new AbortController();operation.current=active;invalidate();setProgress(msg('Reading files…'))
  try {
   const snapshot=await localSnapshot(file);active.signal.throwIfAborted();setFiles(v=>({...v,[name]:snapshot}));setSourceRevision(crypto.randomUUID());onTransientChange?.()
  } catch(e) {if(!active.signal.aborted)setError(e instanceof Error?e.message:msg('The file could not be read.'))}
  finally {if(operation.current===active){operation.current=null;setProgress('')}}
 }
 async function selectResearch(name:string,file:File){
  if(operation.current||disabled)return
  const task=new AbortController();operation.current=task;invalidate();setProgress(msg('Reading files…'))
  try{
   if(file.size>(1<<20))throw Error(msg('The file is too large.'))
   const response=new RetainedJSON(new TextDecoder('utf-8',{fatal:true}).decode(await file.arrayBuffer()));task.signal.throwIfAborted()
   if(operation.current!==task)return
   invalidate();setRetainedResearch(previous=>({...previous,[name]:response}));setSourceRevision(crypto.randomUUID());onTransientChange?.()
  }catch(e){if(!task.signal.aborted)setError((e as Error).message)}
  finally{if(operation.current===task){operation.current=null;setProgress('')}}
 }
 async function retainedPrefix(signal:AbortSignal,exclude?:string):Promise<RetainedResearchPrefix|undefined>{
  const context=await researchContextKey(text,caseText,draft?.importText,sourceRevision)
  const runs=await Promise.all(Object.entries(researchRuns).filter(([,ref])=>ref.context===context).map(async([name,ref])=>({name,run:await portableResearchAPI('/'+ref.id,undefined,undefined,signal)})))
  const ready=runs.filter(row=>!!row.run.responseText&&row.name!==exclude&&(!exclude||!Object.hasOwn((parseMappedCase(row.run.inputText).source as SourceV2).sources,exclude))).sort((a,b)=>Object.keys((parseMappedCase(b.run.inputText).source as SourceV2).sources).length-Object.keys((parseMappedCase(a.run.inputText).source as SourceV2).sources).length)[0]
  return ready?{name:ready.name,input:new RetainedJSON(ready.run.inputText),response:new RetainedJSON(ready.run.responseText)}:undefined
 }
 function bindResearch(name:string,pin:ProfileBinding,capability:'answer'|'search'|'page'){
  if(!mapping||fixed)return
  invalidate()
  if(imported){
   if(!draft?.importText||!binding)return
   const original=parseMappedObject(draft.importText)
   const prior=binding.sources[name]
   const next={...binding,sources:{...binding.sources,[name]:capability==='answer'?{webProfiles:{},...prior,...pin}:{...prior,webProfiles:{...prior?.webProfiles,[capability]:pin}}}}
   draftChanged.current?.({...draft,text,caseText,researchRuns,sourceRevision,importText:JSON.stringify({...original,binding:next})})
  }else updateMapping({...mapping,sources:mapping.sources?.map(s=>s.name!==name||s.kind!=='research'?s:capability==='answer'?{webProfiles:{},...s,...pin}:{...s,webProfiles:{...s.webProfiles,[capability]:pin}})})
 }
 async function reviewResearch(name:string,choice:PortableResearchChoice,limits:ResearchLimits){
  if(operation.current||!mapping||editorInvalid||!caseValid||!researchContext)return
  const task=new AbortController();operation.current=task;invalidate();setProgress(msg('Preparing research review…'))
  try{
   await checkDialect(mapping,packText,task.signal)
   const trusted=await profiles.refetch();task.signal.throwIfAborted();if(trusted.error)throw trusted.error
   const prefix=await retainedPrefix(task.signal,name)
   try{
    await prepareMappedInputs({mapping,caseValue:parseMappedCase(caseText),...(draft?.importText?{imported:parseMappedObject(draft.importText) as unknown as NonNullable<SourceV2['import']>}:{}),files,selections,retainedResearch:Object.fromEntries(Object.entries(retainedResearch).filter(([key])=>key!==name)),retainedPrefix:prefix,reviewResearch:true,profiles:trusted.data??[],config,signal:task.signal,progress:source=>setProgress(msg('Reading {{source}}…',{source}))})
    throw Error(msg('This input already has retained research. Preview it before starting a new review.'))
   }catch(e){
    if(!(e instanceof PortableResearchRequired)||e.name!==name)throw e
    const run=await reviewPortableResearch(e.input,choice,limits,task.signal)
    task.signal.throwIfAborted()
    const nextRuns={...researchRuns,[name]:{id:run.id,context:researchContext}}
    const next:MappedDraft={text,caseText,...(draft?.importText?{importText:draft.importText}:{}),...(draft?.exchangeImportId?{exchangeImportId:draft.exchangeImportId}:{}),sourceRevision,researchRuns:nextRuns}
    if(!onPersistResearchDraft)throw Error(msg('Save this research review in a job draft before running it.'))
    await onPersistResearchDraft(next,task.signal)
    if(!task.signal.aborted)setResearchRuns(nextRuns)
   }
  }finally{if(operation.current===task){operation.current=null;setProgress('')}}
 }
 async function check() {
  if(operation.current || !mapping || editorInvalid || !caseValid) return
  const active=new AbortController();operation.current=active;invalidate();setProgress(msg('Checking inputs…'))
  try {
   await checkDialect(mapping,packText,active.signal)
   // Refresh installation pins before each explicit acquisition.
   const trusted=await profiles.refetch();active.signal.throwIfAborted();if(trusted.error)throw trusted.error
   const prefix=await retainedPrefix(active.signal)
   const result=await prepareMappedInputs({retainedPrefix:prefix,mapping,caseValue:parseMappedCase(caseText),...(draft?.importText?{imported:parseMappedObject(draft.importText) as unknown as NonNullable<SourceV2['import']>}:{}),files,selections,retainedResearch,profiles:trusted.data ?? [],config,signal:active.signal,progress:name=>setProgress(msg('Reading {{source}}…',{source:name}))})
   active.signal.throwIfAborted();setPreview(result)
   if(result.input.source && isSourceV2(result.input.source))onChange(result.input.source)
  } catch(e) {if(!active.signal.aborted){setError(mappingErrorMessage(e));if(e instanceof ConnectionRequestError && e.provider==='google-drive' && e.reconnectRequired)setReconnect(true)}}
  finally {if(operation.current===active){operation.current=null;setProgress('')}}
 }
 async function connectAgain() {
  if(operation.current || disabled)return
  const task=new AbortController();operation.current=task;setProgress(msg('Continue in the Google sign-in window.'));setError('')
  try {
   await authorizeDrive('connect',task.signal,'google-drive',connections.entries.find(e=>e.descriptor.id==='google-drive')?.descriptor.authorizationEndpoints)
   task.signal.throwIfAborted();setReconnect(false);setSelections({});setError(msg('This selection expired. Search again and reselect your sources.'))
  } catch(e) {if(!task.signal.aborted)setError(e instanceof Error?e.message:msg('The file could not be read.'))}
  finally {if(operation.current===task){operation.current=null;setProgress('')}}
 }
 const fileControls=mapping?.sources?.filter(s=>s.kind==='selected-file').map(s=><div key={s.name} className={styles.mappingRow}><div><strong>{s.name}</strong><p className={styles.note}>{files[s.name]?.original.name ?? (selections[s.name] ? msg('Selected') : msg('Not supplied'))}</p></div>{s.provider==='local-file' ? <><input hidden ref={el=>{uploads.current[s.name]=el}} type="file" accept=".json,application/json" aria-label={msg('Choose JSON file for {{source}}',{source:s.name})} disabled={busy} onChange={e=>{const file=e.target.files?.[0];e.target.value='';if(file)void select(s.name,file)}}/><Button disabled={busy} onClick={()=>uploads.current[s.name]?.click()}>{files[s.name] ? msg('Choose another file') : msg('Choose JSON file')}</Button></> : <Button disabled={busy} onClick={e=>{pickerOpener.current=e.currentTarget;setChoosing(s.name)}}>{msg('Choose from Google Drive')}</Button>}</div>)
 return <section className={styles.fields}>
  {choosing && active && !disabled && mapping?.sources?.some(s=>s.name===choosing&&s.kind==='selected-file'&&s.provider==='google-drive') && <DriveFilePicker openerRef={pickerOpener} onClose={()=>setChoosing(undefined)} onSelect={async(items,signal)=>{signal.throwIfAborted();if(items.length!==1)throw Error(msg('Choose one JSON file up to 200 KB.'));invalidate();setSelections(v=>({...v,[choosing]:{fileId:items[0]!.resourceId,grant:items[0]!.grant}}));setSourceRevision(crypto.randomUUID());onTransientChange?.()}}/>}

  {!fixed && !imported && <div className={styles.actions}><JobIntegrationPicker profiles={profiles.data??[]} disabled={busy||editorInvalid||!mapping||(mapping.sources??[]).length>=16} onPick={addSource}/></div>}
  {mapping && (fixed || imported ? <DialectReview details binding={binding} mapping={mapping} profiles={profiles.data?.map(p=>p.profile)}/> : <DialectEditor active={active} fileControls={source=>fileControls?.find(el=>el.key===source.name)} doc={doc} mapping={mapping} profiles={profiles.data??[]} disabled={busy} onChange={updateMapping} onInvalid={setEditorInvalid} onDirty={invalidate}/>)}
  {!fixed && !imported && <Disclosure title={msg('Advanced dialect')}>
  {!profiles.isPending && !profiles.data?.length && <p className={styles.note}>{msg('Connected source profiles must be configured by the installation owner. Case inputs and local files are available without them.')}</p>}
  <Disclosure title={msg('Edit dialect')}><div className={styles.field}><label htmlFor="job-mapping-json">{msg('Dialect JSON')}</label><TextArea id="job-mapping-json" rows={8} disabled={busy||editorInvalid} spellCheck={false} value={text} onChange={e=>{invalidate();setText(e.target.value);setFiles({});setSelections({})}}/><p className={styles.note}>{msg('Edit target paths, request templates and derivation rules here. The release freezes this dialect.')}</p></div></Disclosure></Disclosure>}
  <Disclosure title={msg('Dialects supplied with each run')}>
  {mapping&&<MappedCaseFields doc={doc} mapping={mapping} text={caseText} disabled={busy} onChange={value=>{invalidate();setCaseText(value)}} onValid={setCaseValid}/>}
  <Disclosure title={msg('Case inputs (JSON)')}><div className={styles.field}><label htmlFor="job-case-json">{msg('Case inputs (JSON)')}</label><TextArea id="job-case-json" rows={5} disabled={busy} spellCheck={false} value={caseText} onChange={e=>{invalidate();setCaseText(e.target.value)}}/></div></Disclosure>
  </Disclosure>
  {(fixed||imported)&&fileControls}
  {(mapping?.sources??[]).filter(s=>s.kind==='research').map(source=><section className={styles.fields} key={source.name}><PortableResearchControls source={source} allowBinding={!fixed} reference={researchRuns[source.name]} currentContext={researchContext} profiles={profiles.data??[]} binding={binding} disabled={busy||editorInvalid||!caseValid||!onPersistResearchDraft} onBind={(pin,capability)=>bindResearch(source.name,pin,capability)} onReview={(choice,limits)=>reviewResearch(source.name,choice,limits)}/><Disclosure title={msg('Use retained research file')}><div className={styles.field}><label htmlFor={'research-response-'+source.name}>{msg('Retained research responses')} · {source.name}</label><input id={'research-response-'+source.name} type="file" accept=".json,application/json" disabled={busy} onChange={e=>{const file=e.target.files?.[0];e.target.value='';if(file)void selectResearch(source.name,file)}}/><p className={styles.note}>{retainedResearch[source.name]?msg('Selected'):msg('Not supplied')}</p></div></Disclosure></section>)}

  {!mapping && <p role="alert" className={styles.problem}>{mappingErrorMessage(mappingError)}</p>}
  {(error || profiles.error) && <p role="alert" className={styles.problem}>{error || String(profiles.error)}</p>}
  {reconnect && <Button disabled={busy} onClick={()=>void connectAgain()}>{msg('Reconnect')}</Button>}
  {progress && <LoadingState>{progress}</LoadingState>}
  <div className={styles.actions}><Button disabled={busy || reconnect || editorInvalid || !caseValid || !mapping || profiles.isPending || Boolean(profiles.error)} onClick={()=>void check()}>{msg('Read sources and preview')}</Button>{progress && <Button variant="quiet" onClick={()=>{operation.current?.abort();operation.current=null;setProgress('');invalidate()}}>{msg('Cancel')}</Button>}</div>
  {preview && <><p role="status" className={styles.previewStatus}>{msg('Input preview complete. Review mapped values before continuing.')}</p><Disclosure title={msg('Mapped inputs')}><pre className={styles.json}>{preview.factsText}</pre><pre className={styles.json}>{preview.evidenceText || msg('Not supplied')}</pre></Disclosure>{preview.input.preparation && <InputLineage preparation={preview.input.preparation}/>}</>}
 </section>
}
