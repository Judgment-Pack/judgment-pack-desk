import { useAIConnectionScope } from './aiConnections'
import { AIConnectionsSettings } from './AIConnectionsSettings'
import { DeskAISettings } from './DeskAISettings'
import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { DeskConfigFixture, useEffectiveConfig } from '../config/DeskConfigProvider'
import { applyAssistantProfile } from '../config/assistantProfile'
import { SubscriptionModels } from './SubscriptionModels'
import { allowedAgentModels, ASSISTANT_TOOLS, type AssistantAgentConfig } from '../config/deskConfig'
import { EndpointForm } from './EndpointForm'
import { Disclosure } from '../ui/Disclosure'
import { Field, FieldGroup } from '../ui/Field'
import { Select } from '../ui/Select'
import { Button } from '../ui/Button'
import { RunStatus } from '../ui/RunStatus'
import { Dialog, DialogActions } from '../ui/Dialog'
import { useUpdateAssistantConfig } from './queries'
import { useAssistantRun } from './useAssistantRun'
import { ProviderError, providerRequest, useProviderCatalog, useProviderModels, useProviderStatus, type ProviderChallenge } from './providers'
import { whenSessionEnds } from '../mcp/session'
import { useUnsavedChanges } from '../shell/DraftScope'
import styles from './AssistantSettings.module.css'

export function MachineAssistantSettings({ unavailable, onDirtyChange }: { unavailable: boolean; onDirtyChange?: (dirty:boolean)=>void }) {
  useLocale()
  const { config } = useEffectiveConfig()
  const scope=useAIConnectionScope()
  const [access, setAccess] = useState(config.assistant.engine)
  const [apiDirty,setApiDirty]=useState(false), [agentDirty,setAgentDirty]=useState(false)
  useEffect(()=>{ if (!apiDirty && !agentDirty) setAccess(config.assistant.engine) },[config.assistant.engine])
  useUnsavedChanges(access!==config.assistant.engine)
  useEffect(()=>onDirtyChange?.(apiDirty || agentDirty || access!==config.assistant.engine),[apiDirty,agentDirty,access,config.assistant.engine,onDirtyChange])
  return <div className={styles.settings}>
    {!scope&&<Field label={msg('Connection method')}>
      {wiring=><Select {...wiring} value={access} disabled={unavailable} onValueChange={value=>setAccess(value as typeof access)} options={[
        {value:'vercel',label:msg('API key')}, {value:'codex',label:msg('ChatGPT subscription')}
      ]}/>}
    </Field>}
    {(!scope||access==='vercel')&&<div hidden={access!=='vercel'}><EndpointForm unavailable={unavailable} onDirtyChange={setApiDirty}/></div>}
    {(!scope||access==='codex')&&<div hidden={access!=='codex'}><SubscriptionSettings active={access==='codex'} unavailable={unavailable} onDirtyChange={setAgentDirty}/></div>}
  </div>
}

function SubscriptionSettings({active,unavailable,onDirtyChange}:{active:boolean;unavailable:boolean;onDirtyChange:(dirty:boolean)=>void}) {
  const scope=useAIConnectionScope()
  const {config,desk,assistantProfile}=useEffectiveConfig()
  const catalog=useProviderCatalog(active)
  const provider=catalog.data?.providers.find(item=>item.id==='openai' && item.authMethod==='subscription' && item.agent==='codex')
  const status=useProviderStatus(active && provider?.enabled===true)
  const connected=status.data?.account==='connected'
  const statusUnknown=!!status.error&&!status.data
  const listing=useProviderModels(active && connected)
  const client=useQueryClient(), write=useUpdateAssistantConfig()
  const [model,setModel]=useState(config.assistant.agent?.model??'')
  const [allowed,setAllowed]=useState(()=>[...allowedAgentModels(config.assistant.agent)])
  const [effort,setEffort]=useState(config.assistant.agent?.effort??'')
  const [tools,setTools]=useState<AssistantAgentConfig['tools']>(config.assistant.agent?.tools??[...ASSISTANT_TOOLS])
  const [method,setMethod]=useState('browser'), [challenge,setChallenge]=useState<ProviderChallenge|null>(null)
  const [connecting,setConnecting]=useState(false)
  const [busy,setBusy]=useState(false), [problem,setProblem]=useState(''), [saved,setSaved]=useState(false), [disconnect,setDisconnect]=useState(false)
  const [dirty,setDirty]=useState(false)
  const mounted=useRef(true), generation=useRef(0), operation=useRef<AbortController|null>(null)
  const disconnectButton=useRef<HTMLButtonElement>(null)
  useUnsavedChanges(dirty)
  useEffect(()=>{onDirtyChange(dirty);return()=>onDirtyChange(false)},[dirty,onDirtyChange])
  useEffect(()=>{if(!dirty){setAllowed([...allowedAgentModels(config.assistant.agent)]);setModel(config.assistant.agent?.model??'');setEffort(config.assistant.agent?.effort??'');setTools(config.assistant.agent?.tools??[...ASSISTANT_TOOLS])}},[config.assistant.agent,dirty])
  useEffect(()=>{
    mounted.current=true
    const ended=()=>{ generation.current++;operation.current?.abort();setChallenge(null);setBusy(false);setConnecting(false);void client.removeQueries({queryKey:['model-provider']}) }
    const stop=whenSessionEnds(ended)
    return()=>{mounted.current=false;generation.current++;operation.current?.abort();stop()}
  },[client])
  useEffect(()=>{
    if (!challenge) return
    if (connected || (status.data?.login && status.data.login.id===challenge.id && status.data.login.state!=='pending')) {setChallenge(null);return}
    const timer=setTimeout(()=>setChallenge(null),Math.max(0,Date.parse(challenge.expiresAt)-Date.now()))
    return()=>clearTimeout(timer)
  },[challenge,connected,status.data?.login])
  const refresh=async()=>{if(scope)await Promise.all(['status','models'].map(action=>client.invalidateQueries({queryKey:['model-provider','openai',action,scope.connection.id]})));else await client.invalidateQueries({queryKey:['model-provider','openai']})}
  const action=async(name:'login'|'cancel'|'logout')=>{
    if(busy)return
    const turn=++generation.current, controller=new AbortController();operation.current=controller
    setBusy(true);setConnecting(name==='login');setProblem('');setSaved(false)
    try {
      const body=name==='login'?{method}:name==='cancel'?{id:challenge?.id??status.data?.login?.id}:{}
      const answer=await providerRequest<ProviderChallenge>(name,body,controller.signal,scope?.connection.id)
      if(!mounted.current||turn!==generation.current)return
      // **The link, "Cancel sign-in" and the end of the progress line come
      // from this one answer** (issue #243). The status refresh below only
      // confirms it: while it ran, the connection's "Cancel" was still drawn
      // beside the link, and a click on it ended the local state, sent no
      // cancel for the attempt, and left the link.
      if(name==='login'){setChallenge(answer);setBusy(false);setConnecting(false)}else setChallenge(null)
      if(name==='logout'){client.removeQueries({queryKey:['model-provider','openai','models',...(scope?[scope.connection.id]:[])]});setDisconnect(false)}
      await refresh()
    } catch(error){
      if(mounted.current&&turn===generation.current){
        // A repaired runtime may find the account already signed in. Refresh
        // that state rather than asking the user to disconnect and sign in again.
        if(error instanceof ProviderError&&error.code==='already-connected')await refresh()
        else setProblem(error instanceof Error?error.message:msg('The ChatGPT connection could not complete this request. Try again.'))
      }
    }
    finally {if(mounted.current&&turn===generation.current){setBusy(false);setConnecting(false)}}
  }
  const cancelConnection=()=>{
    generation.current++;operation.current?.abort();setBusy(false);setConnecting(false);setProblem('');void refresh()
  }
  const needsRuntime=status.data?.runtime==='not-installed'
  const canConnect=status.data?.runtime==='available'||needsRuntime
  const models=listing.data?.models??[]
  const chosen=models.find(item=>item.id===model)
  const valid=allowed.length===0 ? model==='' : allowed.length<=128 && allowed.every(id=>models.some(row=>row.id===id)) && allowed.includes(model) && !!chosen && (!effort || chosen.efforts.includes(effort as NonNullable<AssistantAgentConfig['effort']>))
  const pending=status.data?.account==='login-pending' || !!challenge
  const agent:AssistantAgentConfig={provider:'openai',authMethod:'subscription',model:model||null,models:allowed,tools,...(effort?{effort:effort as AssistantAgentConfig['effort']}:{})}
  const stored=JSON.stringify(allowed)===JSON.stringify(allowedAgentModels(config.assistant.agent)) && config.assistant.engine==='codex' && config.assistant.agent?.model===model && (config.assistant.agent.effort??'')===effort && JSON.stringify(config.assistant.agent.tools)===JSON.stringify(tools)
  useEffect(()=>{if(dirty&&stored)setDirty(false)},[dirty,stored])
  const canReadSchema=config.assistant.agent?.tools.includes('get_schema')===true
  const executionAgent=applyAssistantProfile(config.assistant,assistantProfile).agent
  const test=useAssistantRun({endpoint:null,agent:executionAgent?{...executionAgent,tools:canReadSchema?['get_schema']:[]}:undefined,engine:'codex',model:executionAgent?.model??'',thinking:'off',purpose:'test-design'})
  const tested=test.status==='finished' && !test.failure && !test.events.some(event=>event.type==='error') && test.events.some(event=>event.type==='tool_result'&&event.name==='get_schema'&&!event.isError)
  const testRunning=test.status==='running'
  const save=()=>{
    if (!desk || desk.sha256===undefined || !valid || unavailable)return
    setProblem('');setSaved(false)
    write.mutate({assistant:{endpoint:config.assistant.endpoint,thinking:config.assistant.thinking,engine:'codex',agent},ifMatch:desk.sha256},{onSuccess:()=>{setDirty(false);setSaved(true)},onError:error=>setProblem(error.message)})
  }
  const edit=()=>{setDirty(true);setSaved(false);test.stop()}
  return <section aria-label={msg('ChatGPT subscription')} className={styles.subscription}>
    <div><h3>{msg('OpenAI / ChatGPT')}</h3><p className="quiet">{msg('Use your ChatGPT account through Codex. No API key is required.')}</p></div>
    {catalog.isPending && <p role="status">{msg('Checking the ChatGPT connection…')}</p>}
    {catalog.error && <p role="alert">{msg('Assistant connections could not be loaded.')} <Button variant="inline" onClick={()=>void catalog.refetch()}>{msg('Retry')}</Button></p>}
    {provider && !provider.enabled && <p className="quiet">{provider.availability==='unsupported'?msg('ChatGPT subscriptions are not yet supported on this computer.'):provider.availability==='disabled'?msg('ChatGPT subscriptions are disabled by this installation’s administrator.'):msg('The ChatGPT connection is unavailable. Check Desk’s installation.')}</p>}
    {provider?.enabled && <>
      <div className={styles.connection}>
        <RunStatus running={connecting}>{connecting?needsRuntime?msg('Preparing ChatGPT…'):msg('Connecting to ChatGPT…'):connected?msg('Connected to ChatGPT'):pending?msg('Waiting for sign-in…'):status.isPending||(statusUnknown&&status.isFetching)?msg('Checking the ChatGPT connection…'):statusUnknown?msg('Connection unavailable'):msg('Not connected')}</RunStatus>
        {connecting?<Button onClick={cancelConnection}>{msg('Cancel')}</Button>:connected?<Button ref={disconnectButton} disabled={busy} onClick={()=>setDisconnect(true)}>{msg('Disconnect')}</Button>:pending?(challenge?.id||status.data?.login?.id)?<Button disabled={busy} onClick={()=>void action('cancel')}>{msg('Cancel sign-in')}</Button>:<Button disabled={busy} onClick={()=>void status.refetch()}>{msg('Refresh')}</Button>:statusUnknown?<Button disabled={status.isFetching} onClick={()=>void status.refetch()}>{msg('Check again')}</Button>:<Button disabled={busy||unavailable||!provider.engineReady||!canConnect} onClick={()=>void action('login')}>{msg('Connect ChatGPT')}</Button>}
      </div>
      {needsRuntime&&!connecting&&!pending&&<p className="quiet">{msg('Desk prepares the connection automatically the first time you connect.')}</p>}
      {!connected&&!pending&&!connecting&&!statusUnknown&&<Field label={msg('Sign-in method')}>{wiring=><Select {...wiring} value={method} disabled={busy} onValueChange={setMethod} options={provider.loginMethods.map(value=>({value,label:value==='device'?msg('Device code'):msg('Browser')}))}/>}</Field>}
      {status.error && !busy && <p className="quiet">{msg(status.error.message)} {!statusUnknown&&<Button variant="inline" onClick={()=>void status.refetch()}>{msg('Check again')}</Button>}</p>}
      {challenge && <div className={styles.challenge}>
        <a href={challenge.url} target="_blank" rel="noopener noreferrer">{msg('Continue sign-in in your browser')}</a>
        {challenge.code&&<p>{msg('Enter this code:')} <code>{challenge.code}</code></p>}
        <p className="quiet">{msg('Return here after signing in. This page will update automatically.')}</p>
      </div>}
      {connected&&<FieldGroup>
        <SubscriptionModels label={scope?msg('Default models'):undefined} models={models} allowed={allowed} model={model} disabled={busy||testRunning||unavailable||listing.isPending||listing.isError}
          onChange={(next,nextModel)=>{edit();setAllowed(next);if(model!==nextModel)setEffort('');setModel(nextModel)}}/>
        {listing.isPending&&<p className="quiet" role="status">{msg('Loading models…')}</p>}
        {allowed.some(id=>!models.some(row=>row.id===id))&&!listing.isPending&&<p role="alert">{msg('This model is no longer available. Choose another model.')}</p>}
        {listing.error&&<p role="alert">{msg(listing.error.message)} <Button variant="inline" onClick={()=>void listing.refetch()}>{msg('Retry')}</Button></p>}
        {chosen&&<Field label={msg('Reasoning effort')} hint={msg('Controls how much the model reasons before answering. Chat and Research control which sources it can use.')}>
          {wiring=><Select {...wiring} value={effort||'__default__'} disabled={busy||testRunning||unavailable||write.isPending} onValueChange={value=>{edit();setEffort(value==='__default__'?'':value)}} options={[{value:'__default__',label:msg('Model default')},...chosen.efforts.map(value=>({value,label:value}))]}/>}
        </Field>}
        <Disclosure title={msg('Allowed pack tools')}><div className={styles.tools}>{ASSISTANT_TOOLS.map(tool=><label key={tool}><input type="checkbox" checked={tools.includes(tool)} disabled={busy||testRunning||unavailable||write.isPending} onChange={event=>{edit();setTools(event.target.checked?[...tools,tool]:tools.filter(value=>value!==tool))}}/><code>{tool}</code></label>)}</div><p className="quiet">{msg('Tool access follows the saved Desk configuration.')}</p></Disclosure>
        <div className={styles.actions}>
          <Button disabled={unavailable||!!assistantProfile?.problem||!executionAgent?.model||!canReadSchema||!stored||!model||dirty||!valid||busy||testRunning} onClick={()=>test.start('Connection check only. Call get_schema once to read the Judgment Pack schema, then reply with one short confirmation. Do not propose changes or test cases.')}>{msg('Test connection')}</Button>
          {dirty&&<Button variant="quiet" disabled={busy||testRunning||write.isPending} onClick={()=>{setDirty(false);setProblem('');setSaved(false);write.reset()}}>{msg('Cancel')}</Button>}
          {testRunning&&<Button variant="quiet" onClick={test.stop}>{msg('Stop')}</Button>}
          <Button variant="primary" disabled={stored||!valid||!provider.engineReady||unavailable||write.isPending||busy||testRunning} onClick={save}>{write.isPending?msg('Saving…'):msg('Save shared settings')}</Button>
        </div>
        {!canReadSchema&&<p className="quiet">{msg('Enable and save get_schema in Allowed pack tools to test the connection.')}</p>}
        {testRunning&&<p role="status">{msg('Testing connection…')}</p>}
        {test.status==='finished'&&stored&&!dirty&&<p role="status">{tested?msg('Connected. Codex read the pack schema through Desk.'):msg('The connection check did not complete. Check the connection and try again.')}</p>}
      </FieldGroup>}
    </>}
    {problem&&<p role="alert">{msg(problem)}</p>}
    {saved&&<p role="status">{msg('Assistant settings saved.')}</p>}
    <Dialog open={disconnect} onOpenChange={setDisconnect} openerRef={disconnectButton} title={msg('Disconnect ChatGPT?')} description={msg('This stops active Codex work and signs out the account stored by this Desk.')}>
      <DialogActions><Button disabled={busy} onClick={()=>setDisconnect(false)}>{msg('Cancel')}</Button><Button variant="danger" disabled={busy} onClick={()=>void action('logout')}>{msg('Disconnect')}</Button></DialogActions>
    </Dialog>
  </section>
}

export function AssistantSettings({unavailable,onDirtyChange}:{unavailable:boolean;onDirtyChange?:(dirty:boolean)=>void}) {
 return <DeskAISettings unavailable={unavailable} onDirtyChange={onDirtyChange}/>
}

/** Shared credentials and defaults have one home in Connections > AI. */
export function SharedAssistantSettings({unavailable,onDirtyChange}:{unavailable:boolean;onDirtyChange?:(dirty:boolean)=>void}) {
 const effective=useEffectiveConfig(),machine=effective.machineAssistant??effective.config.assistant
 if(effective.aiConnections)return <AIConnectionsSettings unavailable={unavailable} onDirtyChange={onDirtyChange}/>
 return <DeskConfigFixture value={{...effective,config:{...effective.config,assistant:machine}}}>
  <MachineAssistantSettings unavailable={unavailable} onDirtyChange={onDirtyChange}/>
 </DeskConfigFixture>
}

export function QuickAssistantSettings({unavailable,onDirtyChange}:{unavailable:boolean;onDirtyChange?:(dirty:boolean)=>void}) {
 const effective=useEffectiveConfig(),machine=effective.machineAssistant??effective.config.assistant
 const [profileDirty,setProfileDirty]=useState(false),[sharedDirty,setSharedDirty]=useState(false)
 useEffect(()=>onDirtyChange?.(profileDirty||sharedDirty),[profileDirty,sharedDirty,onDirtyChange])
 const configured=machine.engine==='codex'?!!machine.agent:!!machine.endpoint
 return <div className={styles.settings}>
  <DeskAISettings unavailable={unavailable||sharedDirty} onDirtyChange={setProfileDirty}/>
  <Disclosure className={styles.machine} open={!configured||undefined} title={msg('Shared AI settings')}>
   <p>{msg('Connections, credentials and machine defaults are shared across all desks on this computer.')}</p>
   <SharedAssistantSettings unavailable={unavailable||profileDirty} onDirtyChange={setSharedDirty}/>
  </Disclosure>
 </div>
}
