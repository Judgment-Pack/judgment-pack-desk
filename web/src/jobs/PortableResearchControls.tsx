import {Fragment,useEffect,useRef,useState} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {msg} from '../i18n'
import {privateQuery} from '../data/privateQuery'
import {useEffectiveConfig} from '../config/DeskConfigProvider'
import {AIConnectionPicker} from '../assistant/AIConnectionPicker'
import {resolveAIConnection} from '../assistant/aiConnections'
import {ResearchTaskSteps,researchStateLabel} from '../dialects/ResearchActivity'
import {ResearchBudgetHistory,ResearchBudgetTable} from '../dialects/ResearchBudget'
import type {ResearchLimits} from '../dialects/research'
import type {ResearchSource,ProfileBinding,BindingDocument} from '../dialect/types'
import type {ProfileEntry} from './dialectTypes'
import {Button} from '../ui/Button'
import {Field} from '../ui/Field'
import {Input} from '../ui/Input'
import {Select} from '../ui/Select'
import {Disclosure} from '../ui/Disclosure'
import {LoadingState} from '../ui/LoadingState'
import {Notice} from '../ui/Notice'
import {RunStatus} from '../ui/RunStatus'
import {IconPlay,IconStop,IconSearch} from '../shell/icons'
import {portableResearchAPI,registerResearchModel,type PortableResearchChoice,type PortableResearchReference} from './portableResearch'
import {SearchBindingControls} from './SearchBindingControls'
import {PageBindingControls} from './PageBindingControls'
import {PageProfileScope,frozenPagePin,pinnedProfile} from './PageProfileScope'
import styles from './JobsView.module.css'
import researchStyles from '../dialects/DialectsView.module.css'

export function PortableResearchControls({source,reference,currentContext,profiles,binding,disabled,allowBinding=true,onBind,onReview}:{
 source:ResearchSource;reference?:PortableResearchReference;currentContext?:string;profiles:ProfileEntry[];binding?:BindingDocument;disabled:boolean;allowBinding?:boolean
 onBind:(pin:ProfileBinding,capability:'answer'|'search'|'page')=>void
 onReview:(choice:PortableResearchChoice,limits:ResearchLimits)=>Promise<void>
}){
 const effective=useEffectiveConfig(),cache=useQueryClient()
 const [connection,setConnection]=useState<string>(),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const inflight=useRef(false)
 const [limits,setLimits]=useState<ResearchLimits>({records:1,modelCalls:8,searches:source.capabilities.includes('search')?3:0,pages:source.capabilities.includes('page')?3:0})
 const resolved=resolveAIConnection(effective,connection),modelConfig=resolved.assistant.engine==='codex'?resolved.assistant.agent:resolved.assistant.endpoint
 const supported=!!resolved.connection&&(modelConfig?.models??[modelConfig?.model]).includes(source.model)
 const choice:PortableResearchChoice|undefined=resolved.connection&&supported?{connection:resolved.connection.id,revision:resolved.connection.revision,model:source.model,...(resolved.assistant.engine==='codex'&&resolved.assistant.agent?.effort?{effort:resolved.assistant.agent.effort}:{})}:undefined
 const query=useQuery({queryKey:['portable-research',reference?.id],enabled:!!reference,...privateQuery,queryFn:({signal})=>portableResearchAPI('/'+reference!.id,undefined,undefined,signal),refetchInterval:q=>['running','queued'].includes(q.state.data?.state??'')?1500:10_000})
 const run=query.data,live=run?.state==='queued'||run?.state==='running',matches=!!reference&&!!currentContext&&reference.context===currentContext
 const actionDisabled=disabled||busy||!currentContext
 const frozenReview=!!run&&matches&&run.state==='review'
 const choiceLocked=!!live||frozenReview
 const pagePin=(binding?.sources[source.name]?.webProfiles??source.webProfiles)?.page
 const request=run?.activity.budgetRequest
 useEffect(()=>{setError('')},[reference?.id,currentContext])
 useEffect(()=>{if(run&&matches){setConnection(run.choice.connection);setLimits(run.limits)}},[run?.id,matches])
 async function perform(action:()=>Promise<void>){if(inflight.current)return;inflight.current=true;setBusy(true);setError('');try{await action()}catch(e){setError(e instanceof Error?e.message:msg('The request could not be completed.'))}finally{inflight.current=false;setBusy(false)}}
 const command=(operation:string,body:unknown={})=>perform(async()=>{
  if(!run)return
  const updated=await portableResearchAPI('/'+run.id+'/'+operation,body,run.revision)
  cache.setQueryData(['portable-research',run.id],updated)
 })
 return <section className={styles.readiness} aria-label={msg('Research · {{source}}',{source:source.name})}>
  <div className={styles.readinessHeading}><h3>{source.name}</h3>{run&&<RunStatus running={live}>{researchStateLabel(run.state)}</RunStatus>}</div>
  <p className={styles.note}>{msg('{{model}} · {{count}} requested fields',{model:source.model,count:source.fields.length})}</p>
  <Disclosure title={msg('Connection and allowance')}>
   <div className={styles.fields}>
    <AIConnectionPicker value={resolved.connection?.id} disabled={actionDisabled||choiceLocked} onValueChange={setConnection}/>
    {resolved.problem&&<p className={styles.note}>{resolved.problem}</p>}
    {!supported&&resolved.connection&&<p className={styles.note}>{msg('Choose a connection that provides the model declared by this dialect.')}</p>}
    <div className={styles.actions}><Button size="small" disabled={actionDisabled||choiceLocked||!allowBinding||!choice} onClick={()=>void perform(async()=>{if(!choice)return;const entry=await registerResearchModel(choice);await cache.invalidateQueries({queryKey:['job-input-profiles']});onBind({profile:entry.profile.id,profileDigest:entry.digest},'answer')})}>{msg('Bind selected connection')}</Button></div>
    {source.capabilities.includes('search')&&allowBinding&&<SearchBindingControls disabled={actionDisabled||choiceLocked} context={currentContext??''} onBind={pin=>onBind(pin,'search')}/>}
    {source.capabilities.includes('page')&&allowBinding&&<PageBindingControls disabled={actionDisabled||choiceLocked} context={JSON.stringify([currentContext??'',source.name,pagePin?.profile??'',pagePin?.profileDigest??''])} onBind={pin=>onBind(pin,'page')}/>}
    {source.capabilities.filter((c):c is 'search'|'page'=>c!=='answer').map(capability=><Fragment key={capability}><Field label={capability==='search'?msg('Search profile'):msg('Page profile')}>{props=><Select {...props} value={(binding?.sources[source.name]?.webProfiles??source.webProfiles)?.[capability]?.profile} disabled={actionDisabled||choiceLocked||!allowBinding} placeholder={msg('Choose source profile')} options={profiles.filter(p=>p.profile.class==='generated'&&p.profile.research?.capabilities.includes(capability)).map(p=>({value:p.profile.id,label:p.profile.id}))} onValueChange={id=>{const entry=profiles.find(p=>p.profile.id===id);if(entry)onBind({profile:id,profileDigest:entry.digest},capability)}}/>}</Field>{capability==='page'&&pagePin&&<PageProfileScope profile={pinnedProfile(profiles,pagePin)}/>}</Fragment>)}
    <div className={styles.formGrid}>{([{key:'modelCalls',label:msg('Model requests'),min:1,max:30},{key:'searches',label:msg('Searches'),min:0,max:15},{key:'pages',label:msg('Page reads'),min:0,max:15}] as const).filter(item=>item.key==='modelCalls'||source.capabilities.includes(item.key==='pages'?'page':'search')).map(item=><Field key={item.key} label={item.label}>{props=><Input {...props} type="number" min={item.min} max={item.max} value={limits[item.key]} disabled={actionDisabled||choiceLocked} onChange={e=>setLimits(v=>({...v,[item.key]:Number(e.target.value)}))}/>}</Field>)}</div>
   </div>
  </Disclosure>
  {reference&&!run&&query.isPending&&<LoadingState>{msg('Loading research…')}</LoadingState>}
  {query.error&&<Notice tone="warning" actions={<Button size="small" onClick={()=>void query.refetch()}>{msg('Retry')}</Button>}>{msg('Research status could not be refreshed. Previously loaded steps may be out of date.')}</Notice>}
  {reference&&!matches&&currentContext&&<p className={styles.note}>{msg('Research was reviewed for different inputs. Review research again.')}</p>}
  {run&&<>
   <p className={styles.note}>{run.plan.arguments.question}</p>
   <p className={styles.note}>{msg('Reviewed connection: {{connection}} · {{model}}',{connection:resolveAIConnection(effective,run.choice.connection).connection?.name??run.choice.connection,model:run.choice.model})}</p>
   {run.state==='review'&&<p className={styles.note}>{msg('Up to {{models}} model requests, {{searches}} searches and {{pages}} page reads.',{models:run.limits.modelCalls,searches:run.limits.searches,pages:run.limits.pages})}</p>}
   {run.plan.arguments.capabilities?.includes('page')&&<PageProfileScope profile={pinnedProfile(profiles,frozenPagePin(run.inputText,source.name))}/>}
   {run.activity.problem&&<Notice tone="warning">{run.activity.problem}</Notice>}
   {run.activity.steps.length>0&&<Disclosure title={msg('Work · {{count}} steps',{count:run.activity.steps.length})}><ol className={researchStyles.researchSteps} aria-label={msg('Research steps')}><ResearchTaskSteps steps={run.activity.steps} tasks={[]} live={!!live}/></ol></Disclosure>}
   <ResearchBudgetHistory requests={run.activity.budgetRequests?.filter(r=>r.state!=='pending')}/>
   {request&&<><ResearchBudgetTable request={request}/><p className={styles.note}>{msg('Additional requests may incur provider usage. More records are not guaranteed.')}</p></>}
  </>}
  <p className={styles.note}>{msg('Review reads preceding sources. AI research starts only after you run the reviewed request.')}</p>
  <div className={styles.actions}>
   {!live&&<Button size="small" disabled={actionDisabled||!choice} onClick={()=>void perform(()=>onReview(choice!,limits))}><IconSearch/>{msg('Review research')}</Button>}
   {run?.state==='review'&&<Button size="small" variant="primary" disabled={actionDisabled||!matches||!!query.error} onClick={()=>void command('confirm')}><IconPlay/>{msg('Run research')}</Button>}
   {request&&<><Button size="small" disabled={actionDisabled||!matches} onClick={()=>void command('budget-decision',{requestId:request.id,decision:'approve'})}>{msg('Approve and continue')}</Button><Button size="small" variant="quiet" disabled={busy} onClick={()=>void command('budget-decision',{requestId:request.id,decision:'decline'})}>{msg('Keep retained work')}</Button></>}
   {live&&<Button size="small" disabled={busy||run?.activity.stopRequested} onClick={()=>void command('stop')}><IconStop/>{msg('Stop')}</Button>}
   {run?.responseText&&matches&&<span className={styles.note}>{msg('Result retained. Read sources and preview to verify mapped values.')}</span>}
  </div>
  {error&&<Notice tone="warning">{error}</Notice>}
 </section>
}
