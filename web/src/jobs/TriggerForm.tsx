import { useDirtyGuard } from '../shell/useDirtyGuard'
import {scheduleInstant,scheduleWallTime} from './scheduleTime'
import {useEffect,useId,useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {msg} from '../i18n'
import type {PackDocument} from '../mcp/types'
import {listFiles} from '../files/client'
import {Input} from '../ui/Input'
import {Select} from '../ui/Select'
import {TextArea} from '../ui/TextArea'
import {Disclosure} from '../ui/Disclosure'
import {Button} from '../ui/Button'
import {InputFields} from './InputFields'
import {MappedCaseFields} from './MappedCaseFields'
import {editableJSON,parseInput} from './inputModel'
import {jobsRequest} from './wire'
import {jobsAPI,type InputMapping,type JobInput} from './client'
import type {MappingV2} from './mappingTypes'
import type {AutomaticInput,Schedule,TriggerConfig} from './triggerTypes'
import styles from './JobsView.module.css'

type Mapping=InputMapping|MappingV2
export function triggerName(kind:string){return kind==='cloud'?msg('Google Cloud Scheduler'):kind==='schedule'?msg('Schedule'):kind==='file'?msg('File changes'):msg('Authenticated event')}
export function defaultTrigger(kind:TriggerConfig['kind'],mapping?:Mapping):TriggerConfig{
 const config:TriggerConfig={name:triggerName(kind),kind,missed:'skip',overlap:'skip',queueSeconds:3600}
 if(kind==='cloud')config.cloud={connection:'',subscription:'',job:''}
 if(kind==='schedule')config.schedule={kind:'interval',everySeconds:3600,timezone:Intl.DateTimeFormat().resolvedOptions().timeZone||'UTC',startAt:new Date().toISOString()}
 if(kind!=='event')config.input=defaultAutomatic(mapping,kind==='file')
 if(kind==='file'){config.stableSeconds=5;config.watchPath='';if(!mapping)config.input={kind:'input-file',path:''}}
 return config
}
function defaultAutomatic(mapping?:Mapping,fileOnly=false):AutomaticInput {
 return mapping?{kind:!fileOnly&&mapping.version===2&&mapping.sources?.some(s=>s.kind==='operation')?'mapped-sources':'mapped-files',...(mapping.version===2?{case:{}}:{}),files:Object.fromEntries((mapping.version===1?['file']:mapping.sources?.filter(s=>s.provider==='local-file').map(s=>s.name)??[]).map(name=>[name,'']))}:{kind:'input-file',path:''}
}
export function TriggerChoice({doc,mapping,value,onChange,onValid,disabled}:{doc:PackDocument;mapping?:Mapping;value?:TriggerConfig;onChange:(value:TriggerConfig|undefined)=>void;onValid:(v:boolean)=>void;disabled:boolean}){
 const id=useId()
 return <div className={styles.stack}><div className={styles.field}><label htmlFor={id}>{msg('Trigger')}</label><Select id={id} value={value?.kind??'manual'} disabled={disabled} options={[{value:'manual',label:msg('Manual / API')},{value:'schedule',label:msg('Schedule')},{value:'event',label:msg('Authenticated event')},{value:'file',label:msg('File changes')},{value:'cloud',label:msg('Google Cloud Scheduler')}]} onValueChange={v=>{onChange(v==='manual'?undefined:defaultTrigger(v as TriggerConfig['kind'],mapping));onValid(true)}}/></div>{value?<TriggerForm key={value.kind} {...{doc,mapping,value,onChange,onValid,disabled}}/>:<p className={styles.note}>{msg('Start each run from Desk or submit it through the authenticated API.')}</p>}</div>
}
export function TriggerForm({doc,mapping,value,onChange,onValid,onUnwritten,disabled}:{doc:PackDocument;mapping?:Mapping;value:TriggerConfig;onChange:(value:TriggerConfig)=>void;onValid:(v:boolean)=>void;onUnwritten?:(value:boolean)=>void;disabled:boolean}){
 const id=useId(),[inputsValid,setInputsValid]=useState(true),[startValid,setStartValid]=useState(true),[endValid,setEndValid]=useState(true)
 const connections=useQuery({queryKey:['job-background-connections'],queryFn:()=>jobsAPI<{cloudConnections:{id:string;subscription:string;problem?:string}[];gatewayProfiles:string[]}>('background-connections'),enabled:value.kind==='cloud'||mapping?.version===2&&Boolean(mapping.sources?.some(s=>s.kind==='operation'))})
 const supported=!mapping||(mapping.version===1?mapping.provider==='local-file':!(mapping.sources??[]).some(s=>!(s.kind==='selected-file'&&s.provider==='local-file'||value.kind!=='file'&&s.kind==='operation'&&connections.data?.gatewayProfiles.includes(s.profile??''))))
 const incomplete=!startValid||!endValid||value.input?.kind==='constant'&&!inputsValid
 useEffect(()=>{onUnwritten?.(incomplete);return()=>onUnwritten?.(false)},[incomplete,onUnwritten])
 const schedule=value.schedule
 const scheduleValid=!schedule||Boolean(startValid&&endValid&&schedule.timezone&&Number.isFinite(Date.parse(schedule.startAt))&&(!schedule.endAt||Date.parse(schedule.endAt)>Date.parse(schedule.startAt))&&(schedule.kind!=='interval'||Number.isInteger(schedule.everySeconds)&&schedule.everySeconds!>=60&&schedule.everySeconds!<=30*86400)&&(schedule.kind!=='daily'&&schedule.kind!=='weekly'||/^([01]\d|2[0-3]):[0-5]\d$/.test(schedule.time??'')))
 const valid=Boolean(value.name.trim()&&(value.kind!=='cloud'||Boolean(value.cloud?.job.match(/^projects\/[a-z][a-z0-9-]{4,61}[a-z0-9]\/locations\/[a-z0-9-]+\/jobs\/[A-Za-z0-9_-]{1,500}$/)&&connections.data?.cloudConnections.some(c=>c.id===value.cloud?.connection&&c.subscription===value.cloud.subscription)))&&scheduleValid&&Number.isInteger(value.queueSeconds)&&value.queueSeconds>=60&&value.queueSeconds<=86400&&(value.kind==='event'||supported&&inputsValid)&&(value.kind!=='file'||value.watchPath&&[value.input?.path,...Object.values(value.input?.files??{})].includes(value.watchPath)&&Number.isInteger(value.stableSeconds)&&value.stableSeconds!>=1&&value.stableSeconds!<=3600))
 useEffect(()=>onValid(valid),[valid,onValid])
 const change=(patch:Partial<TriggerConfig>)=>onChange({...value,...patch})
 const scheduleChange=(patch:Partial<Schedule>)=>change({schedule:{...schedule!,...patch}})
 return <fieldset className={styles.fields} disabled={disabled}>
  <div className={styles.field}><label htmlFor={`${id}-name`}>{msg('Trigger name')}</label><Input id={`${id}-name`} maxLength={160} value={value.name} onChange={e=>change({name:e.target.value})}/></div>
  {value.kind==='cloud'&&<section className={styles.fields}><h3>{msg('Cloud delivery')}</h3><p className={styles.note}>{msg('Google schedules the occurrence. This computer pulls the signal and runs the job locally. Inputs and results stay here.')}</p>{connections.error&&<p role="alert" className={styles.problem}>{msg('Cloud connections could not be loaded.')}</p>}{connections.isPending?<p role="status">{msg('Loading…')}</p>:!connections.data?.cloudConnections.length?<p className={styles.note}>{msg('No cloud connection is installed. The installation owner must configure a dedicated Pub/Sub subscription and local Google credentials.')}</p>:<><div className={styles.field}><label htmlFor={`${id}-cloud`}>{msg('Cloud connection')}</label><Select id={`${id}-cloud`} value={value.cloud?.connection??''} placeholder={msg('Choose a connection')} options={connections.data.cloudConnections.map(c=>({value:c.id,label:c.id}))} onValueChange={connection=>{const c=connections.data.cloudConnections.find(c=>c.id===connection)!;change({cloud:{connection,subscription:c.subscription,job:value.cloud?.job??''}})}}/></div><p className={styles.note}>{value.cloud?.subscription}</p>{connections.data.cloudConnections.find(c=>c.id===value.cloud?.connection)?.problem&&<p role="alert" className={styles.problem}>{connections.data.cloudConnections.find(c=>c.id===value.cloud?.connection)?.problem}</p>}<div className={styles.field}><label htmlFor={`${id}-job`}>{msg('Scheduler job name')}</label><Input id={`${id}-job`} value={value.cloud?.job??''} placeholder={msg("projects/my-project/locations/us-central1/jobs/job-name")} onChange={e=>change({cloud:{connection:value.cloud?.connection??'',subscription:value.cloud?.subscription??'',job:e.target.value}})}/></div><p className={styles.note}>{msg('Timing and time zone are managed in Google Cloud. Choose the job deployed with the authenticated relay.')}</p></>}</section>}
  {value.kind==='event'?<p className={styles.note}>{msg('Each authenticated delivery supplies its own event ID, timestamp and inputs. The event endpoint and a scoped token become available when enabled.')}</p>:!supported?<p role="alert" className={styles.problem}>{msg('This mapping needs a standing Gateway connection before it can run unattended. Manual runs and authenticated events remain available.')}</p>:<AutomaticInputs key={value.input?.kind} doc={doc} mapping={mapping} value={value.input!} onChange={input=>change({input})} onValid={setInputsValid} disabled={disabled} fileOnly={value.kind==='file'}/>}
  {schedule&&<section className={styles.fields}><h3>{msg('Schedule')}</h3><div className={styles.formGrid}>
   <div className={styles.field}><label htmlFor={`${id}-repeat`}>{msg('Repeat')}</label><Select id={`${id}-repeat`} value={schedule.kind} disabled={disabled} options={[{value:'interval',label:msg('Interval')},{value:'daily',label:msg('Daily')},{value:'weekly',label:msg('Weekly')},{value:'once',label:msg('One time')}]} onValueChange={kind=>change({schedule:{kind:kind as Schedule['kind'],timezone:schedule.timezone,startAt:schedule.startAt,...(kind==='interval'?{everySeconds:3600}:kind==='once'?{}:{time:'09:00',...(kind==='weekly'?{weekday:1}:{})})}})}/></div>
   <div className={styles.field}><label htmlFor={`${id}-zone`}>{msg('Time zone')}</label><Input id={`${id}-zone`} value={schedule.timezone} onChange={e=>scheduleChange({timezone:e.target.value})}/></div>
   {schedule.kind==='interval'&&<div className={styles.field}><label htmlFor={`${id}-interval`}>{msg('Every (minutes)')}</label><Input id={`${id}-interval`} type="number" min={1} max={43200} value={(schedule.everySeconds??60)/60} onChange={e=>scheduleChange({everySeconds:Number(e.target.value)*60})}/></div>}
   {(schedule.kind==='daily'||schedule.kind==='weekly')&&<div className={styles.field}><label htmlFor={`${id}-time`}>{msg('Local time')}</label><Input id={`${id}-time`} type="time" value={schedule.time} onChange={e=>scheduleChange({time:e.target.value})}/></div>}
   {schedule.kind==='weekly'&&<div className={styles.field}><label htmlFor={`${id}-day`}>{msg('Weekday')}</label><Select id={`${id}-day`} value={String(schedule.weekday??1)} disabled={disabled} options={[msg('Sunday'),msg('Monday'),msg('Tuesday'),msg('Wednesday'),msg('Thursday'),msg('Friday'),msg('Saturday')].map((label,i)=>({value:String(i),label}))} onValueChange={v=>scheduleChange({weekday:Number(v)})}/></div>}
  </div><ScheduleDateTime label={msg('Start')} zone={schedule.timezone} value={schedule.startAt} onChange={startAt=>scheduleChange({startAt})} onValid={setStartValid}/ >
  <Disclosure title={msg('End date')}><ScheduleDateTime label={msg('End date')} zone={schedule.timezone} value={schedule.endAt??''} optional onChange={endAt=>scheduleChange({endAt:endAt||undefined})} onValid={setEndValid}/></Disclosure>
  <p className={styles.note}>{msg('Intervals use elapsed time. Calendar schedules skip nonexistent local times and run once when clocks repeat.')}</p>
  </section>}
  {value.kind==='file'&&<div className={styles.formGrid}><ProjectFile label={msg('Watch file')} value={value.watchPath??''} disabled={disabled} onChange={watchPath=>change({watchPath})}/><div className={styles.field}><label htmlFor={`${id}-stable`}>{msg('Stable for (seconds)')}</label><Input id={`${id}-stable`} type="number" min={1} max={3600} value={value.stableSeconds??5} onChange={e=>change({stableSeconds:Number(e.target.value)})}/></div><p className={styles.note}>{msg('Watch changes after enabling. Existing content is the baseline; a missing file can arrive later.')}</p></div>}
  <Disclosure title={msg('Execution policy')}><div className={styles.formGrid}>
   {(value.kind==='schedule'||value.kind==='cloud')&&<div className={styles.field}><label htmlFor={`${id}-missed`}>{msg('Missed runs')}</label><Select id={`${id}-missed`} value={value.missed} disabled={disabled} options={[{value:'skip',label:msg('Skip missed runs')},{value:'latest',label:value.kind==='cloud'?msg('Deliver before expiry'):msg('Run latest once')}]} onValueChange={missed=>change({missed:missed as TriggerConfig['missed']})}/></div>}
   {value.kind==='cloud'&&<p className={styles.note}>{msg('Skip ignores signals more than 30 seconds late. Deliver before expiry accepts each current signal. Expiry starts at the original scheduled time.')}</p>}
   <div className={styles.field}><label htmlFor={`${id}-overlap`}>{msg('While another run is pending')}</label><Select id={`${id}-overlap`} value={value.overlap} disabled={disabled} options={[{value:'skip',label:msg('Skip this occurrence')},{value:'queue',label:msg('Queue this occurrence')}]} onValueChange={overlap=>change({overlap:overlap as TriggerConfig['overlap']})}/></div>
   <div className={styles.field}><label htmlFor={`${id}-expiry`}>{msg('Queue expiry (minutes)')}</label><Input id={`${id}-expiry`} type="number" min={1} max={1440} value={value.queueSeconds/60} onChange={e=>change({queueSeconds:Number(e.target.value)*60})}/></div>
  </div></Disclosure>
  <p className={styles.note}>{msg('Saved paused. Enable after review. Desk must be running and this computer must be awake; closing the browser is safe.')}</p>
 </fieldset>
}
function ProjectFile({label,value,onChange,disabled}:{label:string;value:string;onChange:(p:string)=>void;disabled:boolean}){
 const id=useId(),[browse,setBrowse]=useState(false)
 const files=useQuery({queryKey:['trigger-project-files'],queryFn:({signal})=>listFiles(signal),enabled:browse})
 return <div className={styles.field}><label htmlFor={id}>{label}</label><div className={styles.filePicker}><Input id={id} value={value} placeholder={msg('Project-relative JSON path')} onChange={e=>onChange(e.target.value)} disabled={disabled}/><Button disabled={disabled} onClick={()=>setBrowse(!browse)}>{msg('Browse files')}</Button></div>{browse&&<><Select id={`${id}-choose`} aria-label={msg('Project files')} disabled={disabled} value={value} placeholder={msg('Choose a JSON file')} options={(files.data?.files??[]).filter(f=>f.path.toLowerCase().endsWith('.json')).map(f=>({value:f.path,label:f.path}))} onValueChange={p=>{onChange(p);setBrowse(false)}}/>{files.error&&<p role="alert" className={styles.problem}>{String(files.error)}</p>}</>}</div>
}
function AutomaticInputs({doc,mapping,value,onChange,onValid,disabled,fileOnly}:{doc:PackDocument;mapping?:Mapping;value:AutomaticInput;onChange:(v:AutomaticInput)=>void;onValid:(v:boolean)=>void;disabled:boolean;fileOnly:boolean}){
 const id=useId(),[fieldValid,setFieldValid]=useState(true)
 const paths=[value.path??'',...Object.values(value.files??{})].filter(Boolean)
 const valid=value.kind==='constant'?fieldValid:(paths.length>0||value.kind==='mapped-sources')&&paths.every(p=>!p.includes('\\')&&!p.startsWith('/')&&!p.split('/').some(s=>!s||s==='..'||s==='.')&&p.toLowerCase().endsWith('.json'))&&(!mapping?true:mapping.version===1?Boolean(value.files?.file):mapping.sources?.filter(s=>s.provider==='local-file').every(s=>Boolean(value.files?.[s.name]))??true)&&fieldValid
 useEffect(()=>onValid(valid),[valid,onValid])
 const canConstant=!mapping||mapping.version===2&&!mapping.sources?.length
 const changeKind=(kind:string)=>onChange(kind==='constant'?{kind:'constant',value:jobsRequest(mapping?{source:{mapping,case:{},sources:{}}}:{facts:{}})}:defaultAutomatic(mapping,true))
 return <section className={styles.fields}><h3>{msg('Inputs for each occurrence')}</h3>
  {value.kind==='mapped-sources'&&<p className={styles.note}>{msg('Fetch each operation through its installed Gateway connection. Preview makes a real request. Enabled triggers can make provider calls and incur charges.')}</p>}
  {canConstant&&!fileOnly&&<Select id={`${id}-kind`} aria-label={msg('Automatic input source')} value={value.kind} disabled={disabled} options={[{value:'constant',label:msg('Explicit constants')},{value:mapping?'mapped-files':'input-file',label:msg('Read project files')}]} onValueChange={changeKind}/>}
  {value.kind==='constant'&&<p className={styles.callout}>{msg('These values repeat on every occurrence. Choose a file or connected source when inputs should change.')}</p>}
  {value.kind==='constant'?<ConstantInputs doc={doc} mapping={mapping} value={value} onChange={onChange} onValid={setFieldValid} disabled={disabled}/>:value.kind==='input-file'?<><ProjectFile label={msg('Input file')} value={value.path??''} onChange={path=>onChange({...value,path})} disabled={disabled}/><p className={styles.note}>{msg('One JSON object containing facts and optional evidence availability. The file is read again for each occurrence.')}</p></>:<>
   {Object.keys(value.files??{}).map(name=><ProjectFile key={name} label={name} value={value.files![name]!} disabled={disabled} onChange={p=>onChange({...value,files:{...value.files,[name]:p}})}/>)}
   {mapping?.version===2&&<><ProjectFile label={msg('Case file (optional)')} value={value.path??''} disabled={disabled} onChange={path=>onChange({...value,path:path||undefined,case:path?undefined:{}})}/>{!value.path&&<Disclosure title={msg('Fixed case parameters')}><MappedCaseFields doc={doc} mapping={mapping} text={JSON.stringify(value.case??{})} disabled={disabled} onValid={setFieldValid} onChange={text=>onChange({...value,case:JSON.parse(text)})}/></Disclosure>}</>}
  </>}
 </section>
}

function ConstantInputs({doc,mapping,value,onChange,onValid,disabled}:{doc:PackDocument;mapping?:Mapping;value:AutomaticInput;onChange:(v:AutomaticInput)=>void;onValid:(v:boolean)=>void;disabled:boolean}){
 const [initial]=useState(()=>{try{return editableJSON(value.value??'{}') as JobInput}catch{return undefined}})
 const [facts,setFacts]=useState(JSON.stringify(initial?.facts??{},null,2)),[evidence,setEvidence]=useState(JSON.stringify(initial?.evidence??{},null,2)),[supplied,setSupplied]=useState(initial?.evidence!==undefined)
 const [caseText,setCaseText]=useState(JSON.stringify(initial?.source&&'case' in initial.source?initial.source.case:{})),[valid,setValid]=useState(true)
 useEffect(()=>onValid(valid),[valid,onValid])
 useDirtyGuard(!valid,msg('Discard the unfinished input values?'),{shouldBlock:({currentLocation,nextLocation})=>currentLocation.pathname!==nextLocation.pathname||currentLocation.search!==nextLocation.search})
 function commit(f:string,s:boolean,e:string){try{onChange({...value,value:jobsRequest(parseInput(f,s,e))});setValid(true)}catch{setValid(false)}}
 if(!initial)return <div className={styles.field}><TextArea aria-label={msg('Constant inputs (JSON)')} value={value.value} disabled={disabled} onChange={e=>{onChange({...value,value:e.target.value});try{const v=JSON.parse(e.target.value);if(!v||typeof v!=='object'||Array.isArray(v)||!('facts' in v))throw Error();setValid(true)}catch{setValid(false)}}}/><p className={styles.note}>{msg('Keep exact JSON values. Inputs are checked again before enabling.')}</p></div>
 if(mapping?.version===2)return <MappedCaseFields doc={doc} mapping={mapping} text={caseText} disabled={disabled} onValid={setValid} onChange={text=>{setCaseText(text);onChange({...value,value:jobsRequest({source:{mapping,case:JSON.parse(text),sources:{}}})})}}/>
 return <InputFields doc={doc} facts={facts} setFacts={text=>{setFacts(text);commit(text,supplied,evidence)}} supplied={supplied} setSupplied={s=>{setSupplied(s);commit(facts,s,evidence)}} evidence={evidence} setEvidence={text=>{setEvidence(text);commit(facts,supplied,text)}} onValid={setValid} disabled={disabled}/>
}

function ScheduleDateTime({label,zone,value,onChange,onValid,optional=false}:{label:string;zone:string;value:string;onChange:(v:string)=>void;onValid:(v:boolean)=>void;optional?:boolean}){
 const id=useId(),[text,setText]=useState(()=>scheduleWallTime(value,zone)),[error,setError]=useState(false)
 useDirtyGuard(error,msg('Discard the unfinished date and time?'),{shouldBlock:({currentLocation,nextLocation})=>currentLocation.pathname!==nextLocation.pathname||currentLocation.search!==nextLocation.search})
 useEffect(()=>{setText(scheduleWallTime(value,zone));setError(false)},[value,zone])
 const valid=!error&&Boolean(optional&&!text||scheduleInstant(text,zone))
 useEffect(()=>onValid(valid),[valid,onValid])
 return <div className={styles.field}><label htmlFor={id}>{label} · {zone}</label><Input id={id} type="datetime-local" step={60} value={text} aria-invalid={!valid||undefined} onChange={e=>{const raw=e.target.value;setText(raw);const at=scheduleInstant(raw,zone);if(at||optional&&!raw){setError(false);onChange(at??'')}else setError(true)}}/>{error&&<p role="alert" className={styles.problem}>{msg('Choose a valid date and time in this time zone.')}</p>}</div>
}
