import { sourceMessage } from '../i18n/source'
import { DropdownMenu } from 'radix-ui'
import { IconMore } from '../shell/icons'
import { Disclosure } from '../ui/Disclosure'
import { PREFILLED_URL } from './endpointDraft'
import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { msg } from '../i18n'
import { useEffectiveConfig, DeskConfigFixture } from '../config/DeskConfigProvider'
import { ASSISTANT_TOOLS, type AssistantConfig } from '../config/deskConfig'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { Field, FieldGroup } from '../ui/Field'
import { Dialog, DialogActions } from '../ui/Dialog'
import { useUnsavedChanges } from '../shell/DraftScope'
import { MachineAssistantSettings } from './AssistantSettings'
import { useAssistantKey } from './queries'
import { useProviderStatus } from './providers'
import { AI_CONNECTIONS_KEY, AIConnectionScope, writeAIRegistry, type AIConnection, type AIRegistry } from './aiConnections'
import styles from './AIConnectionsSettings.module.css'

export const aiProviderLabel=(c:AIConnection)=>c.assistant.engine==='codex'?'ChatGPT subscription':c.assistant.endpoint?.kind==='anthropic'?'Anthropic API':c.assistant.endpoint?.kind==='gemini'?'Google Gemini API':'OpenAI-compatible API'
function Status({connection:c}:{connection:AIConnection}) {
 const native=c.assistant.engine==='codex',account=useProviderStatus(c.enabled&&native,c.id),key=useAssistantKey(c.enabled&&!native,c.id,c.revision)
 const state=!c.enabled?sourceMessage('Disabled'):native?account.isPending?sourceMessage('Checking connection…'):account.isError?sourceMessage('Could not check connection'):account.data?.account==='connected'?sourceMessage('Connected'):account.data?.account==='login-pending'?sourceMessage('Waiting for sign-in…'):sourceMessage('Not connected'):key.isPending?sourceMessage('Checking connection…'):key.isError?sourceMessage('Could not check connection'):key.data.bound?sourceMessage('API key saved'):sourceMessage('API key required')
 const model=c.assistant.engine==='codex'?c.assistant.agent?.model:c.assistant.endpoint?.model
 return <span className={styles.meta}>{msg((state===sourceMessage('Connected')||state===sourceMessage('API key saved'))&&!model?sourceMessage('Choose models'):state)}</span>
}
function ConnectionEditor({registry,connection:c,unavailable,onDirtyChange,onBack}:{registry:AIRegistry;connection:AIConnection;unavailable:boolean;onDirtyChange?:(dirty:boolean)=>void;onBack:()=>void}){
 const effective=useEffectiveConfig(),[dirty,setDirty]=useState(false),[discard,setDiscard]=useState(false)
 const [base,setBase]=useState({registry,connection:c})
 useEffect(()=>{if(!dirty)setBase({registry,connection:c})},[registry,c,dirty])
 useEffect(()=>{onDirtyChange?.(dirty);return()=>onDirtyChange?.(false)},[dirty,onDirtyChange])
 const value={...effective,aiConnections:undefined,machineAssistant:base.connection.assistant,assistantProfile:undefined,config:{...effective.config,assistant:base.connection.assistant},desk:{...effective.desk,path:base.registry.path,present:true,sha256:base.registry.sha256,problems:[]}}
 return <div className={styles.stack}>
  <div className={styles.toolbar}><Button variant="quiet" onClick={()=>dirty?setDiscard(true):onBack()}>{msg('Back to connections')}</Button><span className={styles.name}>{c.name}</span></div>
  <p className="quiet">{msg('Settings and credentials belong to this connection. Each desk chooses which connections to use.')}</p>
  <AIConnectionScope value={base}><DeskConfigFixture value={value}><MachineAssistantSettings unavailable={unavailable} onDirtyChange={setDirty}/></DeskConfigFixture></AIConnectionScope>
  <Dialog open={discard} onOpenChange={setDiscard} title={msg('Discard unsaved changes?')}><p>{msg('Your saved connection will stay unchanged.')}</p><DialogActions><Button variant="quiet" onClick={()=>setDiscard(false)}>{msg('Keep editing')}</Button><Button onClick={onBack}>{msg('Discard changes')}</Button></DialogActions></Dialog>
 </div>
}
export function AIConnectionsSettings({unavailable,onDirtyChange}:{unavailable:boolean;onDirtyChange?:(dirty:boolean)=>void}){
 const effective=useEffectiveConfig(),client=useQueryClient(),state=effective.aiConnections,registry=state?.data
 const [selected,setSelected]=useState<string|null>(null),[adding,setAdding]=useState(false),[rename,setRename]=useState<AIConnection|null>(null)
 const [name,setName]=useState(''),[provider,setProvider]=useState('codex'),[snapshot,setSnapshot]=useState<AIRegistry>()
 useUnsavedChanges(adding&&!!name||!!rename&&name!==rename.name)
 useEffect(()=>{onDirtyChange?.(adding&&!!name||!!rename&&name!==rename.name);return()=>onDirtyChange?.(false)},[adding,name,rename,onDirtyChange])
 const save=useMutation({mutationFn:async(input:{base:AIRegistry;connections:AIConnection[];defaultConnection?:string})=>writeAIRegistry(input.base,input.connections,input.defaultConnection),onSuccess:next=>{client.setQueryData(AI_CONNECTIONS_KEY,next);setAdding(false);setRename(null)}})
 if(!registry)return <div className={styles.stack}><p role={state?.problem?'alert':'status'}>{msg(state?.problem?'AI connections could not be read.':'Loading AI connections…')}</p>{state?.problem&&<Button onClick={()=>void client.invalidateQueries({queryKey:AI_CONNECTIONS_KEY})}>{msg('Retry')}</Button>}</div>
 const chosen=registry.connections.find(c=>c.id===selected)
 if(chosen)return <ConnectionEditor key={chosen.id} registry={registry} connection={chosen} unavailable={unavailable} onDirtyChange={onDirtyChange} onBack={()=>setSelected(null)}/>
 const busy=unavailable||save.isPending
 const create=()=>{
  if(!name.trim()||!snapshot)return
  const id='ai-'+Array.from(crypto.getRandomValues(new Uint8Array(12)),v=>v.toString(16).padStart(2,'0')).join('')
  const assistant:AssistantConfig=provider==='codex'?{engine:'codex',endpoint:null,thinking:'off',agent:{provider:'openai',authMethod:'subscription',model:null,models:[],tools:[...ASSISTANT_TOOLS]}}:{engine:'vercel',thinking:'off',endpoint:{kind:provider as 'anthropic'|'gemini'|'openai-compatible',url:PREFILLED_URL[provider as keyof typeof PREFILLED_URL],models:[],model:null,tools:[...ASSISTANT_TOOLS]}}
  const c:AIConnection={id,name:name.trim(),enabled:true,assistant,revision:''}
  save.mutate({base:snapshot,connections:[...snapshot.connections,c],defaultConnection:snapshot.defaultConnection||id},{onSuccess:()=>setSelected(id)})
 }
 return <div className={styles.stack}>
  <div className={styles.toolbar}><span className="quiet">{msg('Saved AI connections')} · {registry.connections.length}</span><Button disabled={busy||registry.connections.length>=32} onClick={()=>{setName('');setSnapshot(registry);save.reset();setAdding(true)}}>{msg('Add connection')}</Button></div>
  {!registry.connections.length&&<p className="quiet">{msg('Add a ChatGPT account or an API connection. You can keep several providers connected.')}</p>}
  <div className={styles.list}>{registry.connections.map(c=><div className={styles.row} key={c.id}>
   <div className={styles.info}><div className={styles.actions}><span className={styles.name}>{c.name}</span>{registry.defaultConnection===c.id&&<span className="badge">{msg('Default')}</span>}</div><span className={styles.meta}>{msg(aiProviderLabel(c))}</span><Status connection={c}/></div>
   <div className={styles.actions}>
    <Button variant="quiet" disabled={busy} onClick={()=>setSelected(c.id)}>{msg('Manage')}</Button>
    <DropdownMenu.Root><DropdownMenu.Trigger asChild><Button variant="quiet" size="icon" disabled={busy} aria-label={msg('Connection actions: {{name}}',{name:c.name})}><IconMore/></Button></DropdownMenu.Trigger>
     <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" align="end" sideOffset={4}>
      <DropdownMenu.Item className="desk-menu-item" onSelect={()=>{setRename(c);setName(c.name);setSnapshot(registry);save.reset()}}>{msg('Rename')}</DropdownMenu.Item>
      {c.enabled&&registry.defaultConnection!==c.id&&<DropdownMenu.Item className="desk-menu-item" onSelect={()=>save.mutate({base:registry,connections:registry.connections,defaultConnection:c.id})}>{msg('Set default')}</DropdownMenu.Item>}
      <DropdownMenu.Item className="desk-menu-item" onSelect={()=>save.mutate({base:registry,connections:registry.connections.map(row=>row.id===c.id?{...row,enabled:!row.enabled}:row),defaultConnection:registry.defaultConnection===c.id?'':registry.defaultConnection})}>{msg(c.enabled?'Disable':'Enable')}</DropdownMenu.Item>
     </DropdownMenu.Content></DropdownMenu.Portal>
    </DropdownMenu.Root>
   </div>
  </div>)}</div>
  <p className="quiet">{msg('The shared default is used by desks that inherit assistant settings. Custom desks keep their own selection. Disabling a connection makes it unavailable to every desk.')}</p>
  <Disclosure title={msg('Advanced settings')}><p className={styles.meta}>{msg('Connection settings are stored on this computer.')}</p><code className={styles.meta}>{registry.path}</code><p className={styles.meta}>{msg('API keys and account credentials are stored separately and are never included in project files.')}</p></Disclosure>
  {save.error&&!adding&&!rename&&<p role="alert">{save.error.message}</p>}
  <Dialog open={adding||!!rename} onOpenChange={open=>{if(!save.isPending&&!open){setAdding(false);setRename(null)}}} title={msg(rename?sourceMessage('Rename connection'):sourceMessage('Add AI connection'))}>
   <FieldGroup><Field label={msg('Name')}>{w=><Input {...w} value={name} maxLength={128} autoFocus placeholder={msg('For example, Anthropic — Work')} onChange={e=>setName(e.target.value)}/>}</Field>
   {adding&&<Field label={msg('Provider')}>{w=><Select {...w} value={provider} onValueChange={setProvider} options={[{value:'codex',label:msg('ChatGPT subscription')},{value:'openai-compatible',label:msg('OpenAI-compatible API')},{value:'anthropic',label:msg('Anthropic API')},{value:'gemini',label:msg('Google Gemini API')}]}/>}</Field>}</FieldGroup>
   {save.error&&<p role="alert">{save.error.message}</p>}
   <DialogActions><Button variant="quiet" disabled={save.isPending} onClick={()=>{setAdding(false);setRename(null)}}>{msg('Cancel')}</Button><Button disabled={busy||!name.trim()} onClick={()=>rename&&snapshot?save.mutate({base:snapshot,connections:snapshot.connections.map(c=>c.id===rename.id?{...c,name:name.trim()}:c)}):create()}>{msg(save.isPending?'Saving…':rename?sourceMessage('Save name'):'Add connection')}</Button></DialogActions>
  </Dialog>
 </div>
}
