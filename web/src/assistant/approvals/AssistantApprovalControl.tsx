import {DialectPublicationApprovalControl} from './DialectPublicationApprovalControl'
import type {DialectChatAction} from '../../dialects/chatActions'
import {ChatAction} from '../../chat/ChatActions'
import {useRef,useState} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {useActionApproval,actionApprovalAPI,actionApprovalKey,type ActionApproval} from './client'
import {ResearchApprovalControl} from '../../dialects/ResearchApprovalControl'
import {readFile} from '../../files/client'
import {useFileContent} from '../../files/queries'
import {isPathDirty,useAuthorDirty} from '../../shell/authorBridge'
import {LoadingState} from '../../ui/LoadingState'
import {Tooltip} from '../../ui/Tooltip'
import {Notice} from '../../ui/Notice'
import {Button} from '../../ui/Button'
import {Menu} from '../../ui/Menu'
import {Dialog,DialogActions} from '../../ui/Dialog'
import {IconCheck,IconChevronDown} from '../../shell/icons'
import {msg} from '../../i18n'
/** One approval control, with explicit resource-specific capabilities. An Auto
 * allowance for research cannot authorize a pack save or another workflow. */
export function AssistantApprovalControl({chatId,pack,inputId,publication,disabled=false,checkpoint}:{chatId:string;pack?:{id:string;path:string};inputId?:string;publication?:DialectChatAction;disabled?:boolean;checkpoint:()=>Promise<void>}) {
 return publication?<DialectPublicationApprovalControl key={publication.id} chatId={chatId} action={publication} disabled={disabled} checkpoint={checkpoint}/>:pack?<ArtifactApprovalControl chatId={chatId} pack={pack} disabled={disabled} checkpoint={checkpoint}/>:<ResearchApprovalControl chatId={chatId} inputId={inputId} disabled={disabled}/>
}
function ArtifactApprovalControl({chatId,pack,disabled,checkpoint}:{chatId:string;pack:{id:string;path:string};disabled:boolean;checkpoint:()=>Promise<void>}){
 const status=useActionApproval(chatId),file=useFileContent(pack.path),cache=useQueryClient(),opener=useRef<HTMLButtonElement>(null),flight=useRef(false)
 const [review,setReview]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState('')
 useAuthorDirty()
 const grant=status.data?.grant,auto=status.data?.mode==='auto'&&grant?.action==='pack-update'&&grant.path===pack.path&&!status.isError
 const change=async(kind:'grant'|'revoke')=>{
  if(flight.current||disabled||(kind==='grant'&&isPathDirty(pack.path)))return
  flight.current=true;setBusy(true);setError('')
  try{
   if(kind==='grant'){
    await checkpoint()
    const current=await readFile(pack.path)
    if(current.sha256!==file.data?.sha256)throw Error(msg('The file changed. Review the allowance again.'))
    const next=await actionApprovalAPI<ActionApproval>(chatId,'grant',{action:'pack-update',revision:current.sha256})
    cache.setQueryData(actionApprovalKey(chatId),next)
   }else if(grant){cache.setQueryData(actionApprovalKey(chatId),await actionApprovalAPI<ActionApproval>(chatId,'revoke',{id:grant.id}))}
   setReview(false)
  }catch(e){setError((e as Error).message)}finally{flight.current=false;setBusy(false)}
 }
 return <><Menu.Root><Tooltip content={auto?msg('Approvals: Auto within limits · {{input}}',{input:pack.id}):msg('Approvals: Ask me')}><Menu.Trigger asChild><ChatAction ref={opener} variant="quiet" aria-label={msg('Approvals')} disabled={disabled||busy||status.isPending}>{status.isPending?<LoadingState inline>{msg('Checking…')}</LoadingState>:status.isError?msg('Approvals unavailable'):auto?msg('Auto'):msg('Ask me')}<IconChevronDown/></ChatAction></Menu.Trigger></Tooltip>
 <Menu.Portal><Menu.Content align="start">
  <Menu.Label>{msg('Pack update approvals')}</Menu.Label>
  <Menu.Item icon={!auto?<IconCheck/>:undefined} description={msg('Review and save each proposal in chat.')} onSelect={()=>void change('revoke')}>{msg('Ask me')}</Menu.Item>
  <Menu.Item icon={auto?<IconCheck/>:undefined} disabled={isPathDirty(pack.path)||!file.data||status.isError} description={isPathDirty(pack.path)?msg('Save or discard your editor changes before saving this proposal.'):msg('Allow one checked update to this saved pack.')} onSelect={()=>{setError('');setReview(true)}}>{msg('Auto within limits…')}</Menu.Item>
  {status.isError&&<Menu.Item onSelect={()=>void status.refetch()}>{msg('Reload approvals')}</Menu.Item>}
 </Menu.Content></Menu.Portal></Menu.Root>
 <Dialog open={review} onOpenChange={open=>{if(!busy)setReview(open)}} title={msg('Allow one pack update')} openerRef={opener} footer={<DialogActions><Button disabled={busy} onClick={()=>setReview(false)}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy||isPathDirty(pack.path)||!file.data} onClick={()=>void change('grant')}>{busy?msg('Allowing…'):msg('Allow one update')}</Button></DialogActions>}>
 <p>{pack.id}</p><p>{msg('Allow this chat to save one proposal to the current saved version within 15 minutes. Every saved case must have an expectation and pass. Open questions block automatic saving. Coverage suggestions remain available for review.')}</p>
 <p>{msg('Changing the file or saved suite invalidates this allowance. Plan mode, other files, research, Jobs and additional AI usage remain outside its scope. It ends after use, expiry, sign-out or a Desk restart.')}</p>
 {error&&<Notice tone="warning">{error}</Notice>}
 </Dialog>{error&&!review&&<span role="status">{error}</span>}</>
}
