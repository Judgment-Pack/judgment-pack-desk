import {useRef,useEffect} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {useAutomaticAction} from '../assistant/approvals/automatic'
import {useActionApproval,actionApprovalKey,actionApprovalAPI} from '../assistant/approvals/client'
import {useFileContent} from '../files/queries'
import type {FileContent} from '../files/client'
import {isPathDirty,useAuthorDirty} from '../shell/authorBridge'
import {checkPackProposal,pendingPackCandidate,packCheckAllowsAuto} from './packProposal'
import {savePackProposal,ProposalSaveBlocked} from './savePackProposal'
import {readTests} from '../packs/test-workspace/store'
import {emptySuite} from '../packs/test-workspace/model'
import {msg} from '../i18n'
import type {Chat,ChatStore} from './store'
import type {ResearchRunBinding} from '../research/useResearchRun'
import type {CallTool} from '../assistant/engine'
/** The pack adapter runs with the chat worker, including when its pane is closed. */
export function usePackAutoSave({chat,store,binding,restored,call,project}:{chat:Chat;store:ChatStore;binding:ResearchRunBinding;restored:boolean;call:CallTool;project?:string}) {
 const approval=useActionApproval(chat.id,!!chat.pack),file=useFileContent(chat.pack?.path),cache=useQueryClient()
 useAuthorDirty()
 const mounted=useRef(true)
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
 const candidate=pendingPackCandidate(binding.state,chat.createdCandidateDigest),basis=candidate?.packReview?.basis,check=candidate?.packReview?.check
 const latest=useRef({chat,binding,candidate});latest.current={chat,binding,candidate}
 useAutomaticAction({grant:approval.isError?undefined:approval.data?.grant,action:'pack-update',path:chat.pack?.path,revision:file.data?.sha256,fingerprint:candidate?.digest,
  ready:!!(restored&&chat.pack&&chat.interaction!=='plan'&&binding.state.status==='complete'&&basis&&basis.packId===chat.pack.id&&basis.path===chat.pack.path&&basis.text===file.data?.content&&check?.documentDigest===candidate?.digest&&packCheckAllowsAuto(check)&&!binding.state.unknowns.length&&!binding.state.questions?.some(q=>!q.resolution)&&!isPathDirty(basis.path)),
  canStart:()=>!!basis&&mounted.current&&!isPathDirty(basis.path),
  execute:async grant=>{
   if(!candidate||!basis||!check)return
   store.report(chat.id,binding);await store.checkpoint(new AbortController().signal)
   const guard=()=>{const current=latest.current;if(!mounted.current||current.chat.interaction==='plan'||current.binding.run?.running||current.candidate?.digest!==candidate.digest||isPathDirty(basis.path))throw new ProposalSaveBlocked(msg('The proposal or editor changed. Review it again before saving.'))}
   const saved=await savePackProposal({candidate,basis,reviewedSuiteKey:check.suiteKey,guard,
    check:async()=>{const fresh=await checkPackProposal(candidate,async()=>{const saved=await readTests();if(saved.project!==project)throw new ProposalSaveBlocked(msg('The project changed during the checks.'));return saved.content.suites[basis.packId]??emptySuite()},call,new AbortController().signal);if(!packCheckAllowsAuto(fresh))throw new ProposalSaveBlocked(msg('Review the latest checks before saving automatically.'));return fresh},
    write:input=>actionApprovalAPI<FileContent>(chat.id,'commit',{action:'pack-update',id:grant.id,revision:input.baseSha256,content:input.content})})
   cache.setQueryData(['desk-file',basis.path],saved)
   await Promise.allSettled([actionApprovalKey(chat.id),['list_packs'],['get_pack',basis.packId],['validate'],['desk-files']].map(queryKey=>cache.invalidateQueries({queryKey})))
  },onError:()=>{void cache.invalidateQueries({queryKey:actionApprovalKey(chat.id)});store.problem(msg('Automatic save could not be completed or verified. Review the proposal and its allowance before trying again.'))}})
}
