import { Message } from '../../i18n/Message'
import { msg, useLocale } from '../../i18n'
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
  useLocale()
  return <>
    <CodeBlock text={JSON.stringify(disposition, null, 2)} label={label} />
    {target !== undefined && <p className={styles.hint}><Message text={"Expected handoff target: <0/>. Unchanged by a correction."} slots={[<code>{JSON.stringify(target)}</code>]} /></p>}
  </>
}

export function ExpectationReview({ state, onSelect, onProposeCorrection, onApproveCorrection }: ExpectationReviewActions & { state: RunState; onSelect: (selection: Selection) => void }) {
  useLocale()
  if (state.expectationIssues.length === 0) return null
  const pending = state.expectationIssues.filter(issue => !issue.resolved)
  return <section className={styles.section} aria-label={msg("Expectation review")}>
    <h3>{pending.length ? msg("Blocked expectations") : msg("Reviewed corrections")}</h3>
    {pending.length > 0 && <p className={styles.detail} role="status"><Message text={"<0/> expectation<1/> did not pass runtime expectation validation. Testing is paused until each correction is reviewed and approved."} slots={[pending.length, pending.length === 1 ? '' : msg("s")]} /></p>}
    {state.expectationIssues.map(issue => <article className={styles.row} key={issue.id} aria-label={msg("Expectation {{value0}}", { value0: issue.id })}>
      <div className={styles.rowHead}><strong>{issue.id}</strong><span className={styles.badge}>{issue.resolved ? msg("Correction approved") : msg("Blocked expectation")}</span></div>
      <p className={styles.detail}>{issue.message}</p>
      <p className={styles.hint}>{issue.original.rationale}</p>
      <div><Button variant="inline" onClick={() => onSelect({ kind: 'excerpt', id: issue.original.expectationSource })}><Message text={"View source <0/>"} slots={[issue.original.expectationSource]} /></Button></div>
      {issue.resolved ? <Disclosure title={msg("Correction history")}>
        <Expectation disposition={issue.original.expectedDisposition} target={issue.original.expectedHandoffTarget} label={msg("Original expectation")} />
        <Expectation disposition={issue.resolved.replacement.expectedDisposition} target={issue.resolved.replacement.expectedHandoffTarget} label={msg("Approved expectation")} />
        <p className={styles.detail}>{issue.resolved.rationale}</p>
        <p className={styles.hint}><Message text={"Approved <0/>. Case inputs and source were preserved."} slots={[issue.resolved.approvedAt]} /></p>
      </Disclosure> : <>
        <Expectation disposition={issue.original.expectedDisposition} target={issue.original.expectedHandoffTarget} label={msg("Original expectation")} />
        {issue.proposalError && <p className={styles.detail} role="alert">{issue.proposalError}</p>}
        {issue.proposal && <>
          <Expectation disposition={issue.proposal.expectedDisposition} target={issue.original.expectedHandoffTarget} label={msg("Proposed expectation")} />
          <p className={styles.detail}>{issue.proposal.rationale}</p>
          <p className={styles.hint}>{msg("Only the expectation changes. Approval reruns all cases against the unchanged draft.")}</p>
          <div><Button variant="primary" disabled={state.status === 'running' || !onApproveCorrection} onClick={() => onApproveCorrection?.(issue.id, issue.proposal!.token)}>{msg("Approve correction and retest")}</Button></div>
        </>}
        <div><Button disabled={state.status === 'running' || !onProposeCorrection} onClick={() => onProposeCorrection?.(issue.id)}>{issue.proposal ? msg("Request another correction") : msg("Suggest correction")}</Button></div>
      </>}
    </article>)}
  </section>
}
