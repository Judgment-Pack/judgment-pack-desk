import styles from './DialectsView.module.css'
import {useRef,useState,type RefObject} from 'react'
import {useNavigate} from 'react-router-dom'
import {useQueryClient} from '@tanstack/react-query'
import {msg} from '../i18n'
import {useEffectiveConfig} from '../config/DeskConfigProvider'
import {resolveAIConnection} from '../assistant/aiConnections'
import {Button} from '../ui/Button'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Disclosure} from '../ui/Disclosure'
import {Field,FieldGroup} from '../ui/Field'
import {Input} from '../ui/Input'
import {TextArea} from '../ui/TextArea'
import {Select} from '../ui/Select'
import {Notice} from '../ui/Notice'
import {ResearchRecipe} from './ResearchRecipe'
import {defaultResearchRetries,type ResearchRecipe as Recipe} from './research'
import {dialectAPI,type PreparationReply} from './client'
import {dialectTemplates,readDialectTemplate,templateOrigin,type DialectTemplate} from './templates'

export function NewDialectDialog({opener,onClose,companies}:{opener:RefObject<HTMLButtonElement|null>;onClose:()=>void;companies?:{source:PreparationReply;recordIds:string[]}}){
 const cache=useQueryClient(),navigate=useNavigate(),effective=useEffectiveConfig()
 const templates=dialectTemplates(),first=templates.find(t=>t.id===(companies?'company-people':'blank'))!
 const resolved=resolveAIConnection(effective),assistant=resolved.assistant
 const config=assistant.engine==='codex'?assistant.agent:assistant.endpoint
 const recipeFor=(t:DialectTemplate):Recipe|undefined=>t.research?{version:1,...structuredClone(t.research),retries:defaultResearchRetries(),limits:{...t.research.limits,searches:0},ai:{connection:resolved.connection?.id??'',revision:resolved.connection?.revision??'',model:config?.model??''},...(companies&&t.research.target==='people-at-companies'?{companyInputs:companies.source.document.records.filter(r=>companies.recordIds.includes(r.id)&&r.research?.entity.kind==='company').map(r=>({preparationId:companies.source.document.id,revision:companies.source.sha256,entityId:r.research!.entity.id}))}:{})}:undefined
 const [template,setTemplate]=useState(first),[imported,setImported]=useState<DialectTemplate>(),[research,setResearch]=useState(()=>recipeFor(first))
 const [name,setName]=useState(companies?msg('People at {{name}}',{name:companies.source.document.name}):''),[intent,setIntent]=useState(first.intent)
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),reading=useRef(0),active=useRef(false)
 const apply=(next:DialectTemplate)=>{setTemplate(next);setIntent(next.intent);setResearch(recipeFor(next));setError('')}
 const companyReady=research?.target!=='people-at-companies'||(research.companyIds?.length??0)+(research.companyInputs?.length??0)>0
 const ready=!!name.trim()&&companyReady&&(!research||!!research.ai.connection&&!!research.ai.revision&&!!research.ai.model)
 const create=async()=>{if(active.current||!ready)return;active.current=true;setBusy(true);setError('');try{
  const origin=await templateOrigin(template)
  const saved=await dialectAPI<PreparationReply>('','POST',{name:name.trim(),recipe:{intent,template:origin,...(research?{research}:{})}})
  await cache.invalidateQueries({queryKey:['input-preparations']});onClose();navigate('/dialects/'+saved.document.id+'?tab=recipe')
 }catch(e){setError(e instanceof Error?e.message:msg('The request could not be completed.'))}finally{active.current=false;setBusy(false)}}
 return <Dialog open onOpenChange={open=>{if(!open&&!busy){reading.current++;onClose()}}} title={msg('New dialect')} size="form" openerRef={opener} footer={<DialogActions><Button disabled={busy} onClick={onClose}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy||!ready} onClick={()=>void create()}>{msg('Create dialect')}</Button></DialogActions>}><FieldGroup>
  <Field label={msg('Template')} hint={template.description}>{props=><Select {...props} disabled={busy} value={template.id} options={[...templates.filter(t=>t.id!==imported?.id),...(imported?[imported]:[])].map(t=>({value:t.id,label:t.name}))} onValueChange={id=>apply(imported?.id===id?imported:templates.find(t=>t.id===id)!)}/>}</Field>
  {imported?.id===template.id&&<p className={styles.note}>{msg('Template license: {{license}}',{license:template.license})}</p>}
  <Field label={msg('Name')}>{props=><Input {...props} autoFocus disabled={busy} maxLength={160} value={name} onChange={e=>setName(e.target.value)}/>}</Field>
  <Field label={msg('What records do you need?')}>{props=><TextArea {...props} disabled={busy} rows={4} maxLength={12000} value={intent} onChange={e=>setIntent(e.target.value)}/>}</Field>
  {research&&<ResearchRecipe value={research} onChange={setResearch} records={[]} disabled={busy}/>}
  <Disclosure title={msg('Import a template')}><Field label={msg('Template file')} hint={msg('Choose a Desk template JSON file. Review its instructions and settings before creating. Connections and approvals are configured here.')}>{props=><Input {...props} type="file" accept=".json,application/json" disabled={busy} onChange={async e=>{const file=e.target.files?.[0],version=++reading.current;if(!file)return;try{if(file.size>65536)throw Error(msg('Choose a template smaller than 64 KB.'));const next=readDialectTemplate(await file.text());if(reading.current!==version)return;setImported(next);apply(next)}catch(e){if(reading.current===version)setError(e instanceof Error?e.message:msg('The file could not be read.'))}}}/>}</Field></Disclosure>
  <p className={styles.note}>{msg('Creating saves the recipe only. Research and Jobs start after a separate review.')}</p>
  {error&&<Notice tone="error">{error}</Notice>}
 </FieldGroup></Dialog>
}
