import {useEffect,useRef} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {useAutomaticAction} from '../assistant/approvals/automatic'
import {useActionApproval,actionApprovalKey} from '../assistant/approvals/client'
import {publicationPath,publicationFingerprint} from '../dialects/publicationProposal'
import {savePublication} from '../dialects/savePublication'
import {useDialectRecipeEditing,dialectRecipeIsEditing} from '../dialects/recipeEditing'
import type {Chat,ChatStore} from './store'
import type {ResearchRunBinding} from '../research/useResearchRun'
import {msg} from '../i18n'
export function useDialectPublicationAutoSave({chat,store,binding,restored}:{chat:Chat;store:ChatStore;binding:ResearchRunBinding;restored:boolean}){
 const approval=useActionApproval(chat.id,!!chat.inputActions?.some(a=>a.kind==='publish-dialect'&&!a.applied)),cache=useQueryClient()
 const grant=approval.isError?undefined:approval.data?.grant
 const action=grant?.action==='dialect-publish'?chat.inputActions?.find(a=>a.id===grant.target&&a.kind==='publish-dialect'&&!a.applied):undefined
 const editing=useDialectRecipeEditing(action?.inputId??'')
 const mounted=useRef(true)
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
 useAutomaticAction({grant,action:'dialect-publish',path:action?publicationPath(action):undefined,revision:action?.revision,fingerprint:action?publicationFingerprint(action):undefined,
 ready:!!(restored&&action&&grant?.target===action.id&&chat.interaction!=='plan'&&binding.state.status==='complete'&&!binding.run?.running&&!binding.state.unknowns.length&&!binding.state.questions?.some(q=>!q.resolution)&&!editing),
 canStart:()=>!!action&&mounted.current&&!dialectRecipeIsEditing(action.inputId),
 execute:async grant=>{
  if(!action)return
  await savePublication({store,chatId:chat.id,action,grantId:grant.id,guard:()=>{if(!mounted.current||dialectRecipeIsEditing(action.inputId))throw Error(msg('Save or discard Recipe edits before reviewing this action.'))}})
  await Promise.allSettled([actionApprovalKey(chat.id),['dialect-publications',action.inputId],['dialect-publication-result']].map(queryKey=>cache.invalidateQueries({queryKey})))
 },onError:()=>{void cache.invalidateQueries({queryKey:actionApprovalKey(chat.id)});store.problem(msg('Automatic publication could not be verified. Review the proposal and check its saved result before trying again.'))}})
}
