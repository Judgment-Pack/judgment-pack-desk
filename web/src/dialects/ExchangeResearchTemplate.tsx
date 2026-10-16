import {useRef,useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {msg} from '../i18n'
import {exchangeImportsQuery,exchangeImportQuery} from '../exchange/client'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {TextArea} from '../ui/TextArea'
import {parseMapping} from '../jobs/mappedInputs'
import {Button,ButtonLink} from '../ui/Button'
import {LoadingState} from '../ui/LoadingState'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import type {DialectTemplate,TemplateOrigin} from './templates'
import {researchDraftFromPreset} from './exchangeResearchTemplate'
import styles from './DialectDraftPage.module.css'

export function ExchangeResearchTemplate({current,purpose,disabled,onApply}:{current?:TemplateOrigin;purpose:string;disabled:boolean;onApply:(template:DialectTemplate,origin:TemplateOrigin)=>Promise<void>}){
 const imports=useQuery(exchangeImportsQuery()),[selected,setSelected]=useState(''),[error,setError]=useState<unknown>()
 // Generated instructions from a prior preset are never the next preset's task.
 const [question,setQuestion]=useState(current?.id.startsWith('exchange-')?'':purpose)
 const applying=useRef(false),[busy,setBusy]=useState(false)
 const retained=useQuery({...exchangeImportQuery(selected),enabled:!!selected})
 const selectedImport=retained.data?.import.id===selected?retained.data:undefined
 let needsQuestion=false
 try{const source=parseMapping(selectedImport?.candidate.document??'{}').sources?.[0];needsQuestion=source?.kind==='research'&&typeof source.question!=='string'}catch{}
 const apply=async()=>{if(!selectedImport||disabled||applying.current)return;applying.current=true;setBusy(true);try{setError(undefined);const draft=researchDraftFromPreset(selectedImport.candidate,selectedImport.import,question);await onApply(draft.template,draft.origin)}catch(e){setError(e)}finally{applying.current=false;setBusy(false)}}
 return <div className={styles.stack}>
  <QueryStatusNotice id="draft-template-list" label={msg('Templates unavailable')} error={imports.error} retrying={imports.isFetching} onRetry={()=>imports.refetch()}/>
  <QueryStatusNotice id="draft-template-read" label={msg('Template unavailable')} error={retained.error} retrying={retained.isFetching} onRetry={()=>retained.refetch()}/>
  <QueryStatusNotice id="draft-template-apply" label={msg('Template needs attention')} error={error} retryLabel={msg('Dismiss')} onRetry={()=>setError(undefined)}/>
  <div><ButtonLink to="/dialects/templates" size="small" variant="quiet">{msg('Exchange templates')}</ButtonLink></div>
  {current&&<p className={styles.note}>{current.name} · {msg('Template license: {{license}}',{license:current.license})}</p>}
  {imports.isPending?<LoadingState inline>{msg('Loading templates…')}</LoadingState>:!!imports.data?.items.length?<Field label={msg('Imported template')}>{props=><Select {...props} disabled={disabled||busy} value={selected} placeholder={msg('Choose a template')} options={[...imports.data!.items].reverse().map(item=>({value:item.id,label:`${item.entryId} · ${item.tag}`}))} onValueChange={id=>{setSelected(id);setError(undefined)}}/>}</Field>:!imports.error&&<p className={styles.note}>{msg('No imported templates.')}</p>}
  {!!selected&&retained.isPending&&<LoadingState inline>{msg('Loading template…')}</LoadingState>}
  {selectedImport&&<>{needsQuestion&&<Field label={msg('What records do you need?')}>{props=><TextArea {...props} value={question} disabled={disabled||busy} onChange={event=>setQuestion(event.target.value)}/>}</Field>}<p>{selectedImport.candidate.entry.description}</p><p className={styles.note}>{msg('The imported template stays unchanged. Review the copied fields and instructions; choose connections and limits locally.')}</p><div><Button size="small" disabled={disabled||busy||needsQuestion&&!question.trim()} onClick={()=>void apply()}>{msg('Use template')}</Button></div></>}
 </div>
}
