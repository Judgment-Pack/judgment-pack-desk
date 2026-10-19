import {useChats} from './ChatProvider'
import type { ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { useFileContent } from '../files/queries'
import { msg } from '../i18n'
import { useTestStorage } from '../packs/test-workspace/store'
import { jsonIdentity } from '../research/checkCandidate'
import type { Candidate, RunState } from '../research/run'
import {ChatAction,ChatActions} from './ChatActions'
import {IconCheck} from '../shell/icons'
import {ProposalDiffView} from '../assistant/ProposalDiffView'
import {diffProposal} from '../assistant/proposalDiff'
import {usePackProposalSave} from './usePackProposalSave'
import {legacyPackBasis,type PackProposalBasis} from './packProposal'
import { Disclosure } from '../ui/Disclosure'
import { CodeBlock } from '../ui/CodeBlock'
import { packVersion, pendingPackCandidate, samePack } from './packProposal'
import type { Chat } from './store'
import styles from './PackProposalCard.module.css'

export function PackProposalCard({candidate,packId,savedText,comparisonText,busy,onCheck,onCorrect,basis:retainedBasis,path,plan,onSaved,review,actions,actionDetail}: {
  candidate:Candidate;packId:string;savedText?:string;comparisonText?:string;busy:boolean;onCheck:()=>void;onCorrect?:()=>void;basis?:PackProposalBasis;path?:string;plan?:boolean;onSaved?:()=>void;review?:ReactNode;actions?:ReactNode;actionDetail?:ReactNode
}) {
  const basis=retainedBasis?.packId===packId && retainedBasis.path===path ? retainedBasis : undefined
  const saving=usePackProposalSave({candidate,basis,busy,plan,onSaved})
  const {suite,query} = useTestStorage(packId)
  const check = candidate.packReview?.check
  const current = check?.documentDigest === candidate.digest && query.isSuccess && check.suiteKey === jsonIdentity(suite.cases)
  const report = current ? check?.report : undefined
  const mismatches = report?.packs?.flatMap(pack => pack.rows ?? []).filter(row => row.status !== 'passed') ?? []
  const status = !current ? msg('Saved suite not checked for this proposal') : !check?.valid ? msg('Structure needs corrections')
    : report ? msg('{{passed}} of {{total}} saved tests passed', {passed:report.summary.passed,total:report.summary.total}) : msg('Structure checked · Tests not run')
  const gaps=report?.packs?.flatMap(p=>p.coverage??[]).filter(p=>p.status==='missing')??[]
  const saveReason=saving.reason||(!current?msg('Check the proposal before saving.'):!check?.valid||mismatches.length?msg('Resolve the validation findings and test mismatches before saving this proposal.'):'')
  return <section className={styles.root} aria-label={msg('Proposed pack update')}>
    <div className={styles.summary}>
      <strong className={styles.title}>{msg('Proposed update')} {packVersion(candidate.text)}</strong>
      <span>{msg('Saved version {{version}} · Changes not saved', {version:packVersion(savedText) || '—'})}</span>
      <span role="status">{status}</span>
    </div>
    {current && check?.valid && (report?.summary.total ?? 0) < check.totalCases && <p className={styles.caption}>{msg('{{count}} cases have no expected result and were not checked.',{count:check.totalCases-(report?.summary.total??0)})}</p>}
    <div className={styles.details}>
    {!!mismatches.length && <Disclosure size="small" title={msg('Test mismatches · {{count}}',{count:mismatches.length})}>
      {mismatches.map(row=><div key={row.id}><strong>{row.id}</strong><CodeBlock label={msg('Expected and actual')} text={JSON.stringify({expected:displayResult(row.expected),actual:displayResult(row.actual)},null,2)} /></div>)}
    </Disclosure>}
    {current && check && !check.valid && <Disclosure size="small" title={msg('Validation details')}><CodeBlock label={msg('Diagnostics')} text={JSON.stringify(check.diagnostics,null,2)}/></Disclosure>}
    {!!gaps.length && <Disclosure size="small" title={msg('Coverage gaps · {{count}}',{count:gaps.length})}>{gaps.map(gap=><p key={gap.probe}><strong>{gap.probe}</strong> · {gap.detail}</p>)}<p className={styles.caption}>{msg('Coverage probes suggest additional tests. They do not establish missing policy or justify changing expected results.')}</p></Disclosure>}
    {basis && <Disclosure size="small" title={msg('Review proposed changes')}><ProposalDiffView diff={diffProposal(basis.text,candidate.document)} onBaseline={basis.text===(comparisonText??savedText)}/></Disclosure>}
    </div>
    <ChatActions detail={(saving.error || saveReason || actionDetail) ? <>{(saving.error || saveReason) && <div>{saving.error || saveReason}</div>}{actionDetail && <div>{actionDetail}</div>}</> : undefined}>
      <ChatAction variant="primary" icon={<IconCheck/>} disabled={busy||saving.saving||!!saveReason} aria-busy={saving.saving} onClick={()=>void saving.save()}>{saving.saving?msg('Saving proposal…'):msg('Save proposal')}</ChatAction>
      <ChatAction variant="quiet" disabled={busy || saving.saving || !query.isSuccess} onClick={onCheck}>{msg('Check proposal')}</ChatAction>
      {current && (!check?.valid || !!mismatches.length) && onCorrect && <ChatAction disabled={busy||saving.saving||plan} onClick={onCorrect}>{msg('Fix findings')}</ChatAction>}
      {actions}
      {review}
    </ChatActions>
  </section>
}
/** The main chat and pack pane review and save the same retained proposal. */
export function LinkedPackProposal({chat,state,onCheck,busy}:{chat:Chat;state:RunState;onCheck:()=>void;busy:boolean}) {
  const file=useFileContent(chat.pack?.path), navigate=useNavigate(), {store}=useChats()
  const candidate=pendingPackCandidate(state,chat.createdCandidateDigest)
  if(!chat.pack || !candidate || state.status==='running' || samePack(candidate.text,file.data?.content))return null
  return <PackProposalCard candidate={candidate} packId={chat.pack.id} savedText={file.data?.content} path={chat.pack.path} busy={busy} onCheck={onCheck}
    onCorrect={()=>store?.perform(chat.id,active=>active.run?.send(msg('Correct the proposed pack update using the current policy and saved test findings. Keep saved cases unchanged; ask me if an expectation conflicts with policy.')))}
    basis={candidate.packReview?.basis ?? legacyPackBasis(state,chat.pack,candidate)} plan={chat.interaction==='plan'}
    review={<ChatAction variant="quiet" onClick={()=>navigate(`/packs/${encodeURIComponent(chat.pack!.id)}?edit=1&chat=${encodeURIComponent(chat.id)}`)}>{msg('Open editor')}</ChatAction>}/>
}

function displayResult(value:string):unknown {try{return JSON.parse(value)}catch{return value}}
