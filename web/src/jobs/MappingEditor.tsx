import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { msg, useLocale } from '../i18n'
import type { PackDocument } from '../mcp/types'
import { humanId } from '../packs/logicModel'
import { factFields } from '../packs/test-workspace/model'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { Disclosure } from '../ui/Disclosure'
import { TextArea } from '../ui/TextArea'
import { useDetailsPortal, useDetailsSlot } from '../shell/DetailsSlot'
import { IconChevronRight, IconDetails, IconFolder, IconGear } from '../shell/icons'
import type { MappingSource, MappingV2, ProfileEntry } from './mappingTypes'
import { parseMappedObject } from './mappedInputs'
import { setBinding, getBinding } from './mappingEditorModel'
import { JobIntegrationPicker } from './JobIntegrationPicker'
import { canChangeIntegration, setSourceIntegration } from './jobIntegrations'
import styles from './JobsView.module.css'

type Assignment={kind:'facts'|'evidence';target:string;label:string;description?:string}
type EditorProps={doc:PackDocument;mapping:MappingV2;profiles:ProfileEntry[];disabled:boolean;active?:boolean;fileControls?:(source:MappingSource)=>ReactNode;onChange:(m:MappingV2)=>void;onInvalid:(invalid:boolean)=>void;onDirty:()=>void}
export function MappingEditor({doc,mapping,profiles,disabled,active=true,fileControls,onChange,onInvalid,onDirty}:EditorProps){
 useLocale()
 const [selected,setSelected]=useState(''),[assignment,setAssignment]=useState<Assignment>(),[pending,setPending]=useState(false),details=useDetailsSlot()
 const source=mapping.sources?.find(s=>s.name===selected)
 const profile=profiles.find(p=>p.profile.id===source?.profile)?.profile
 const [integrationTarget,setIntegrationTarget]=useState<HTMLDivElement|null>(null)
 const integration=<div ref={setIntegrationTarget}/>
 const invalid=useCallback((value:boolean)=>{setPending(value);onInvalid(value)},[onInvalid])
 const previous=useRef((mapping.sources??[]).map(s=>s.name))
 useEffect(()=>{const names=(mapping.sources??[]).map(s=>s.name);const added=names.find(name=>!previous.current.includes(name));previous.current=names;if(added&&active){setSelected(added);setAssignment(undefined);details.reveal()}},[mapping.sources,active,details.reveal])
 function choose(name:string){setSelected(name);setAssignment(undefined);details.reveal()}
 const fields=factFields(doc)
 const all:Assignment[]=[...fields.map(f=>({kind:'facts' as const,target:f.path,label:f.label})),...(doc.evidenceRequirements??[]).map(e=>({kind:'evidence' as const,target:e.id,label:humanId(e.id,e.id),description:e.description}))]
 const assigned=all.filter(a=>getBinding(mapping,a.kind,a.target).owner!=='omitted').length
 function row(a:Assignment){
  const binding=getBinding(mapping,a.kind,a.target)
  const label=binding.complex?msg('Advanced mapping'):binding.owner==='case'?msg('Case inputs'):binding.owner==='omitted'?msg('Not supplied'):binding.owner
  return <button type="button" className={styles.assignmentRow} key={a.target} disabled={disabled||pending} onClick={()=>{setAssignment(a);details.reveal()}} aria-label={msg('Configure {{name}}',{name:a.label})} aria-current={assignment?.target===a.target&&assignment.kind===a.kind&&details.open?'true':undefined}><span>{a.label}</span><span className={styles.assignmentOrigin}>{label}</span><IconChevronRight/></button>
 }
 return <div className={styles.fields}>
  <div className={styles.sourceList} aria-label={msg('Input sources')}>
   <div className={styles.sourceSummary}><IconDetails/><span><strong>{msg('Case inputs')}</strong><small>{msg('Values supplied with each run')}</small></span></div>
   {(mapping.sources??[]).map(s=><button type="button" key={s.name} className={styles.sourceSummary} disabled={disabled||pending} onClick={()=>choose(s.name)} aria-label={s.name} aria-current={s.name===selected&&!assignment&&details.open?'true':undefined}>{s.provider==='local-file'?<IconFolder/>:<IconGear/>}<span><strong>{s.name}</strong><small>{s.provider==='local-file'?msg('Local JSON file'):s.provider==='google-drive'?msg('Google Drive'):s.profile} · {s.kind==='operation'?msg('Read operation'):msg('Selected file')}</small></span><IconChevronRight/></button>)}
  </div>
  {source&&!assignment&&<JobIntegrationPicker triggerTarget={integrationTarget} current={source} profiles={profiles} disabled={disabled||pending||!canChangeIntegration(mapping,source.name)} onPick={entry=>{onDirty();onChange(setSourceIntegration(mapping,entry,source.name))}}/>}
  {source&&<SourceEditor active={active&&!assignment} fileControls={fileControls?.(source)} integration={integration} key={`${source.name}:${source.profile??source.provider}:${source.profileDigest??''}`} source={source} tools={profile?.tools??[]} sourceClass={profile?.class??'asserted'} disabled={disabled} onInvalid={invalid} onDirty={onDirty} onApply={value=>onChange({...mapping,sources:mapping.sources?.map(s=>s.name===source.name?value:s)})}/>}
  {assignment&&<AssignmentEditor key={`${assignment.kind}:${assignment.target}`} {...{assignment,mapping,profiles,disabled,active,onChange}} onSource={choose}/>}
  <div className={styles.readinessHeading}><h3>{msg('Facts and evidence')}</h3><span className={styles.note}>{msg('{{assigned}} of {{total}} assigned',{assigned,total:all.length})}</span></div>
  <p className={styles.note}>{msg('Choose an assignment to configure its source and method. Unmapped values remain unknown.')}</p>
  {fields.length>0&&<section className={styles.assignmentGroup}><h4>{msg('Facts')} · {fields.length}</h4>{all.filter(a=>a.kind==='facts').map(row)}</section>}
  {Boolean(doc.evidenceRequirements?.length)&&<section className={styles.assignmentGroup}><h4>{msg('Evidence availability')} · {doc.evidenceRequirements!.length}</h4>{all.filter(a=>a.kind==='evidence').map(row)}</section>}
 </div>
}
function AssignmentEditor({assignment:a,mapping,profiles,disabled,active,onChange,onSource}:{assignment:Assignment;mapping:MappingV2;profiles:ProfileEntry[];disabled:boolean;active:boolean;onChange:(m:MappingV2)=>void;onSource:(name:string)=>void}){
 const id=useId(),binding=getBinding(mapping,a.kind,a.target)
 const source=mapping.sources?.find(s=>s.name===binding.owner),sourceClass=profiles.find(p=>p.profile.id===source?.profile)?.profile.class??'asserted'
 const mutate=(owner:string,path:string)=>onChange(setBinding(mapping,a.kind,a.target,owner,path))
 const opts=[{value:'omitted',label:msg('Not supplied')},{value:'case',label:msg('Case inputs')},...(mapping.sources??[]).map(s=>({value:s.name,label:s.name})),...(binding.complex?[{value:'advanced',label:msg('Advanced mapping')}]:[])]
 const content=<div className={styles.sourceEditor}><h2>{a.label}</h2>{a.description&&<p>{a.description}</p>}<p className={styles.note}>{a.kind==='facts'?msg('Map a value from a source into this fact.'):msg('Evidence paths must contain present, absent or unknown. A file connection does not verify evidence.')}</p>
  <div className={styles.field}><label htmlFor={`${id}-source`}>{msg('Source')}</label><Select id={`${id}-source`} disabled={disabled||binding.complex} value={binding.complex?'advanced':binding.owner} options={opts} onValueChange={owner=>mutate(owner,owner==='case'?(a.kind==='facts'?`/facts${a.target}`:`/evidence/${a.target.replace(/~/g,'~0').replace(/\//g,'~1')}`):binding.path)}/></div>
  {binding.complex?<p className={styles.note}>{msg('This assignment uses a derivation rule. Review it in Edit mapping.')}</p>:binding.owner==='omitted'?<p className={styles.note}>{msg('Unknown')}</p>:<div className={styles.field}><label htmlFor={`${id}-path`}>{msg('Source path')}</label><Input id={`${id}-path`} disabled={disabled} placeholder={msg('Source path, e.g. /request/type')} value={binding.path} onChange={e=>mutate(binding.owner,e.target.value)}/><p className={styles.note}>{msg('JSON pointer into the selected source response.')}</p></div>}
  {source&&<Button variant="quiet" onClick={()=>onSource(source.name)}>{msg('Configure source')}</Button>}
  {sourceClass==='generated'&&!binding.complex&&<label className={styles.admission}><input type="checkbox" disabled={disabled} checked={(mapping.admits?.[a.kind]?.[a.target]??[]).includes('generated')} onChange={e=>{const next=structuredClone(mapping);next.admits??={};next.admits[a.kind]??={};const classes=next.admits[a.kind]![a.target]??['asserted','record'];next.admits[a.kind]![a.target]=e.target.checked?[...new Set([...classes,'generated'])]:classes.filter(c=>c!=='generated');onChange(next)}}/>{msg('Allow generated values')}</label>}
  <Disclosure title={msg('Technical details')}><dl className={styles.properties}><div><dt>{msg('Target')}</dt><dd><code>{a.target}</code></dd></div><div><dt>{msg('Input class')}</dt><dd>{binding.complex?msg('Advanced mapping'):sourceClass}</dd></div></dl></Disclosure>
 </div>
 const portal=useDetailsPortal(active?content:null)
 return active?portal??content:null
}
function SourceEditor({active=true,fileControls,integration,source,tools,sourceClass,disabled,onApply,onInvalid,onDirty}:{active?:boolean;fileControls?:import('react').ReactNode;integration?:import('react').ReactNode;source:MappingSource;tools:string[];sourceClass:string;disabled:boolean;onApply:(s:MappingSource)=>void;onInvalid:(v:boolean)=>void;onDirty:()=>void}){
 const id=useId(),[args,setArgs]=useState(JSON.stringify(source.arguments?.arguments??{},null,2)),[age,setAge]=useState(String(source.maxAge??300)),[tool,setTool]=useState(String(source.arguments?.tool??'')),[error,setError]=useState('')
 const dirty=args!==JSON.stringify(source.arguments?.arguments??{},null,2)||age!==String(source.maxAge??300)||tool!==String(source.arguments?.tool??'')
 const request=JSON.stringify([source.arguments,source.maxAge])
 useEffect(()=>{setArgs(JSON.stringify(source.arguments?.arguments??{},null,2));setAge(String(source.maxAge??300));setTool(String(source.arguments?.tool??''));setError('')},[request])
 useEffect(()=>{onInvalid(dirty);return()=>onInvalid(false)},[dirty,onInvalid])
 function edit(){onDirty();setError('')}
 function apply(){try{const value=Number(age);if(!Number.isInteger(value)||value<1||value>86400)throw Error(msg('Enter a whole number between 1 and 86400.'));const argumentsValue=parseMappedObject(args);onApply({...source,...(source.profile?{maxAge:value}:{}),...(source.kind==='operation'?{arguments:{...source.arguments,tool,arguments:argumentsValue}}:{})});setError('')}catch(e){setError((e as Error).message)}}
 const content=<div className={styles.sourceEditor}>
  <section className={styles.fields}><h2>{source.name}</h2>{integration}</section>
  {fileControls}
  {source.kind==='operation'&&<section className={styles.fields}><div className={styles.field}><label htmlFor={`${id}-tool`}>{msg('Read operation')}</label><Select id={`${id}-tool`} value={tool} options={tools.map(t=>({value:t,label:t}))} disabled={disabled} onValueChange={value=>{setTool(value);edit()}}/></div><div className={styles.field}><label htmlFor={`${id}-args`}>{msg('Request parameters (JSON)')}</label><TextArea id={`${id}-args`} rows={6} value={args} disabled={disabled} onChange={e=>{setArgs(e.target.value);edit()}}/><p className={styles.note}>{msg('The release freezes this mapping.')}</p></div></section>}
  {source.profile&&<section className={styles.fields}><div className={styles.field}><label htmlFor={`${id}-age`}>{msg('Maximum source age (seconds)')}</label><Input id={`${id}-age`} type="number" min={1} max={86400} value={age} disabled={disabled} onChange={e=>{setAge(e.target.value);edit()}}/></div></section>}
  {dirty&&<Button disabled={disabled} onClick={apply}>{msg('Apply configuration')}</Button>}
  {sourceClass==='generated'&&<p className={styles.note}>{msg('Generated values need explicit permission for each mapped target.')}</p>}
  {error&&<p role="alert" className={styles.problem}>{error}</p>}
  <Disclosure title={msg('Technical details')}><p className={styles.note}>{msg('Input class')}: {sourceClass}</p><pre className={styles.json}>{JSON.stringify(source,null,2)}</pre></Disclosure>
 </div>
 const portal=useDetailsPortal(active ? content : null)
 return active ? portal ?? content : null
}
