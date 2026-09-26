import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { msg } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { authorizeDrive, type DriveSelection } from '../connections/client'
import { factFields } from '../packs/test-workspace/model'
import type { PackDocument } from '../mcp/types'
import { Button } from '../ui/Button'
import { TextArea } from '../ui/TextArea'
import { Select } from '../ui/Select'
import { Disclosure } from '../ui/Disclosure'
import { jobsAPI, type SourceInput, type InputPreview } from './client'
import type { MappingV2, ProfileEntry, SourceV2 } from './mappingTypes'
import { isSourceV2 } from './mappingTypes'
import { parseMapping, parseMappedObject, prepareMappedInputs } from './mappedInputs'
import { localSnapshot, pointerEscape } from './sourceInputs'
import { MappingReview, InputLineage } from './MappingReview'
import styles from './JobsView.module.css'

function starter(doc: PackDocument): MappingV2 {
 return {version:2,case:{parameters:{},facts:factFields(doc).map(f => ({target:f.path,source:`/facts${f.path}`})),evidence:(doc.evidenceRequirements ?? []).map(e => ({requirement:e.id,source:`/evidence/${pointerEscape(e.id)}`}))},sources:[]}
}
export function MappedInputFields({doc, fixed, disabled, onChange}: {doc: PackDocument; fixed?: MappingV2; disabled: boolean; onChange: (source: SourceV2 | undefined) => void}) {
 const config = useEffectiveConfig().config.research
 const profiles = useQuery({queryKey:['job-input-profiles'],queryFn:({signal}) => jobsAPI<ProfileEntry[]>('input-profiles',undefined,undefined,signal)})
 const [text,setText] = useState(() => JSON.stringify(fixed ?? starter(doc),null,2)), [caseText,setCaseText] = useState('{"facts": {}, "evidence": {}}')
 const [files,setFiles] = useState<Record<string,SourceInput['snapshot']>>({}), [selections,setSelections] = useState<Record<string,DriveSelection>>({})
 const [profileId,setProfileId] = useState(''), [preview,setPreview] = useState<InputPreview>(), [error,setError] = useState(''), [progress,setProgress] = useState('')
 const operation = useRef<AbortController | null>(null), uploads = useRef<Record<string, HTMLInputElement | null>>({})
 const mapping = useMemo(() => {try {return parseMapping(text)} catch {return undefined}},[text])
 const context = JSON.stringify([config.gateway,config.documents,config.managedLocal,profiles.data,fixed])
 const busy = disabled || Boolean(progress)
 function invalidate() {setPreview(undefined); onChange(undefined); setError('')}
 useEffect(() => {setPreview(undefined); onChange(undefined); setFiles({}); setSelections({}); setProgress(''); setError(''); return () => {operation.current?.abort(); operation.current=null}},[context,onChange])
 useEffect(() => {if(fixed) setText(JSON.stringify(fixed,null,2))},[fixed])
 function updateMapping(next: MappingV2) {invalidate(); setText(JSON.stringify(next,null,2)); setFiles({}); setSelections({})}
 function addSource(profile?: ProfileEntry) {
  if (!mapping) return
  const next = structuredClone(mapping)
  next.sources ??= []
  const name = `source${next.sources.length+1}`
  const copy = {facts:next.case?.facts ?? [], evidence:next.case?.evidence ?? []}
  if(next.case) {next.case.facts=[];next.case.evidence=[]}
  if(!profile) next.sources.push({name,kind:'selected-file',provider:'local-file',read:{copy}})
  else if(profile.profile.source === 'drive') {
   const params = next.case?.parameters ?? {}
   params[`${name}File`] = {pointer:`/selections/${name}/fileId`,type:'string'}
   params[`${name}Grant`] = {pointer:`/selections/${name}/grant`,type:'string'}
   next.case = {...(next.case ?? {facts:[],evidence:[]}),parameters:params}
   next.sources.push({name,kind:'selected-file',provider:'google-drive',profile:profile.profile.id,profileDigest:profile.digest,maxAge:300,arguments:{fileId:{$param:`${name}File`},grant:{$param:`${name}Grant`}},read:{copy}})
  } else next.sources.push({name,kind:'operation',profile:profile.profile.id,profileDigest:profile.digest,maxAge:300,arguments:{tool:profile.profile.tools?.[0] ?? '',arguments:{}},read:{copy}})
  updateMapping(next)
 }
 async function select(name: string, file?: File) {
  if(operation.current || disabled) return
  const active = new AbortController();operation.current=active;invalidate();setProgress(msg('Reading files…'))
  try {
   if(file) {const snapshot=await localSnapshot(file);active.signal.throwIfAborted();setFiles(v=>({...v,[name]:snapshot}))}
   else {const selected=await authorizeDrive('pick',active.signal);active.signal.throwIfAborted();if(selected.length!==1)throw Error(msg('Choose one JSON file up to 200 KB.'));setSelections(v=>({...v,[name]:selected[0]!}))}
  } catch(e) {if(!active.signal.aborted)setError(e instanceof Error?e.message:msg('The file could not be read.'))}
  finally {if(operation.current===active){operation.current=null;setProgress('')}}
 }
 async function check() {
  if(operation.current || !mapping) return
  const active=new AbortController();operation.current=active;invalidate();setProgress(msg('Checking inputs…'))
  try {
   // Refresh installation pins before each explicit acquisition.
   const trusted=await profiles.refetch();active.signal.throwIfAborted();if(trusted.error)throw trusted.error
   const result=await prepareMappedInputs({mapping,caseValue:parseMappedObject(caseText),files,selections,profiles:trusted.data ?? [],config,signal:active.signal,progress:name=>setProgress(msg('Reading {{source}}…',{source:name}))})
   active.signal.throwIfAborted();setPreview(result)
   if(result.input.source && isSourceV2(result.input.source))onChange(result.input.source)
  } catch(e) {if(!active.signal.aborted)setError(e instanceof Error?e.message:msg('The inputs could not be mapped.'))}
  finally {if(operation.current===active){operation.current=null;setProgress('')}}
 }
 return <section className={styles.fields}>
  <p className={styles.note}>{msg('Review the source requests before reading. Copies and run records stay in local storage.')}</p>
  {mapping && <MappingReview details={Boolean(fixed)} mapping={mapping} profiles={profiles.data?.map(p=>p.profile)} />}
  {!fixed && <><div className={styles.actions}><Button disabled={busy || !mapping || (mapping.sources ?? []).length>=16} onClick={()=>addSource()}>{msg('Add local file')}</Button>{Boolean(profiles.data?.length) && <><div className={styles.profilePicker}><Select id="job-trusted-profile" aria-label={msg('Trusted source profile')} value={profileId} placeholder={msg('Trusted source profile')} disabled={busy} options={(profiles.data ?? []).filter(p=>p.profile.shape==='mcp'||p.profile.source==='drive').map(p=>({value:p.profile.id,label:`${p.profile.id} · ${p.profile.class}`}))} onValueChange={setProfileId}/></div><Button disabled={busy || !mapping || (mapping.sources ?? []).length>=16 || !profileId} onClick={()=>{const p=profiles.data?.find(p=>p.profile.id===profileId);if(p)addSource(p)}}>{msg('Add source')}</Button></>}</div>
  {!profiles.isPending && !profiles.data?.length && <p className={styles.note}>{msg('Connected source profiles must be configured by the installation owner. Case inputs and local files are available without them.')}</p>}
  <Disclosure title={msg('Edit mapping')}><div className={styles.field}><label htmlFor="job-mapping-json">{msg('Mapping JSON')}</label><TextArea id="job-mapping-json" rows={8} disabled={busy} spellCheck={false} value={text} onChange={e=>{invalidate();setText(e.target.value);setFiles({});setSelections({})}}/><p className={styles.note}>{msg('Edit target paths, request templates and derivation rules here. The release freezes this mapping.')}</p></div></Disclosure></>}
  <div className={styles.field}><label htmlFor="job-case-json">{msg('Case inputs (JSON)')}</label><TextArea id="job-case-json" rows={5} disabled={busy} spellCheck={false} value={caseText} onChange={e=>{invalidate();setCaseText(e.target.value)}}/></div>
  {mapping?.sources?.filter(s=>s.kind==='selected-file').map(s=><div key={s.name} className={styles.mappingRow}><div><strong>{s.name}</strong><p className={styles.note}>{files[s.name]?.original.name ?? (selections[s.name] ? msg('Selected') : msg('Not supplied'))}</p></div>{s.provider==='local-file' ? <><input hidden ref={el=>{uploads.current[s.name]=el}} type="file" accept=".json,application/json" aria-label={msg('Choose JSON file for {{source}}',{source:s.name})} disabled={busy} onChange={e=>{const file=e.target.files?.[0];e.target.value='';if(file)void select(s.name,file)}}/><Button disabled={busy} onClick={()=>uploads.current[s.name]?.click()}>{files[s.name] ? msg('Choose another file') : msg('Choose JSON file')}</Button></> : <Button disabled={busy} onClick={()=>void select(s.name)}>{msg('Choose from Google Drive')}</Button>}</div>)}
  {!mapping && <p role="alert" className={styles.problem}>{msg('The inputs could not be mapped.')}</p>}
  {(error || profiles.error) && <p role="alert" className={styles.problem}>{error || String(profiles.error)}</p>}
  {progress && <p role="status" className={styles.note}>{progress}</p>}
  <div className={styles.actions}><Button disabled={busy || !mapping || profiles.isPending || Boolean(profiles.error)} onClick={()=>void check()}>{msg('Read sources and preview')}</Button>{progress && <Button variant="quiet" onClick={()=>{operation.current?.abort();operation.current=null;setProgress('');invalidate()}}>{msg('Cancel')}</Button>}</div>
  {preview && <><Disclosure title={msg('Mapped inputs')}><pre className={styles.json}>{preview.factsText}</pre><pre className={styles.json}>{preview.evidenceText || msg('Not supplied')}</pre></Disclosure>{preview.input.preparation && <InputLineage preparation={preview.input.preparation}/>}</>}
 </section>
}
