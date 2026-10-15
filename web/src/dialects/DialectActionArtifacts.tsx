import {privateQuery} from '../data/privateQuery'
import {LoadingState} from '../ui/LoadingState'
import {DialectProposalReview} from './DialectProposalReview'
import { QueryStatusNotice } from '../shell/QueryStatusNotice'
import {RunStatus} from '../ui/RunStatus'
import {dialectChatHref} from '../chat/navigation'
import {researchStateLabel} from './ResearchActivity'
import {useDialectRecipeEditing} from './recipeEditing'
import {useEffect,useMemo,useRef,useState,type ReactNode,type RefObject} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {msg} from '../i18n'
import {ChatAction} from '../chat/ChatActions'
import {ArtifactReference} from '../chat/ArtifactReference'
import {useChats} from '../chat/ChatProvider'
import {Button,ButtonLink} from '../ui/Button'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Field} from '../ui/Field'
import {Input} from '../ui/Input'
import {TextArea} from '../ui/TextArea'
import {Notice} from '../ui/Notice'
import {Disclosure} from '../ui/Disclosure'
import {useConfirmDiscard,useRegisteredChanges} from '../shell/UnsavedChanges'
import {ResearchRecipe} from './ResearchRecipe'
import {ResearchReview,ResearchProgress} from './ResearchRun'
import {ResearchActivityDialog} from './ResearchActivity'
import {ReviewRecords} from './ReviewRecords'
import {dialectAPI,type DialectRecipe,type PreparationReply} from './client'
import {applyDialectRecipeAction,type DialectChatAction} from './chatActions'
import {visibleResearchRun,type ResearchSummary,type ResearchRun} from './research'
import styles from './DialectsView.module.css'
import chatStyles from '../chat/ChatWorkspace.module.css'

export function dialectActionTitle(action:DialectChatAction){
 switch(action.kind){
  case 'update-dialect':return msg('Proposed dialect')
  case 'update-recipe':return msg('Proposed recipe update')
  case 'remove-recipe':return msg('Remove recipe setup')
  case 'research':return msg('Research records')
  case 'reprocess':return msg('Reprocess selected records')
  case 'stop-research':return msg('Stop research')
  case 'evaluate':return msg('Evaluate selected records')
 }
}
export function dialectActionOutcome(action:DialectChatAction){
 return action.kind==='update-dialect'?msg('Dialect saved'):action.kind==='reprocess'?msg('Reprocessing started'):action.kind==='research'?msg('Research started'):action.kind==='evaluate'?msg('Evaluation submitted'):action.kind==='stop-research'?msg('Stop requested'):action.kind==='remove-recipe'?msg('Setup removed'):msg('Recipe saved')
}
export function DialectActionReference({chatId,action,disabled=false,presentation='card'}:{chatId:string;action:DialectChatAction;disabled?:boolean;presentation?:'card'|'action'}){
 const editing=useDialectRecipeEditing(action.inputId)
 const [open,setOpen]=useState(false),opener=useRef<HTMLButtonElement|null>(null)
 const reviewLabel=action.kind==='update-dialect'?msg('Review dialect'):action.kind==='update-recipe'?msg('Review changes'):action.kind==='reprocess'?msg('Review reprocessing'):action.kind==='research'?msg('Review research'):msg('Review action')
 return <>{presentation==='action'&&!action.applied?<ChatAction disabled={disabled||editing} onClick={event=>{opener.current=event.currentTarget;setOpen(true)}}>{reviewLabel}</ChatAction>:<ArtifactReference compact title={dialectActionTitle(action)} description={<>{action.name} · {msg('Recipe version {{version}}',{version:action.recipeVersion})}{action.applied&&<> · {dialectActionOutcome(action)}</>}</>}
  action={action.applied?msg('Open dialect'):action.kind==='update-dialect'?msg('Review dialect'):action.kind==='update-recipe'?msg('Review changes'):action.kind==='reprocess'?msg('Review reprocessing'):action.kind==='research'?msg('Review research'):msg('Review action')} href={action.applied?dialectChatHref(action.inputId,chatId):undefined} disabled={disabled||editing}
  onOpen={()=>{opener.current=document.activeElement instanceof HTMLButtonElement?document.activeElement:null;setOpen(true)}}/>}
  {editing&&presentation==='card'&&<p className={chatStyles.caption}>{msg('Save or discard Recipe edits before reviewing this action.')}</p>}
  {open&&<DialectActionReview chatId={chatId} action={action} disabled={disabled} opener={opener} onClose={()=>setOpen(false)}/>}
 </>
}
export function DialectActionNotice({chatId,actions,disabled}:{chatId:string;actions:DialectChatAction[];disabled?:boolean}){
 const action=actions.at(-1)
 // Completed actions stay with their Input, not above the composer.
 if(action?.applied)return null
 return action?<DialectActionReference key={action.id} chatId={chatId} action={action} disabled={disabled}/>:null
}

function DialectActionReview({chatId,action,opener,onClose,disabled}:{chatId:string;action:DialectChatAction;opener:RefObject<HTMLButtonElement|null>;onClose:()=>void;disabled?:boolean}){
 const editing=useDialectRecipeEditing(action.inputId)
 disabled=disabled||editing
 const {store}=useChats(),cache=useQueryClient()
 const query=useQuery({...privateQuery,queryKey:['input-preparations',action.inputId],queryFn:()=>dialectAPI<PreparationReply>('/'+action.inputId),retry:false,staleTime:0,refetchOnMount:'always'})
 const [opened,setOpened]=useState(false)
 const saved=query.data,stale=!!saved&&saved.sha256!==action.revision
 const finish=(result?:{revision?:string;runId?:string})=>{
  const current=store?.getSnapshot().chats.find(chat=>chat.id===chatId)
  if(current)store?.update(chatId,{inputActions:current.inputActions?.map(item=>item.id===action.id?{...item,applied:{at:new Date().toISOString(),...result}}:item)})
  void cache.invalidateQueries({queryKey:['input-preparations']});void cache.invalidateQueries({queryKey:['input-research',action.inputId]});void cache.invalidateQueries({queryKey:['input-evaluations',action.inputId]})
 }
 // Once the existing research/evaluation review opens, it owns its frozen
 // snapshot and confirmation checks. A resulting background refresh must not
 // unmount it or silently replace its selection.
 const [frozen,setFrozen]=useState<PreparationReply>()
 const held=frozen??saved
 useEffect(()=>{
  if(!frozen&&saved&&!query.isFetching&&!query.error&&!stale&&!disabled){setFrozen(saved);setOpened(true)}
 },[frozen,saved,query.isFetching,query.error,stale,disabled])
 if(opened&&held&&(action.kind==='research'||action.kind==='reprocess'))return <ResearchReview recordIds={action.kind==='reprocess'?action.recordIds:undefined} saved={held} opener={opener} onClose={onClose} onStarted={run=>finish({runId:run.id})}/>
 if(opened&&held&&action.kind==='evaluate')return <ReviewRecords saved={saved??held} records={held.document.records.filter(row=>action.recordIds?.includes(row.id))} opener={opener} onClose={onClose} onEvaluated={()=>finish()}/>
 if(frozen&&action.kind==='update-dialect')return <DialectProposalReview saved={frozen} action={action} blocked={stale||disabled||!!query.error} opener={opener} onClose={onClose} onSaved={next=>{cache.setQueryData(['input-preparations',action.inputId],next);finish({revision:next.sha256})}}/>
 if(frozen&&(action.kind==='update-recipe'||action.kind==='remove-recipe'))return <RecipeActionReview saved={frozen} blocked={stale||disabled||!!query.error} action={action} opener={opener} onClose={onClose} onSaved={next=>{cache.setQueryData(['input-preparations',action.inputId],next);finish({revision:next.sha256})}}/>
 return <Dialog open title={dialectActionTitle(action)} openerRef={opener} onOpenChange={open=>{if(!open)onClose()}} footer={<DialogActions><Button onClick={onClose}>{msg('Close')}</Button></DialogActions>}><div className={styles.stack}>
  {query.isFetching&&<LoadingState>{msg('Loading…')}</LoadingState>}
  {query.error&&<Notice tone="warning" actions={<Button onClick={()=>void query.refetch()}>{msg('Retry')}</Button>}>{query.error.message}</Notice>}
  {stale&&<Notice tone="warning">{msg('Dialects changed. Request a new proposal against the current recipe.')}</Notice>}
  {disabled&&<p className={styles.note}>{msg('Save or discard Recipe edits before reviewing this action.')}</p>}
  {!stale&&!disabled&&saved&&<><p>{action.name} · {msg('Recipe version {{version}}',{version:action.recipeVersion})}</p>{action.kind==='research'&&<p className={styles.note}>{msg('Review the connection and limits before starting. Saving a recipe does not start research.')}</p>}{action.kind==='evaluate'&&<p>{msg('{{count}} selected',{count:action.recordIds?.length??0})}</p>}{action.kind==='stop-research'&&<DialectChatProgress inputId={action.inputId} runId={action.runId} onStopped={()=>finish()}/>}</>}
  {(stale||disabled)&&<ButtonLink to={'/dialects/'+action.inputId+'?tab=recipe'}>{msg('Open recipe')}</ButtonLink>}
 </div></Dialog>
}
function RecipeActionReview({saved,action,opener,onClose,onSaved,blocked}:{blocked?:boolean;saved:PreparationReply;action:DialectChatAction;opener:RefObject<HTMLButtonElement|null>;onClose:()=>void;onSaved:(next:PreparationReply)=>void}){
 const initial=useMemo(()=>applyDialectRecipeAction(saved,action),[action.id])
 const [name,setName]=useState(initial.name),[recipe,setRecipe]=useState<DialectRecipe>(initial.recipe),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const confirm=useConfirmDiscard(),inFlight=useRef(false),removing=action.kind==='remove-recipe'
 const dirty=name!==initial.name||JSON.stringify(recipe)!==JSON.stringify(initial.recipe)
 const question=msg('Discard changes to this recipe review? The original proposal stays in chat.')
 const clear=useRegisteredChanges(dirty,question,{busy})
 const close=async()=>{if(!inFlight.current&&(!dirty||await confirm(question))){clear();onClose()}}
 const save=async()=>{
  if(inFlight.current||blocked)return;inFlight.current=true;setBusy(true);setError('')
  try{const next=await dialectAPI<PreparationReply>('/'+action.inputId,'PUT',{name,recipe},action.revision);onSaved(next);clear();onClose()}catch(e){setError((e as Error).message)}finally{inFlight.current=false;setBusy(false)}
 }
 const ready=!recipe.research||!!recipe.research.ai.connection&&!!recipe.research.ai.revision&&!!recipe.research.ai.model&&recipe.research.fields.length>0
 const changes=recipeChanges(saved,initial)
 return <Dialog open title={dialectActionTitle(action)} size="form" openerRef={opener} onOpenChange={open=>{if(!open)void close()}} footer={<div className={styles.stack}>{error&&<Notice tone="warning">{error}</Notice>}<DialogActions><Button disabled={busy} onClick={()=>void close()}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy||blocked||!name.trim()||!ready} onClick={()=>void save()}>{busy?msg('Saving…'):removing?msg('Remove setup'):msg('Save recipe')}</Button></DialogActions></div>}><div className={styles.stack}>
  {blocked&&<Notice tone="warning">{msg('Dialects changed or Recipe has unsaved edits. Your proposed changes remain here; close this review and request a fresh proposal before saving.')}</Notice>}<p className={styles.note}>{action.name} · {msg('Recipe version {{version}}',{version:action.recipeVersion})}</p>
  {removing?<><p>{action.section==='all'?msg('Remove preparation instructions, research and qualification settings?'):action.section==='research'?msg('Remove the research setup from this recipe?'):msg('Remove the qualification setup from this recipe?')}</p><p className={styles.note}>{msg('Existing records and earlier recipe versions will remain available. No run will be started or stopped.')}</p></>:<>
   <ul>{changes.map((change,i)=><li key={i}>{change}</li>)}</ul>
   <Field label={msg('Name')}>{props=><Input {...props} value={name} disabled={busy} maxLength={160} onChange={event=>setName(event.target.value)}/>}</Field>
   <Field label={msg('What records do you need?')}>{props=><TextArea {...props} value={recipe.intent} disabled={busy} rows={4} maxLength={12000} onChange={event=>setRecipe({...recipe,intent:event.target.value})}/>}</Field>
   <ResearchRecipe value={recipe.research} records={saved.document.records} disabled={busy} onChange={research=>setRecipe({...recipe,research})}/>
   <Disclosure title={msg('Previous instructions')}><p className={styles.reviewNote}>{saved.document.recipe.intent}</p></Disclosure>
   <p className={styles.note}>{msg('Saving does not start research or change qualification rules. Existing records remain available.')}</p>
  </>}
 </div></Dialog>
}
function recipeChanges(saved:PreparationReply,next:{name:string;recipe:DialectRecipe}){
 const before=saved.document.recipe.research,after=next.recipe.research,changes:string[]=[]
 if(next.name!==saved.document.name)changes.push(msg('Name')+': '+saved.document.name+' → '+next.name)
 if(next.recipe.intent!==saved.document.recipe.intent)changes.push(msg('Preparation instructions updated'))
 for(const field of after?.fields??[]){const prior=before?.fields.find(item=>item.name===field.name);if(JSON.stringify(prior)!==JSON.stringify(field))changes.push((prior?msg('Updated field'):msg('Added field'))+': '+field.name)}
 for(const field of before?.fields??[])if(!after?.fields.some(item=>item.name===field.name))changes.push(msg('Removed field')+': '+field.name)
 if(JSON.stringify(before?.limits)!==JSON.stringify(after?.limits))changes.push(msg('Research limits updated'))
 if(JSON.stringify(before?.criteria)!==JSON.stringify(after?.criteria))changes.push(msg('Research criteria updated'))
 if(before?.target!==after?.target)changes.push(msg('Research target updated'))
 return changes
}
export function DialectChatProgress({inputId,runId,onStopped,emptyFallback,presentation='full'}:{inputId?:string;runId?:string;onStopped?:()=>void;emptyFallback?:ReactNode;presentation?:'full'|'compact'|'hidden'}){
 const cache=useQueryClient(),enabled=!!inputId&&inputId!=='list'
 const query=useQuery({queryKey:['input-research',inputId],enabled,queryFn:()=>dialectAPI<{items:ResearchSummary[]}>('/'+inputId+'/research'),refetchInterval:2000,retry:false})
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),[steps,setSteps]=useState<string>(),opener=useRef<HTMLButtonElement|null>(null)
 const run=runId?query.data?.items.find(item=>item.id===runId):visibleResearchRun(query.data?.items??[])
 const stop=async()=>{if(!run||busy)return;setBusy(true);setError('');try{await dialectAPI<ResearchRun>('/'+inputId+'/research/'+run.id+'/stop','POST',{});onStopped?.();await cache.invalidateQueries({queryKey:['input-research',inputId]})}catch(e){setError((e as Error).message)}finally{setBusy(false)}}
 // Use the same query for progress and its empty state, including work started
 // outside chat. Wait for the first result so existing activity never flashes a welcome.
 if(!enabled)return emptyFallback??null
 if(!run&&!query.error&&!error)return query.isPending?null:emptyFallback??null
 // Keep the query for welcome suppression, even when the page owns the status.
 const notice=<QueryStatusNotice id={'input-research:'+inputId} label={msg('Research status unavailable')} error={query.error} retrying={query.isFetching} onRetry={()=>query.refetch()}>{msg('Research status could not be refreshed. The last known status is shown; the run may still be active.')}</QueryStatusNotice>
 if(presentation==='hidden'||!run&&!error)return notice
 if(presentation==='compact'){
  const live=run?.state==='queued'||run?.state==='running'
  const label=run?.stopRequested&&live?msg('Stopping research…'):run?researchStateLabel(run.state):''
  return <>{notice}<div className={chatStyles.inputResearchLink} data-attention={!!query.error||run&&['paused','blocked','interrupted','partial','completed-with-gaps'].includes(run.state)||undefined}>
   <RunStatus running={live&&!run?.stopRequested}><span>{label}{run&&<> · {msg('{{count}} records saved',{count:run.recordCount})}</>}</span></RunStatus>
   {live&&<Button variant="quiet" disabled={busy||run?.stopRequested} onClick={()=>void stop()}>{busy||run?.stopRequested?msg('Stopping…'):msg('Stop')}</Button>}
   {error&&<Notice tone="warning">{error}</Notice>}
  </div></>
 }

 return <div className={styles.stack}>{notice}{run&&<ResearchProgress preparationId={inputId} run={run} onStop={()=>void stop()} busy={busy} onSteps={button=>{opener.current=button;setSteps(run.id)}}/>}{error&&<Notice tone="warning">{error}</Notice>}{steps&&<ResearchActivityDialog preparationId={inputId!} runId={steps} opener={opener} onClose={()=>setSteps(undefined)}/>}</div>
}
