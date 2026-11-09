import {useChatStore} from './ChatProvider'
import {useRef,useState,useEffect,useSyncExternalStore} from 'react'
import {useQueryClient} from '@tanstack/react-query'
import {useFileListing} from '../files/queries'
import {useMcp} from '../mcp/McpProvider'
import type {McpToolResult} from '@judgment-pack/harness/engine'
import {readTests} from '../packs/test-workspace/store'
import {emptySuite} from '../packs/test-workspace/model'
import {isPathDirty,useAuthorDirty} from '../shell/authorBridge'
import {FileRequestError,StaleWrite} from '../files/client'
import {msg,systemMessage} from '../i18n'
import type {Candidate} from '../research/run'
import {checkPackProposal,type PackProposalBasis} from './packProposal'
import {ProposalSaveBlocked,savePackProposal} from './savePackProposal'
const emptyBindings=new Map(),subscribeNothing=()=>()=>{}
export function usePackProposalSave({candidate,basis,busy,plan,onSaved}:{candidate:Candidate;basis?:PackProposalBasis;busy:boolean;plan?:boolean;onSaved?:()=>void}) {
  const store=useChatStore()
  useSyncExternalStore(store?.subscribe??subscribeNothing,()=>store?.getSnapshot().bindings??emptyBindings,()=>emptyBindings)
  const ownerFor=(held:Candidate)=>store&&[...store.getSnapshot().bindings.values()].find(b=>b.run?.ownership==='server'&&b.state.candidates.some(c=>c.digest===held.digest))
  const owner=ownerFor(candidate)
  const reconnectReason=msg('Reconnect the current proposal before checking it.')
  const mcp=useMcp(),query=useQueryClient(),[saving,setSaving]=useState(false),[error,setError]=useState(''),flight=useRef(false)
  const project=useFileListing().data?.root
  const mounted=useRef(true)
  useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
  useAuthorDirty()
  const latest=useRef({candidate,basis,busy,plan});latest.current={candidate,basis,busy,plan}
  const reason=plan?msg('Switch to Work before saving a proposal.'):!basis?msg('The original document is unavailable. Request a new proposal.')
    :isPathDirty(basis.path)?msg('Save or discard your editor changes before saving this proposal.')
    :!mcp.client?msg('Connect the runtime to check and save this proposal.'):owner&&!owner.run?.canCheckProposal?.(candidate)?reconnectReason:''
  const save=async()=>{
    if(flight.current||busy||reason||!basis)return
    const client=mcp.client!,held=candidate,base=basis,serverOwned=!!owner
    flight.current=true;setSaving(true);setError('')
    try {
      const guard=()=>{
        const current=latest.current
        if(!mounted.current||current.candidate.digest!==held.digest||current.basis?.path!==base.path||current.basis.packId!==base.packId||current.busy||current.plan||isPathDirty(base.path))throw new ProposalSaveBlocked(msg('The proposal or editor changed. Review it again before saving.'))
        const currentOwner=ownerFor(held)
        if((serverOwned||currentOwner)&&!currentOwner?.run?.canCheckProposal?.(held))throw new ProposalSaveBlocked(reconnectReason)
      }
      const saved=await savePackProposal({candidate:held,basis:base,reviewedSuiteKey:held.packReview?.check?.suiteKey??'',guard,
        check:()=>{
          const owner=ownerFor(held)
          if(owner||serverOwned){if(!owner?.run?.canCheckProposal?.(held)||!owner.run.checkProposal)throw new ProposalSaveBlocked(msg('Reconnect the current proposal before checking it.'));return owner.run.checkProposal(held)}
          return checkPackProposal(held,async()=>{const snapshot=await readTests();if(project!==undefined&&snapshot.project!==project)throw new ProposalSaveBlocked(msg('The project changed during the checks.'));return snapshot.content.suites[base.packId]??emptySuite()},async(name,args)=>await client.callTool({name,arguments:args}) as McpToolResult,new AbortController().signal)}})
      query.setQueryData(['desk-file',base.path],saved)
      await Promise.allSettled([['list_packs'],['get_pack',base.packId],['validate'],['desk-files']].map(queryKey=>query.invalidateQueries({queryKey})))
      onSaved?.()
    } catch(cause) {
      setError(cause instanceof ProposalSaveBlocked || cause instanceof FileRequestError ? systemMessage(cause.message)
        :cause instanceof StaleWrite?msg('The pack changed before saving. Refresh and review the current version.')
        :msg('The save could not be completed or verified. Refresh the pack before trying again.'))
    } finally {flight.current=false;setSaving(false)}
  }
  return {save,saving,error,reason}
}
