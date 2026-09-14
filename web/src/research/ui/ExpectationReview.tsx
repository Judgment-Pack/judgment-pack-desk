import { Button } from '../../ui/Button'
import { CodeBlock } from '../../ui/CodeBlock'
import { Disclosure } from '../../ui/Disclosure'
import type { RunState } from '../run'
import type { Selection } from './DraftPanels'
import styles from './ResearchAuthoring.module.css'

export interface ExpectationReviewActions {
  onProposeCorrection?: (id: string) => void
  onApproveCorrection?: (id: string, token: string) => void
}

export function ExpectationReview({ state, onSelect, onProposeCorrection, onApproveCorrection }: ExpectationReviewActions & { state: RunState; onSelect: (selection: Selection) => void }) {
  if (state.expectationIssues.length === 0) return null
  const pending = state.expectationIssues.filter(issue => !issue.resolved)
  return <section className={styles.section} aria-label="Expectation review">
    <h3>{pending.length ? 'Invalid expectations' : 'Reviewed corrections'}</h3>
    {pending.length > 0 && <p className={styles.detail} role="status">{pending.length} expectation{pending.length === 1 ? '' : 's'} did not pass runtime expectation validation. Testing is paused until each correction is reviewed and approved.</p>}
    {state.expectationIssues.map(issue => <article className={styles.row} key={issue.id} aria-label={`Expectation ${issue.id}`}>
      <div className={styles.rowHead}><strong>{issue.id}</strong><span className={styles.badge}>{issue.resolved ? 'Correction approved' : 'Invalid expectation'}</span></div>
      <p className={styles.detail}>{issue.message}</p>
      <p className={styles.hint}>{issue.original.rationale}</p>
      <div><Button variant="inline" onClick={() => onSelect({ kind: 'excerpt', id: issue.original.expectationSource })}>View source {issue.original.expectationSource}</Button></div>
      {issue.resolved ? <Disclosure title="Correction history">
        <CodeBlock text={JSON.stringify(issue.original.expectedDisposition, null, 2)} label="Original expectation" />
        <CodeBlock text={JSON.stringify(issue.resolved.replacement.expectedDisposition, null, 2)} label="Approved expectation" />
        <p className={styles.detail}>{issue.resolved.rationale}</p>
        <p className={styles.hint}>Approved {issue.resolved.approvedAt}. Case inputs and source were preserved.</p>
      </Disclosure> : <>
        <CodeBlock text={JSON.stringify(issue.original.expectedDisposition, null, 2)} label="Original expectation" />
        {issue.proposalError && <p className={styles.detail} role="alert">{issue.proposalError}</p>}
        {issue.proposal && <>
          <CodeBlock text={JSON.stringify(issue.proposal.expectedDisposition, null, 2)} label="Proposed expectation" />
          <p className={styles.detail}>{issue.proposal.rationale}</p>
          <p className={styles.hint}>Only the expectation changes. Approval reruns all cases against the unchanged draft.</p>
          <div><Button variant="primary" disabled={state.status === 'running' || !onApproveCorrection} onClick={() => onApproveCorrection?.(issue.id, issue.proposal!.token)}>Approve correction and retest</Button></div>
        </>}
        <div><Button disabled={state.status === 'running' || !onProposeCorrection} onClick={() => onProposeCorrection?.(issue.id)}>{issue.proposal ? 'Request another correction' : 'Suggest correction'}</Button></div>
      </>}
    </article>)}
  </section>
}
