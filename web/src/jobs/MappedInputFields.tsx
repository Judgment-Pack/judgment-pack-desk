import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { msg } from '../i18n'
import type { MappedDraft } from './drafts'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { authorizeDrive, type DriveSelection } from '../connections/client'
import { factFields } from '../packs/test-workspace/model'
import type { PackDocument } from '../mcp/types'
import { Button } from '../ui/Button'
import { TextArea } from '../ui/TextArea'
import { useConnections } from '../connections/catalog'
import { JobIntegrationPicker } from './JobIntegrationPicker'
import { setSourceIntegration } from './jobIntegrations'
import { Disclosure } from '../ui/Disclosure'
import { jobsAPI, type SourceInput, type InputPreview } from './client'
import type { MappingV2, ProfileEntry, SourceV2 } from './mappingTypes'
import { isSourceV2 } from './mappingTypes'
import { parseMapping, parseMappedObject, prepareMappedInputs } from './mappedInputs'
import { localSnapshot, pointerEscape } from './sourceInputs'
import { MappingEditor } from './MappingEditor'
import { MappedCaseFields } from './MappedCaseFields'
import { MappingReview, InputLineage } from './MappingReview'
import styles from './JobsView.module.css'

function starter(doc: PackDocument): MappingV2 {
 return {version:2,case:{parameters:{},facts:factFields(doc).map(f => ({target:f.path,source:`/facts${f.path}`})),evidence:(doc.evidenceRequirements ?? []).map(e => ({requirement:e.id,source:`/evidence/${pointerEscape(e.id)}`}))},sources:[]}
}
export function MappedInputFields({doc, fixed, disabled, active=true, onChange, draft, onDraftChange, onWorkChange, onPendingChange, onTransientChange}: {doc: PackDocument; fixed?: MappingV2; disabled: boolean; active?: boolean; onChange: (source: SourceV2 | undefined) => void; draft?: MappedDraft; onDraftChange?: (draft: MappedDraft) => void; onWorkChange?: (dirty: boolean) => void; onPendingChange?: (pending: boolean) => void; onTransientChange?: () => void}) {
 const effective=useEffectiveConfig(),config=effective.config.research
 const localConnections=effective.desk?.localGateway?.status==='ready'&&!effective.desk?.decoded?.values?.research?.gateway
 const connections=useConnections(localConnections)
 const profiles = useQuery({queryKey:['job-input-profiles'],queryFn:({signal}) => jobsAPI<ProfileEntry[]>('input-profiles',undefined,undefined,signal)})
 const [text,setText] = useState(() => draft?.text ?? JSON.stringify(fixed ?? starter(doc),null,2)), [caseText,setCaseText] = useState(draft?.caseText ?? '{"facts": {}, "evidence": {}}')
 const [files,setFiles] = useState<Record<string,SourceInput['snapshot']>>({}), [selections,setSelections] = useState<Record<string,DriveSelection>>({})
 const [preview,setPreview] = useState<InputPreview>(), [error,setError] = useState(''), [progress,setProgress] = useState('')
 const [editorInvalid,setEditorInvalid]=useState(false),[caseValid,setCaseValid]=useState(true)
 const operation = useRef<AbortController | null>(null), uploads = useRef<Record<string, HTMLInputElement | null>>({})
 const initial = useRef({text,caseText})
 const draftChanged = useRef(onDraftChange); draftChanged.current = onDraftChange
 useEffect(() => { if (text !== initial.current.text || caseText !== initial.current.caseText || draft) draftChanged.current?.({text,caseText}) }, [text,caseText])
 useEffect(() => { onWorkChange?.(text !== initial.current.text || caseText !== initial.current.caseText || editorInvalid || Object.keys(files).length > 0 || Object.keys(selections).length > 0) }, [text,caseText,editorInvalid,files,selections,onWorkChange])
 useEffect(() => { onPendingChange?.(editorInvalid || !caseValid) }, [editorInvalid,caseValid,onPendingChange])
 const mapping = useMemo(() => {try {return parseMapping(text)} catch {return undefined}},[text])
 const context = JSON.stringify([config.gateway,config.documents,config.managedLocal,profiles.data?.filter(p=>mapping?.sources?.some(s=>s.profile===p.profile.id)),fixed,connections.entries.filter(e=>mapping?.sources?.some(s=>s.provider==='google-drive'&&e.descriptor.id==='google-drive'||profiles.data?.some(p=>p.profile.id===s.profile&&p.profile.source===(e.descriptor.source?.id??e.descriptor.id)))).map(e=>[e.descriptor.id,e.status.data?.state,e.status.data?.account?.id,e.status.data?.resource?.id]),connections.isError])
 const busy = disabled || Boolean(progress)
 function invalidate() {setPreview(undefined); onChange(undefined); setError('')}
 useEffect(() => {setPreview(undefined); onChange(undefined); setSelections({}); setProgress(''); setError(''); return () => {operation.current?.abort(); operation.current=null}},[context,onChange])
 useEffect(() => {if(fixed) {setText(JSON.stringify(fixed,null,2));setFiles({});setSelections({})}},[fixed])
 function updateMapping(next: MappingV2) {
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
 async function select(name: string, file?: File) {
  if(operation.current || disabled) return
  const active = new AbortController();operation.current=active;invalidate();setProgress(msg('Reading files…'))
  try {
   if(file) {const snapshot=await localSnapshot(file);active.signal.throwIfAborted();setFiles(v=>({...v,[name]:snapshot}));onTransientChange?.()}
   else {const selected=await authorizeDrive('pick',active.signal);active.signal.throwIfAborted();if(selected.length!==1)throw Error(msg('Choose one JSON file up to 200 KB.'));setSelections(v=>({...v,[name]:selected[0]!}));onTransientChange?.()}
  } catch(e) {if(!active.signal.aborted)setError(e instanceof Error?e.message:msg('The file could not be read.'))}
  finally {if(operation.current===active){operation.current=null;setProgress('')}}
 }
 async function check() {
  if(operation.current || !mapping || editorInvalid || !caseValid) return
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
 const fileControls=mapping?.sources?.filter(s=>s.kind==='selected-file').map(s=><div key={s.name} className={styles.mappingRow}><div><strong>{s.name}</strong><p className={styles.note}>{files[s.name]?.original.name ?? (selections[s.name] ? msg('Selected') : msg('Not supplied'))}</p></div>{s.provider==='local-file' ? <><input hidden ref={el=>{uploads.current[s.name]=el}} type="file" accept=".json,application/json" aria-label={msg('Choose JSON file for {{source}}',{source:s.name})} disabled={busy} onChange={e=>{const file=e.target.files?.[0];e.target.value='';if(file)void select(s.name,file)}}/><Button disabled={busy} onClick={()=>uploads.current[s.name]?.click()}>{files[s.name] ? msg('Choose another file') : msg('Choose JSON file')}</Button></> : <Button disabled={busy} onClick={()=>void select(s.name)}>{msg('Choose from Google Drive')}</Button>}</div>)
 return <section className={styles.fields}>

  {!fixed && <div className={styles.actions}><JobIntegrationPicker profiles={profiles.data??[]} disabled={busy||editorInvalid||!mapping||(mapping.sources??[]).length>=16} onPick={addSource}/></div>}
  {mapping && (fixed ? <MappingReview details mapping={mapping} profiles={profiles.data?.map(p=>p.profile)}/> : <MappingEditor active={active} fileControls={source=>fileControls?.find(el=>el.key===source.name)} doc={doc} mapping={mapping} profiles={profiles.data??[]} disabled={busy} onChange={updateMapping} onInvalid={setEditorInvalid} onDirty={invalidate}/>)}
  {!fixed && <Disclosure title={msg('Advanced mapping')}>
  {!profiles.isPending && !profiles.data?.length && <p className={styles.note}>{msg('Connected source profiles must be configured by the installation owner. Case inputs and local files are available without them.')}</p>}
  <Disclosure title={msg('Edit mapping')}><div className={styles.field}><label htmlFor="job-mapping-json">{msg('Mapping JSON')}</label><TextArea id="job-mapping-json" rows={8} disabled={busy||editorInvalid} spellCheck={false} value={text} onChange={e=>{invalidate();setText(e.target.value);setFiles({});setSelections({})}}/><p className={styles.note}>{msg('Edit target paths, request templates and derivation rules here. The release freezes this mapping.')}</p></div></Disclosure></Disclosure>}
  <Disclosure title={msg('Inputs supplied with each run')}>
  {mapping&&<MappedCaseFields doc={doc} mapping={mapping} text={caseText} disabled={busy} onChange={value=>{invalidate();setCaseText(value)}} onValid={setCaseValid}/>}
  <Disclosure title={msg('Case inputs (JSON)')}><div className={styles.field}><label htmlFor="job-case-json">{msg('Case inputs (JSON)')}</label><TextArea id="job-case-json" rows={5} disabled={busy} spellCheck={false} value={caseText} onChange={e=>{invalidate();setCaseText(e.target.value)}}/></div></Disclosure>
  </Disclosure>
  {fixed&&fileControls}

  {!mapping && <p role="alert" className={styles.problem}>{msg('The inputs could not be mapped.')}</p>}
  {(error || profiles.error) && <p role="alert" className={styles.problem}>{error || String(profiles.error)}</p>}
  {progress && <p role="status" className={styles.note}>{progress}</p>}
  <div className={styles.actions}><Button disabled={busy || editorInvalid || !caseValid || !mapping || profiles.isPending || Boolean(profiles.error)} onClick={()=>void check()}>{msg('Read sources and preview')}</Button>{progress && <Button variant="quiet" onClick={()=>{operation.current?.abort();operation.current=null;setProgress('');invalidate()}}>{msg('Cancel')}</Button>}</div>
  {preview && <><p role="status" className={styles.previewStatus}>{msg('Input preview complete. Review mapped values before continuing.')}</p><Disclosure title={msg('Mapped inputs')}><pre className={styles.json}>{preview.factsText}</pre><pre className={styles.json}>{preview.evidenceText || msg('Not supplied')}</pre></Disclosure>{preview.input.preparation && <InputLineage preparation={preview.input.preparation}/>}</>}
 </section>
}
