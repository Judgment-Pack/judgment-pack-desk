import {useRef,useState,useEffect} from 'react'
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
export function usePackProposalSave({candidate,basis,busy,plan,onSaved}:{candidate:Candidate;basis?:PackProposalBasis;busy:boolean;plan?:boolean;onSaved?:()=>void}) {
  const mcp=useMcp(),query=useQueryClient(),[saving,setSaving]=useState(false),[error,setError]=useState(''),flight=useRef(false)
  const project=useFileListing().data?.root
  const mounted=useRef(true)
  useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
  useAuthorDirty()
  const latest=useRef({candidate,basis,busy,plan});latest.current={candidate,basis,busy,plan}
  const reason=plan?msg('Switch to Work before saving a proposal.'):!basis?msg('The original document is unavailable. Request a new proposal.')
    :isPathDirty(basis.path)?msg('Save or discard your editor changes before saving this proposal.')
    :!mcp.client?msg('Connect the runtime to check and save this proposal.'):''
  const save=async()=>{
    if(flight.current||busy||reason||!basis)return
    const client=mcp.client!,held=candidate,base=basis
    flight.current=true;setSaving(true);setError('')
    try {
      const guard=()=>{
        const current=latest.current
        if(!mounted.current||current.candidate.digest!==held.digest||current.basis?.path!==base.path||current.basis.packId!==base.packId||current.busy||current.plan||isPathDirty(base.path))throw new ProposalSaveBlocked(msg('The proposal or editor changed. Review it again before saving.'))
      }
      const saved=await savePackProposal({candidate:held,basis:base,reviewedSuiteKey:held.packReview?.check?.suiteKey??'',guard,
        check:()=>checkPackProposal(held,async()=>{const snapshot=await readTests();if(project!==undefined&&snapshot.project!==project)throw new ProposalSaveBlocked(msg('The project changed during the checks.'));return snapshot.content.suites[base.packId]??emptySuite()},async(name,args)=>await client.callTool({name,arguments:args}) as McpToolResult,new AbortController().signal)})
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
