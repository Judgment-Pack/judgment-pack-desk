import {ChatAction} from '../chat/ChatActions'
import {dialectListQuery} from './queries'
import {LoadingState} from '../ui/LoadingState'
import {useRef,useState} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {msg,formatDate} from '../i18n'
import {Button} from '../ui/Button'
import {Menu} from '../ui/Menu'
import {Select} from '../ui/Select'
import {Field} from '../ui/Field'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Notice} from '../ui/Notice'
import {Tooltip} from '../ui/Tooltip'
import {IconCheck,IconChevronDown} from '../shell/icons'
import {dialectAPI,type PreparationReply} from './client'
import {validDialectId} from './chatActions'
import {useDialectRecipeEditing} from './recipeEditing'
import {approvalKey,researchApprovalAPI,useResearchApproval,type ResearchApprovalStatus} from './researchApproval'
import {ResearchReview} from './ResearchRun'
import styles from './DialectsView.module.css'

/** Shared main/pane composer control. Only the server's live allowance selects
 * Auto. A stored chat preference, model output or restored message cannot. */
export function ResearchApprovalControl({chatId,inputId,disabled=false}:{chatId:string;inputId?:string;disabled?:boolean}){
 const status=useResearchApproval(chatId),cache=useQueryClient(),opener=useRef<HTMLButtonElement>(null),inFlight=useRef(false)
 const [setup,setSetup]=useState(false),[details,setDetails]=useState(false),[saved,setSaved]=useState<PreparationReply>(),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const editing=useDialectRecipeEditing(inputId??''),grant=status.data?.grant,auto=status.data?.mode==='auto'&&!status.isError
 const available=!!status.data&&!status.isError
 const refresh=()=>cache.invalidateQueries({queryKey:approvalKey(chatId)})
 const ask=async()=>{
  if(inFlight.current)return
  if(!grant||grant.state==='used'||grant.state==='revoked'){setDetails(false);return}
  inFlight.current=true;setBusy(true);setError('')
  try{const updated=await researchApprovalAPI<ResearchApprovalStatus>(chatId,'/revoke',{runId:grant.runId});cache.setQueryData(approvalKey(chatId),updated);setDetails(false);await refresh()}catch(e){setError((e as Error).message);void refresh()}finally{inFlight.current=false;setBusy(false)}
 }
 const review=async()=>{
  if(disabled||editing||busy)return
  setError('')
  if(grant&&(grant.state==='ready'||grant.state==='stale')){setDetails(true);return}
  if(!validDialectId(inputId)){setSetup(true);return}
  inFlight.current=true;setBusy(true)
  try{setSaved(await dialectAPI<PreparationReply>('/'+inputId))}catch(e){setError((e as Error).message)}finally{inFlight.current=false;setBusy(false)}
 }
 return <>
  <Menu.Root><Tooltip content={status.isError?msg('Approvals unavailable'):auto?msg('Approvals: Auto within limits · {{input}}',{input:grant?.name??''}):msg('Approvals: Ask me')}><Menu.Trigger asChild><ChatAction ref={opener} variant="quiet" aria-label={msg('Approvals')} disabled={disabled||busy||status.isPending}>{status.isPending?<LoadingState inline>{msg('Checking…')}</LoadingState>:status.isError?msg('Approvals unavailable'):auto?msg('Auto'):msg('Ask me')}<IconChevronDown/></ChatAction></Menu.Trigger></Tooltip>
   <Menu.Portal><Menu.Content align="start" onCloseAutoFocus={event=>{if(setup||saved||details)event.preventDefault()}}>
    <Menu.Label>{msg('Workflow approvals')}</Menu.Label>
    <Menu.Item icon={available&&!auto?<IconCheck/>:undefined} disabled={!available||busy} description={msg('Review a workflow before it starts.')} onSelect={()=>void ask()}>{msg('Ask me')}</Menu.Item>
    <Menu.Item icon={auto?<IconCheck/>:undefined} disabled={!available||busy||editing} description={editing?msg('Save or discard Recipe edits first.'):msg('Allow one research run with reviewed limits.')} onSelect={()=>void review()}>{msg('Auto within limits…')}</Menu.Item>
    {status.isError&&<Menu.Item onSelect={()=>void status.refetch()}>{msg('Reload approvals')}</Menu.Item>}
   </Menu.Content></Menu.Portal>
  </Menu.Root>
  {error&&!details&&<Notice tone="warning">{error}</Notice>}
  {setup&&<ChooseResearchInput opener={opener} onClose={()=>setSetup(false)} onSelect={value=>{setSetup(false);setSaved(value)}}/>}
  {saved&&<ResearchReview saved={saved} opener={opener} onClose={()=>setSaved(undefined)} onStarted={()=>{}} authorization={{chatId,onAuthorized:()=>{void refresh()}}}/>}
  {details&&grant&&<Dialog open title={msg('Research allowance')} openerRef={opener} onOpenChange={open=>{if(!open&&!busy)setDetails(false)}} footer={<DialogActions><Button disabled={busy} onClick={()=>setDetails(false)}>{msg('Close')}</Button><Button disabled={busy} onClick={()=>void ask()}>{busy?msg('Updating…'):msg('Switch to Ask me')}</Button></DialogActions>}><div className={styles.stack}>
   <p>{grant.name} · {msg('Recipe version {{version}}',{version:grant.recipeVersion})}</p>
   <p className={styles.note}>{grant.state==='stale'?msg('Dialects changed. Revoke this allowance and review the current recipe before enabling Auto again.'):msg('One research run may start in this chat before {{time}}.',{time:formatDate(new Date(grant.expiresAt),{hour:'numeric',minute:'2-digit'})})}</p>
   <p className={styles.note}>{msg('Up to {{records}} records, {{searches}} searches, {{pages}} page reads and {{models}} model requests.',{records:grant.limits.records,searches:grant.limits.searches,pages:grant.limits.pages,models:grant.limits.modelCalls})}</p>
   <p className={styles.note}>{msg('This allowance belongs to this signed-in window. Recipe changes, evaluation and additional usage still need review.')}</p>
   {error&&<Notice tone="warning">{error}</Notice>}
  </div></Dialog>}
 </>
}
function ChooseResearchInput({opener,onClose,onSelect}:{opener:React.RefObject<HTMLButtonElement|null>;onClose:()=>void;onSelect:(saved:PreparationReply)=>void}){
 const inputs=useQuery({...dialectListQuery(),retry:false})
 const [id,setId]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const editing=useDialectRecipeEditing(id)
 const choose=async()=>{if(!id||busy||editing)return;setBusy(true);setError('');try{onSelect(await dialectAPI<PreparationReply>('/'+id))}catch(e){setError((e as Error).message)}finally{setBusy(false)}}
 return <Dialog open title={msg('Choose research input')} openerRef={opener} onOpenChange={open=>{if(!open&&!busy)onClose()}} footer={<DialogActions><Button disabled={busy} onClick={onClose}>{msg('Cancel')}</Button><Button variant="primary" disabled={!id||busy||editing||!!inputs.error} onClick={()=>void choose()}>{busy?<LoadingState inline>{msg('Loading…')}</LoadingState>:msg('Review allowance')}</Button></DialogActions>}><div className={styles.stack}>
  <p className={styles.note}>{msg('Choose a saved recipe to allow one research run. Enabling Auto does not start it.')}</p>
  <Field label={msg('Dialect')}>{props=><Select {...props} placeholder={msg('Choose a dialect')} value={id} disabled={busy||inputs.isPending} options={(inputs.data?.items??[]).map(item=>({value:item.id,label:item.name}))} onValueChange={setId}/>}</Field>
  {inputs.data&&!inputs.data.items.length&&<p className={styles.note}>{msg('Save an Dialects recipe before enabling Auto.')}</p>}
  {editing&&<p className={styles.note}>{msg('Save or discard Recipe edits first.')}</p>}
  {(inputs.error||error)&&<Notice tone="warning">{error||inputs.error?.message}</Notice>}
 </div></Dialog>
}
