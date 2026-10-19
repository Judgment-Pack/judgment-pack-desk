import {artifactImpactLabel} from '../../artifacts/ArtifactImpactFields'
import {useRef,useState} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {ChatAction} from '../../chat/ChatActions'
import {Menu} from '../../ui/Menu'
import {Dialog,DialogActions} from '../../ui/Dialog'
import {Notice} from '../../ui/Notice'
import {LoadingState} from '../../ui/LoadingState'
import {IconCheck,IconChevronDown} from '../../shell/icons'
import {msg} from '../../i18n'
import {useActionApproval,actionApprovalAPI,actionApprovalKey,type ActionApproval} from './client'
import {useDialectRecipeEditing,dialectRecipeIsEditing} from '../../dialects/recipeEditing'
import {publicationPath} from '../../dialects/publicationProposal'
import type {DialectChatAction} from '../../dialects/chatActions'
export function DialectPublicationApprovalControl({chatId,action,disabled,checkpoint}:{chatId:string;action:DialectChatAction;disabled:boolean;checkpoint:()=>Promise<void>}){
 const status=useActionApproval(chatId),cache=useQueryClient(),opener=useRef<HTMLButtonElement>(null),flight=useRef(false),editing=useDialectRecipeEditing(action.inputId)
 const [review,setReview]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const grant=status.data?.grant,p=action.publication!,auto=status.data?.mode==='auto'&&grant?.action==='dialect-publish'&&grant.target===action.id&&grant.path===publicationPath(action)&&!status.isError
 const change=async(kind:'grant'|'revoke')=>{
  if(flight.current||disabled||kind==='grant'&&dialectRecipeIsEditing(action.inputId))return
  flight.current=true;setBusy(true);setError('')
  try{
   if(kind==='grant'){
    await checkpoint()
    if(dialectRecipeIsEditing(action.inputId))throw Error(msg('Save or discard Recipe edits before reviewing this action.'))
    cache.setQueryData(actionApprovalKey(chatId),await actionApprovalAPI<ActionApproval>(chatId,'grant',{action:'dialect-publish',target:action.id,revision:action.revision}))
   }else if(grant){cache.setQueryData(actionApprovalKey(chatId),await actionApprovalAPI<ActionApproval>(chatId,'revoke',{id:grant.id}))}
   setReview(false)
  }catch(e){setError((e as Error).message)}finally{flight.current=false;setBusy(false)}
 }
 return <><Menu.Root><Menu.Trigger asChild><ChatAction ref={opener} variant="quiet" aria-label={msg('Publication approvals')} disabled={disabled||busy||status.isPending}>{status.isPending?<LoadingState inline>{msg('Checking…')}</LoadingState>:status.isError?msg('Approvals unavailable'):auto?msg('Auto'):msg('Ask me')}<IconChevronDown/></ChatAction></Menu.Trigger>
 <Menu.Portal><Menu.Content align="start"><Menu.Label>{msg('Dialect publication approvals')}</Menu.Label>
 <Menu.Item icon={!auto?<IconCheck/>:undefined} description={msg('Review and save each proposal in chat.')} onSelect={()=>void change('revoke')}>{msg('Ask me')}</Menu.Item>
 <Menu.Item icon={auto?<IconCheck/>:undefined} disabled={editing||status.isError} description={msg('Allow this exact checked version to be published once.')} onSelect={()=>{setError('');setReview(true)}}>{msg('Auto within limits…')}</Menu.Item>
 {status.isError&&<Menu.Item onSelect={()=>void status.refetch()}>{msg('Reload approvals')}</Menu.Item>}
 </Menu.Content></Menu.Portal></Menu.Root>
 <Dialog open={review} onOpenChange={open=>{if(!busy)setReview(open)}} openerRef={opener} title={msg('Allow one publication')} footer={<DialogActions><ChatAction disabled={busy} onClick={()=>setReview(false)}>{msg('Cancel')}</ChatAction><ChatAction disabled={busy||editing||disabled} onClick={()=>void change('grant')}>{busy?msg('Allowing…'):msg('Allow one publication')}</ChatAction></DialogActions>}>
 <p>{action.name} · {p.version} · {p.variant==='generated'?msg('Reviewed research'):msg('Imported records')}</p><p>{msg('Compatibility impact')}: {artifactImpactLabel(p.impact.classification)}</p><p>{p.impact.rationale}</p>
 <p>{msg('Allow this checked version once within 15 minutes. Changing the recipe, proposal or publication base invalidates the allowance. Plan mode blocks publication. Existing jobs keep their version.')}</p>
 <p>{msg('This allowance ends after use, expiry, sign-out or a Desk restart. Research and job changes require separate approval.')}</p>
 {error&&<Notice tone="warning">{error}</Notice>}</Dialog>{error&&!review&&<span role="status">{error}</span>}</>
}
