import { sourceMessage } from '../i18n/source'
import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { msg } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { allowedAgentModels, type EffectiveConfig } from '../config/deskConfig'
import { ASSISTANT_PROFILE_PATH, decodeAssistantProfile, type AssistantProfile, type ModelPreferences } from '../config/assistantProfile'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import { writeFile } from '../files/client'
import { requestOpen } from '../shell/authorBridge'
import { useUnsavedChanges } from '../shell/DraftScope'
import { Button, ButtonLink } from '../ui/Button'
import { Field } from '../ui/Field'
import { Select } from '../ui/Select'
import { Input } from '../ui/Input'
import { Popover } from '../ui/Popover'
import { Disclosure } from '../ui/Disclosure'
import { SubscriptionModels } from './SubscriptionModels'
import { DeskModelPreferences } from './DeskModelPreferences'
import { useProviderModels, useProviderStatus } from './providers'
import { AI_CONNECTIONS_KEY, modelPreferences, type AIConnection, type AIRegistry } from './aiConnections'
import styles from './AIConnectionsSettings.module.css'

function initialProfile(read:EffectiveConfig['assistantProfile'],registry:AIRegistry):AssistantProfile {
 if(read?.value?.profileVersion===2)return read.value
 const models=Object.fromEntries(registry.connections.flatMap(c=>{const p=modelPreferences(read,c);return p&&!p.inherit?[[c.id,p]]:[]}))
 return Object.keys(models).length?{profileVersion:2,inherit:false,connections:registry.connections.filter(c=>c.enabled).map(c=>c.id),defaultConnection:registry.defaultConnection||null,models}:{profileVersion:2,inherit:true}
}
function ConnectionPreferences({connection:c,value,disabled,onChange}:{connection:AIConnection;value:ModelPreferences;disabled:boolean;onChange:(p:ModelPreferences)=>void}) {
 const native=c.assistant.engine==='codex',account=useProviderStatus(native&&c.enabled,c.id),catalog=useProviderModels(native&&c.enabled&&account.data?.account==='connected',c.id)
 const available=native?allowedAgentModels(c.assistant.agent):c.assistant.endpoint?.models??[]
 const baseline={models:available,model:native?c.assistant.agent?.model??null:c.assistant.endpoint?.model??null,effort:c.assistant.agent?.effort,thinking:c.assistant.thinking}
 const current=value.inherit?baseline:value
 const choices=native&&catalog.data?catalog.data.models:[...new Set([...available,...current.models])].map(id=>({id,name:id,efforts:[],defaultEffort:'none' as const}))
 const chosen=choices.find(m=>m.id===current.model)
 return <div className={styles.group}>
  <h3>{c.name}</h3>
  <label className={styles.label}><input type="checkbox" disabled={disabled} checked={value.inherit} onChange={e=>onChange(e.target.checked?{inherit:true}:{inherit:false,models:[...baseline.models],model:baseline.model,...(native?baseline.effort?{effort:baseline.effort}:{}:{thinking:baseline.thinking})})}/>{msg('Use connection model defaults')}</label>
  {value.inherit?<span className={styles.meta}>{msg('Default model')} · {baseline.model??msg('No model selected')}</span>:<>
   <SubscriptionModels models={choices} allowed={current.models} model={current.model??''} disabled={disabled} onChange={(models,model)=>onChange({inherit:false,models,model:model||null,...(native?model===current.model&&current.effort?{effort:current.effort}:{}:{thinking:current.thinking})})}/>
   {native&&current.model&&<Field label={msg('Reasoning effort')}>{w=><Select {...w} disabled={disabled||catalog.isPending||catalog.isError} value={current.effort??'__default__'} onValueChange={v=>{if(!value.inherit){const {effort:_,...rest}=value;onChange({...rest,...(v==='__default__'?{}:{effort:v as typeof value.effort})})}}} options={[{value:'__default__',label:msg('Model default')},...(chosen?.efforts??[]).map(value=>({value,label:value}))]}/>}</Field>}
   {!native&&<Field label={msg('Reasoning effort')}>{w=><Select {...w} disabled={disabled} value={current.thinking??c.assistant.thinking} onValueChange={v=>{if(!value.inherit)onChange({...value,thinking:v as typeof c.assistant.thinking})}} options={[{value:'off',label:msg('off')},{value:'on',label:msg('standard')},{value:'ultra',label:msg('deep')}]}/>}</Field>}
  </>}
  {native&&catalog.error&&<span role="alert" className={styles.meta}>{msg('Model details could not be loaded.')} <Button variant="inline" onClick={()=>void catalog.refetch()}>{msg('Retry')}</Button></span>}
 </div>
}
export function DeskAISettings(props:{unavailable:boolean;onDirtyChange?:(dirty:boolean)=>void}) {
 const effective=useEffectiveConfig()
 return effective.aiConnections?<ConnectedDeskSettings {...props}/>:<DeskModelPreferences {...props}/>
}
function ConnectedDeskSettings({unavailable,onDirtyChange}:{unavailable:boolean;onDirtyChange?:(dirty:boolean)=>void}) {
 const effective=useEffectiveConfig(),client=useQueryClient(),registry=effective.aiConnections?.data,read=effective.assistantProfile
 const baseline=registry?initialProfile(read,registry):{profileVersion:2 as const,inherit:true}
 const [draft,setDraft]=useState<AssistantProfile>(baseline),[base,setBase]=useState(read),[dirty,setDirty]=useState(false),[saved,setSaved]=useState(false),[search,setSearch]=useState('')
 const seed=JSON.stringify(baseline)
 useEffect(()=>{if(!dirty){setDraft(baseline);setBase(read)}},[seed,read,dirty])
 useUnsavedChanges(dirty);useEffect(()=>{onDirtyChange?.(dirty);return()=>onDirtyChange?.(false)},[dirty,onDirtyChange])
 const edit=(next:AssistantProfile)=>{if(!dirty)setBase(read);setDraft(next);setDirty(true);setSaved(false);save.reset()}
 const save=useMutation({mutationFn:async()=>{
  if(!base||base.sha256===undefined||base.problem)throw new Error(sourceMessage('Reload assistant preferences before saving.'))
  const content=JSON.stringify(draft,null,2)+'\n';decodeAssistantProfile(content)
  return writeFile({path:ASSISTANT_PROFILE_PATH,content,baseSha256:base.sha256})
 },onSuccess:file=>{
  client.setQueryData<EffectiveConfig>(DESK_CONFIG_QUERY_KEY,previous=>previous&&({...previous,assistantProfile:{present:true,sha256:file.sha256,text:file.content,value:decodeAssistantProfile(file.content)}}))
  void client.invalidateQueries({queryKey:DESK_CONFIG_QUERY_KEY});void client.invalidateQueries({queryKey:['desk-files']});void client.invalidateQueries({queryKey:['desk-file',ASSISTANT_PROFILE_PATH]});setDirty(false);setSaved(true)
 }})
 if(!registry)return <div><p role={effective.aiConnections?.problem?'alert':'status'}>{msg(effective.aiConnections?.problem?'AI connections could not be read.':'Loading AI connections…')}</p>{effective.aiConnections?.problem&&<Button onClick={()=>void client.invalidateQueries({queryKey:AI_CONNECTIONS_KEY})}>{msg('Retry')}</Button>}</div>
 const locked=unavailable||save.isPending||!!read?.problem||base?.sha256===undefined
 const enabled=registry.connections.filter(c=>c.enabled),ids=draft.inherit?enabled.map(c=>c.id):draft.connections??[]
 const selected=draft.inherit?registry.defaultConnection:draft.defaultConnection
 const choices=enabled.filter(c=>ids.includes(c.id)),missing=ids.filter(id=>!enabled.some(c=>c.id===id))
 const setConnections=(connections:string[])=>edit({...draft,inherit:false,connections,defaultConnection:connections.includes(selected??'')?selected??null:connections[0]??null,models:Object.fromEntries(Object.entries(draft.models??{}).filter(([id])=>connections.includes(id)))})
 let valid=true;try{decodeAssistantProfile(JSON.stringify(draft))}catch{valid=false}
 if(!draft.inherit&&(missing.length||ids.length>0&&!choices.some(c=>c.id===selected)))valid=false
 return <section className={styles.stack} aria-label={msg('Desk assistant settings')}>
  <p className="quiet">{msg('Choose the AI connections and models available in this desk. Credentials are managed in Connections > AI.')}</p>
  <label className={styles.label}><input type="checkbox" checked={!!draft.inherit} disabled={locked} onChange={e=>edit(e.target.checked?{profileVersion:2,inherit:true}:{profileVersion:2,inherit:false,connections:enabled.map(c=>c.id),defaultConnection:registry.defaultConnection||enabled[0]?.id||null,models:{}})}/>{msg('Use shared assistant defaults')}</label>
  {draft.inherit?<div className={styles.stack}><span className={styles.meta}>{msg('Default connection')} · {registry.connections.find(c=>c.id===registry.defaultConnection)?.name??msg('Not selected')}</span><span className={styles.meta}>{msg('Available connections')} · {enabled.map(c=>c.name).join(', ')||msg('None')}</span><span className={styles.meta}>{msg('Shared connection and model defaults apply here. Existing chats keep their selected connection.')}</span></div>:<>
   <Field label={msg('Enabled connections')}>{w=><Popover title={msg('Enabled connections')} align="start" trigger={<Button id={w.id} disabled={locked}>{msg('Connections')} · {ids.length}</Button>}>
    <Input type="search" aria-label={msg('Search connections')} placeholder={msg('Search connections')} value={search} onChange={e=>setSearch(e.target.value)}/>
    <div className={styles.toolbar}><Button variant="quiet" disabled={locked} onClick={()=>setConnections(enabled.map(c=>c.id))}>{msg('Select all')}</Button><Button variant="quiet" disabled={locked} onClick={()=>setConnections([])}>{msg('Remove all')}</Button></div>
    <div className={styles.choices}>{enabled.filter(c=>c.name.toLowerCase().includes(search.toLowerCase())).map(c=><label key={c.id} className={styles.label}><input type="checkbox" disabled={locked} checked={ids.includes(c.id)} onChange={e=>setConnections(e.target.checked?[...ids,c.id]:ids.filter(id=>id!==c.id))}/>{c.name}</label>)}</div>
   </Popover>}</Field>
   {missing.length>0&&<p role="alert">{msg('A selected connection is disabled or no longer available.')} <Button variant="inline" onClick={()=>setConnections(ids.filter(id=>!missing.includes(id)))}>{msg('Remove unavailable connections')}</Button></p>}
   <Field label={msg('Default connection')}>{w=><Select {...w} value={selected??''} disabled={locked||!choices.length} onValueChange={defaultConnection=>edit({...draft,defaultConnection})} placeholder={msg('Choose a connection')} options={choices.map(c=>({value:c.id,label:c.name}))}/>}</Field>
   {choices.map(c=><ConnectionPreferences key={c.id} connection={c} value={draft.models?.[c.id]??{inherit:true}} disabled={locked} onChange={value=>edit({...draft,models:{...draft.models,[c.id]:value}})}/>)}
  </>}
  <ButtonLink variant="quiet" to="/admin#connections">{msg('Manage AI connections')}</ButtonLink>
  {read?.problem&&<p role="alert">{read.problem}</p>}{save.error&&<p role="alert">{save.error.message}</p>}
  <div className={styles.toolbar}><span role="status" className={styles.meta}>{saved?msg('Assistant preferences saved.'):dirty?msg('Unsaved changes'):''}</span><div className={styles.actions}>{dirty&&<Button variant="quiet" onClick={()=>{setDirty(false);save.reset()}}>{msg('Cancel')}</Button>}<Button variant="primary" disabled={locked||!dirty||!valid} onClick={()=>save.mutate()}>{msg(save.isPending?'Saving…':'Save preferences')}</Button></div></div>
  <Disclosure title={msg('Advanced settings')}><div className={styles.actions}><Button variant="quiet" disabled={dirty||save.isPending} onClick={()=>void client.invalidateQueries({queryKey:DESK_CONFIG_QUERY_KEY})}>{msg('Reload from disk')}</Button>{read?.present&&<ButtonLink variant="inline" to="/author" onClick={()=>requestOpen(ASSISTANT_PROFILE_PATH)}>{ASSISTANT_PROFILE_PATH}</ButtonLink>}</div></Disclosure>
 </section>
}
