import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { msg } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { allowedAgentModels } from '../config/deskConfig'
import { ASSISTANT_PROFILE_PATH, INHERITED_PROFILE, PROFILE_PROBLEM, applyAssistantProfile, decodeAssistantProfile, type AssistantProfile, type ModelPreferences } from '../config/assistantProfile'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import type { EffectiveConfig } from '../config/deskConfig'
import { writeFile } from '../files/client'
import { useUnsavedChanges } from '../shell/DraftScope'
import { requestOpen } from '../shell/authorBridge'
import { SubscriptionModels } from './SubscriptionModels'
import { useProviderModels, useProviderStatus } from './providers'
import { Disclosure } from '../ui/Disclosure'
import { Field } from '../ui/Field'
import { Select } from '../ui/Select'
import { Button, ButtonLink } from '../ui/Button'
import styles from './AssistantSettings.module.css'

export function DeskModelPreferences({unavailable,onDirtyChange}:{unavailable:boolean;onDirtyChange?:(dirty:boolean)=>void}) {
 const effective=useEffectiveConfig(),client=useQueryClient()
 const machine=effective.machineAssistant??effective.config.assistant,read=effective.assistantProfile
 const codex=machine.engine==='codex',key=codex?'codex':'api'
 const account=useProviderStatus(codex),catalog=useProviderModels(codex&&account.data?.account==='connected')
 const baseline=read?.value?.[key]??{inherit:true} as ModelPreferences
 const [draft,setDraft]=useState<ModelPreferences>(baseline),[dirty,setDirty]=useState(false),[saved,setSaved]=useState(false)
 const [base,setBase]=useState(read),[draftKey,setDraftKey]=useState(key)
 const seed=JSON.stringify(baseline)
 useEffect(()=>{if(!dirty){setDraft(baseline);setBase(read);setDraftKey(key)}},[seed,dirty,read,key])
 useUnsavedChanges(dirty)
 useEffect(()=>{onDirtyChange?.(dirty);return()=>onDirtyChange?.(false)},[dirty,onDirtyChange])
 const custom=codex?machine.agent:machine.endpoint
 const current=draft.inherit ? {models:codex?allowedAgentModels(machine.agent):machine.endpoint?.models??[],model:custom?.model??null,effort:machine.agent?.effort,thinking:machine.thinking} : draft
 const choices=codex?catalog.data?.models??[]:[...new Set([...(machine.endpoint?.models??[]),...(!draft.inherit?draft.models:[])])].map(id=>({id,name:id,efforts:[],defaultEffort:'none' as const}))
 const chosen=choices.find(row=>row.id===current.model)
 const edit=(value:ModelPreferences)=>{if(!dirty){setBase(read);setDraftKey(key)}setDraft(value);setDirty(true);setSaved(false);write.reset()}
 const valid=draft.inherit || draft.models.length===0 || (!!custom&&draft.models.every(id=>choices.some(row=>row.id===id))&&draft.models.includes(draft.model??'')&&(!codex||!draft.effort||chosen?.efforts.includes(draft.effort)))
 const write=useMutation({mutationFn:async()=>{
   if(!base||base.sha256===undefined||base.problem||draftKey!==key)throw new Error(PROFILE_PROBLEM)
   const value:AssistantProfile={...(base.value??INHERITED_PROFILE),[draftKey]:draft}
   const content=JSON.stringify(value,null,2)+'\n';decodeAssistantProfile(content)
   return writeFile({path:ASSISTANT_PROFILE_PATH,content,baseSha256:base.sha256})
  },onSuccess:file=>{
   const profile={present:true,sha256:file.sha256,text:file.content,value:decodeAssistantProfile(file.content)}
   client.setQueryData<EffectiveConfig>(DESK_CONFIG_QUERY_KEY,previous=>previous&&({...previous,assistantProfile:profile,config:{...previous.config,assistant:applyAssistantProfile(previous.machineAssistant??machine,profile)}}))
   void client.invalidateQueries({queryKey:DESK_CONFIG_QUERY_KEY});void client.invalidateQueries({queryKey:['desk-files']});void client.invalidateQueries({queryKey:['desk-file',ASSISTANT_PROFILE_PATH]})
   setDirty(false);setSaved(true)
  }})
 const locked=unavailable||write.isPending||draftKey!==key||!!read?.problem||read?.sha256===undefined
 return <section className={styles.subscription} aria-label={msg('Desk model preferences')}>
  <div><h3>{msg('This desk')}</h3><p className="quiet">{draft.inherit?msg('Using shared defaults. Changes to shared defaults apply here.'):msg('Model choices and reasoning are saved for this desk only.')}</p></div>
  <label className={`checkbox ${styles.inherit}`}><input type="checkbox" checked={draft.inherit} disabled={locked} onChange={event=>edit(event.target.checked?{inherit:true}:{inherit:false,models:[...current.models],model:current.model,...(codex?(current.effort?{effort:current.effort}:{}):{thinking:current.thinking})})}/>{msg('Use shared defaults')}</label>
  {draft.inherit ? <dl className={styles.modelSummary}>
   <div><dt>{msg('Allowed models')}</dt><dd>{current.models.length ? current.models.join(', ') : msg('No models enabled')}</dd></div>
   <div><dt>{msg('Default model')}</dt><dd>{current.model??msg('No model selected')}</dd></div>
   <div><dt>{msg('Reasoning effort')}</dt><dd>{codex?current.effort??msg('Model default'):current.thinking??machine.thinking}</dd></div>
  </dl> : <>
  <SubscriptionModels models={choices} allowed={current.models} model={current.model??''} disabled={locked||draft.inherit||codex&&(catalog.isPending||catalog.isError)}
   onChange={(models,model)=>edit({inherit:false,models,model:model||null,...(codex?(model===current.model&&current.effort?{effort:current.effort}:{}):{thinking:current.thinking})})}/>
  {codex&&current.model&&<Field label={msg('Reasoning effort')}>{wiring=><Select {...wiring} value={current.effort??'__default__'} disabled={locked||draft.inherit||!chosen}
    onValueChange={value=>{if(!draft.inherit){const {effort:_,...rest}=draft;edit({...rest,...(value==='__default__'?{}:{effort:value as typeof draft.effort})})}}} options={[{value:'__default__',label:msg('Model default')},...(chosen?.efforts??[]).map(value=>({value,label:value}))]}/>}</Field>}
  {!codex&&<Field label={msg('Reasoning effort')}>{wiring=><Select {...wiring} value={current.thinking??machine.thinking} disabled={locked||draft.inherit} onValueChange={value=>{if(!draft.inherit)edit({...draft,thinking:value as typeof machine.thinking})}} options={[{value:'off',label:msg('off')},{value:'on',label:msg('standard')},{value:'ultra',label:msg('deep')}]}/>}</Field>}
  </>}
  <ButtonLink variant="quiet" to="/admin#connections">{msg('Manage shared AI settings')}</ButtonLink>
  {catalog.error&&codex&&<p role="alert">{msg(catalog.error.message)}</p>}
  {read?.problem&&<p role="alert">{msg(read.problem)}</p>}
  {write.error&&<p role="alert">{write.error.message}</p>}
  <div className={styles.actions}>
   {dirty&&<Button variant="quiet" onClick={()=>{setDirty(false);setDraft(baseline);setSaved(false);write.reset()}}>{msg('Cancel')}</Button>}
   <Button variant="primary" disabled={locked||!valid||(!dirty&&read?.present)} onClick={()=>write.mutate()}>{write.isPending?msg('Saving…'):msg('Save preferences')}</Button>
  </div>
  <Disclosure title={msg('Advanced settings')}>
   <div className={styles.actions}><Button variant="quiet" disabled={write.isPending||dirty} onClick={()=>void client.invalidateQueries({queryKey:DESK_CONFIG_QUERY_KEY})}>{msg('Reload from disk')}</Button>
   {read?.present&&<ButtonLink variant="inline" to="/author" onClick={()=>requestOpen(ASSISTANT_PROFILE_PATH)}>{ASSISTANT_PROFILE_PATH}</ButtonLink>}
  </div>
  </Disclosure>
  {saved&&<p role="status">{msg('Desk model preferences saved.')}</p>}
 </section>
}
