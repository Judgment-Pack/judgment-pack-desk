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

/**
 * One half of an exact expectation, with the handoff target it is asserted
 * beside. §8.3 keeps the target outside the disposition, and a row asserts both,
 * so showing the disposition alone would show a person less than they approve.
 */
function Expectation({ disposition, target, label }: { disposition: unknown; target: unknown; label: string }) {
  return <>
    <CodeBlock text={JSON.stringify(disposition, null, 2)} label={label} />
    {target !== undefined && <p className={styles.hint}>Expected handoff target: <code>{JSON.stringify(target)}</code>. Unchanged by a correction.</p>}
  </>
}

export function ExpectationReview({ state, onSelect, onProposeCorrection, onApproveCorrection }: ExpectationReviewActions & { state: RunState; onSelect: (selection: Selection) => void }) {
  if (state.expectationIssues.length === 0) return null
  const pending = state.expectationIssues.filter(issue => !issue.resolved)
  return <section className={styles.section} aria-label="Expectation review">
    <h3>{pending.length ? 'Blocked expectations' : 'Reviewed corrections'}</h3>
    {pending.length > 0 && <p className={styles.detail} role="status">{pending.length} expectation{pending.length === 1 ? '' : 's'} did not pass runtime expectation validation. Testing is paused until each correction is reviewed and approved.</p>}
    {state.expectationIssues.map(issue => <article className={styles.row} key={issue.id} aria-label={`Expectation ${issue.id}`}>
      <div className={styles.rowHead}><strong>{issue.id}</strong><span className={styles.badge}>{issue.resolved ? 'Correction approved' : 'Blocked expectation'}</span></div>
      <p className={styles.detail}>{issue.message}</p>
      <p className={styles.hint}>{issue.original.rationale}</p>
      <div><Button variant="inline" onClick={() => onSelect({ kind: 'excerpt', id: issue.original.expectationSource })}>View source {issue.original.expectationSource}</Button></div>
      {issue.resolved ? <Disclosure title="Correction history">
        <Expectation disposition={issue.original.expectedDisposition} target={issue.original.expectedHandoffTarget} label="Original expectation" />
        <Expectation disposition={issue.resolved.replacement.expectedDisposition} target={issue.resolved.replacement.expectedHandoffTarget} label="Approved expectation" />
        <p className={styles.detail}>{issue.resolved.rationale}</p>
        <p className={styles.hint}>Approved {issue.resolved.approvedAt}. Case inputs and source were preserved.</p>
      </Disclosure> : <>
        <Expectation disposition={issue.original.expectedDisposition} target={issue.original.expectedHandoffTarget} label="Original expectation" />
        {issue.proposalError && <p className={styles.detail} role="alert">{issue.proposalError}</p>}
        {issue.proposal && <>
          <Expectation disposition={issue.proposal.expectedDisposition} target={issue.original.expectedHandoffTarget} label="Proposed expectation" />
          <p className={styles.detail}>{issue.proposal.rationale}</p>
          <p className={styles.hint}>Only the expectation changes. Approval reruns all cases against the unchanged draft.</p>
          <div><Button variant="primary" disabled={state.status === 'running' || !onApproveCorrection} onClick={() => onApproveCorrection?.(issue.id, issue.proposal!.token)}>Approve correction and retest</Button></div>
        </>}
        <div><Button disabled={state.status === 'running' || !onProposeCorrection} onClick={() => onProposeCorrection?.(issue.id)}>{issue.proposal ? 'Request another correction' : 'Suggest correction'}</Button></div>
      </>}
    </article>)}
  </section>
}
