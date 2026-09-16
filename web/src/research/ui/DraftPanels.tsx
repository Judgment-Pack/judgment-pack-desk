import { Fragment, useState } from 'react'
import type { PackDocument } from '../../mcp/types'
import { PackOverview } from '../../packs/PackWorkspace'
import { Button } from '../../ui/Button'
import { CodeBlock } from '../../ui/CodeBlock'
import { Disclosure } from '../../ui/Disclosure'
import { Tabs } from '../../ui/Tabs'
import type { SourceRecord } from '../ledger'
import { canCreateResearchDraft, type RunState } from '../run'
import { ExpectationReview, type ExpectationReviewActions } from './ExpectationReview'
import styles from './ResearchAuthoring.module.css'

/** What the Inspector shows: a source, an excerpt within it, or a rule of the draft. */
export type Selection = { kind: 'source'; id: string } | { kind: 'excerpt'; id: string } | { kind: 'rule'; id: string } | null

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function verificationBadge(record: SourceRecord) {
  const state = record.failure !== null ? 'failed' : record.verification.state
  const label = record.failure !== null ? 'retrieval failed' : record.verification.state === 'verified' ? 'receipt verified' : record.verification.state === 'failed' ? 'receipt failed' : 'receipt unchecked'
  return (
    <span className={styles.badge} data-state={state}>
      {label}
    </span>
  )
}

export function SourcesPanel({ sources, selection, onSelect }: { sources: readonly SourceRecord[]; selection: Selection; onSelect: (next: Selection) => void }) {
  if (sources.length === 0) return <p className={styles.empty}>No sources yet. Searches and reads the assistant makes appear here, each with its receipt.</p>
  return (
    <ul className={styles.list} aria-label="Sources">
      {sources.map((record) => {
        const current = selection?.kind === 'source' && selection.id === record.id
        const title = record.kind === 'search' ? `Search: ${record.request.query ?? ''}` : record.document?.title || record.request.url || record.id
        return (
          <li key={record.id} className={styles.row} data-current={current} aria-current={current ? 'true' : undefined}>
            <button type="button" className={styles.rowButton} onClick={() => onSelect({ kind: 'source', id: record.id })} aria-label={`Inspect ${record.id}`}>
              <div className={styles.rowHead}>
                <span className={styles.badge}>{record.id}</span>
                <strong>{title}</strong>
                {verificationBadge(record)}
                {record.excerpts.length > 0 && <span className={styles.badge}>{record.excerpts.length} excerpt{record.excerpts.length === 1 ? '' : 's'}</span>}
              </div>
              {record.kind === 'page' && <div className={styles.url}>{record.request.url}</div>}
              {record.kind === 'search' && record.hits && <div className={styles.url}>{record.hits.length} hit(s) from {record.request.source}</div>}
              {record.failure !== null && <div className={styles.url}>{record.failure}</div>}
            </button>
          </li>
        )
      })}
    </ul>
  )
}

/**
 * One side of what the check compared, in the shape the review panel's
 * `Expectation` uses. §8.3 keeps the handoff target outside the disposition and
 * the check compares the pair, so the target is printed beside the disposition
 * rather than left out of the only place a person can read it.
 *
 * No target line where the case asserted none, decided: `checkCandidate`
 * records `handoffTarget` on both sides only where the row carries
 * `expectedHandoffTarget`, so an absent line means "not compared" rather than
 * "no target". Printing a target the runtime returned but nothing compared
 * would put a value in a disclosure of a disagreement that is no part of the
 * disagreement, and printing `null` would assert the runtime named none. This
 * shows what was compared and nothing else.
 */
function CheckedSide({ side, label }: { side: unknown; label: string }) {
  const pair = (side ?? {}) as { disposition?: unknown; handoffTarget?: unknown }
  return <>
    <CodeBlock text={JSON.stringify(pair.disposition ?? null, null, 2)} label={`${label} disposition`} />
    {pair.handoffTarget !== undefined && <p className={styles.hint}>{label} handoff target: <code>{JSON.stringify(pair.handoffTarget)}</code></p>}
  </>
}

export function TestsPanel({ state, onSelect, ...actions }: ExpectationReviewActions & { state: RunState; onSelect: (next: Selection) => void }) {
  const latest = state.candidates.at(-1)
  const check = latest?.check
  const byId = new Map((check?.cases ?? []).map((row) => [row.id, row]))
  if (state.cases.length === 0 && state.droppedCases.length === 0 && state.expectationIssues.length === 0) {
    return <p className={styles.empty}>No test cases yet. A reviewer establishes them from the cited excerpts once a draft exists.</p>
  }
  return (
    <div className={styles.panel}>
      <ExpectationReview state={state} onSelect={onSelect} {...actions} />
      {check && (
        <p className={styles.detail}>
          Revision {latest?.revision}: {check.valid ? 'valid' : 'invalid'} to the runtime; {check.cases.filter((c) => c.passed).length} of {check.cases.length} cases agree with their expectations. Rehearsal only; no decision was recorded.
        </p>
      )}
      {check && !check.valid && check.diagnostics.length > 0 && (
        <Disclosure title={`${check.diagnostics.length} validation diagnostic(s)`}>
          <CodeBlock text={JSON.stringify(check.diagnostics, null, 2)} label="Diagnostics" />
        </Disclosure>
      )}
      <div className={styles.tableWrap}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th scope="col">Case</th>
              <th scope="col">Expected</th>
              <th scope="col">Runtime answered</th>
              <th scope="col">Result</th>
              <th scope="col">Expectation from</th>
            </tr>
          </thead>
          <tbody>
            {state.cases.map((row) => {
              const result = byId.get(row.id)
              const expected = (row.expectedDisposition as { kind?: string; outcomeId?: string }) ?? {}
              const actual = (result?.actual as { disposition?: { kind?: string; outcomeId?: string } } | null)?.disposition
              // A refusal is not a comparison: the evaluation did not complete, so
              // what the check stored is not a pair — `null`, or the bare
              // disposition of a rehearsal that never finished — and a disclosure
              // over it would print `Runtime disposition: null` beside a real
              // expectation, an answer the runtime never gave. The refusal named
              // in the Runtime answered cell is the whole of what there is to show,
              // and it is why the row's `disagrees` badge carries no disclosure.
              const differs = result !== undefined && !result.passed && result.refused === undefined
              return (
                <Fragment key={row.id}>
                  <tr>
                    <td>
                      <div>{row.id}</div>
                      <div className={styles.hint}>{row.rationale}</div>
                    </td>
                    <td>{expected.outcomeId ?? expected.kind ?? '—'}</td>
                    <td>{result === undefined ? 'not yet checked' : result.refused ? `refused: ${result.refused}` : (actual?.outcomeId ?? actual?.kind ?? '—')}</td>
                    <td>
                      {result === undefined ? (
                        <span className={styles.badge}>pending</span>
                      ) : (
                        <span className={styles.badge} data-state={result.passed ? 'passed' : 'disagrees'}>
                          {result.passed ? 'agrees' : 'disagrees'}
                        </span>
                      )}
                    </td>
                    <td>
                      <Button variant="inline" onClick={() => onSelect({ kind: 'excerpt', id: row.expectationSource })}>
                        {row.expectationSource}
                      </Button>
                    </td>
                  </tr>
                  {differs && (
                    <tr>
                      <td colSpan={5}>
                        <Disclosure title={`What ${row.id} disagrees about`}>
                          <CheckedSide side={result.expected} label="Expected" />
                          <CheckedSide side={result.actual} label="Runtime" />
                        </Disclosure>
                      </td>
                    </tr>
                  )}
                </Fragment>
              )
            })}
          </tbody>
        </table>
      </div>
      {state.droppedCases.length > 0 && (
        <Disclosure title={`${state.droppedCases.length} proposed case(s) were not admitted`}>
          <ul className={styles.unknowns}>
            {state.droppedCases.map((dropped) => (
              <li key={dropped.id}>
                {dropped.id}: {dropped.reason}
              </li>
            ))}
          </ul>
        </Disclosure>
      )}
    </div>
  )
}

export function DraftPanel({ state, onSelect }: { state: RunState; onSelect: (next: Selection) => void }) {
  const latest = state.candidates.at(-1)
  if (!latest) return <p className={styles.empty}>No draft yet.</p>
  const document = latest.document
  const rules = isRecord(document) && Array.isArray(document.rules) ? (document.rules as Record<string, unknown>[]) : []
  return (
    <div className={styles.panel}>
      <p className={styles.detail}>
        Revision {latest.revision} · produced by {latest.producedBy} · sha256 {latest.digest.slice(0, 12)}…
      </p>
      {isRecord(document) && <PackOverview document={document as unknown as PackDocument} />}
      {rules.length > 0 && (
        <section className={styles.section} aria-label="Rules and their sources">
          <h3>Rules and their sources</h3>
          <ul className={styles.list}>
            {rules.map((rule, index) => {
              const id = typeof rule.id === 'string' ? rule.id : `#${index}`
              const refs = Array.isArray(rule.sourceRefs) ? (rule.sourceRefs as string[]) : []
              return (
                <li key={id} className={styles.row}>
                  <button type="button" className={styles.rowButton} onClick={() => onSelect({ kind: 'rule', id })} aria-label={`Inspect rule ${id}`}>
                    <div className={styles.rowHead}>
                      <strong>{id}</strong>
                      <span className={styles.badge}>{refs.length === 0 ? 'no source' : `${refs.length} source ref${refs.length === 1 ? '' : 's'}`}</span>
                    </div>
                    <div className={styles.url}>{typeof rule.description === 'string' ? rule.description : ''}</div>
                  </button>
                </li>
              )
            })}
          </ul>
        </section>
      )}
      <Disclosure title="Full document (JSON)">
        <CodeBlock text={latest.text} label="Pack JSON" />
      </Disclosure>
    </div>
  )
}

export function ReviewPanel({ state, sources, onCreate, onSelect }: { state: RunState; sources: readonly SourceRecord[]; onCreate: () => void; onSelect: (next: Selection) => void }) {
  const latest = state.candidates.at(-1)
  const check = latest?.check
  const passing = canCreateResearchDraft(state)
  const verified = sources.filter((s) => s.verification.state === 'verified').length
  const failed = sources.filter((s) => s.verification.state === 'failed' || s.failure !== null).length
  const untraced = state.citations.filter((c) => !c.traced)
  return (
    <div className={styles.panel}>
      <section className={styles.section}>
        <h3>Where this stands</h3>
        <p className={styles.detail}>{state.detail || 'Not started.'}</p>
        <dl className={styles.facts}>
          <dt>Revisions</dt>
          <dd>{state.candidates.length} ({state.revisionsUsed} repair{state.revisionsUsed === 1 ? '' : 's'})</dd>
          <dt>Test cases</dt>
          <dd>{state.expectationIssues.some(issue => !issue.resolved) ? 'Blocked by invalid expectations' : check ? `${check.cases.filter((c) => c.passed).length} of ${state.cases.length} agree` : 'not yet checked'}</dd>
          <dt>Sources</dt>
          <dd>
            {sources.length} recorded; {verified} with verified receipts; {failed} failed or unverified
          </dd>
          <dt>Citations</dt>
          <dd>
            {state.citations.length - untraced.length} of {state.citations.length} traced to a recorded excerpt
          </dd>
        </dl>
      </section>
      {state.unknowns.length > 0 && (
        <section className={styles.section}>
          <h3>Assumptions and open questions</h3>
          <ul className={styles.unknowns}>
            {state.unknowns.map((line, index) => (
              <li key={index}>{line}</li>
            ))}
          </ul>
        </section>
      )}
      {untraced.length > 0 && (
        <section className={styles.section}>
          <h3>Citations that could not be traced</h3>
          <ul className={styles.unknowns}>
            {untraced.map((citation) => (
              <li key={citation.sourceId}>
                <strong>{citation.sourceId}</strong>: {citation.reason}
                {citation.url ? ` (${citation.url})` : ''}
              </li>
            ))}
          </ul>
        </section>
      )}
      {state.citations.some((c) => c.traced) && (
        <section className={styles.section}>
          <h3>Traced citations</h3>
          <ul className={styles.list}>
            {state.citations
              .filter((c) => c.traced)
              .map((citation) => (
                <li key={citation.sourceId} className={styles.row}>
                  <button type="button" className={styles.rowButton} onClick={() => onSelect({ kind: 'excerpt', id: citation.excerptId! })}>
                    <div className={styles.rowHead}>
                      <strong>{citation.sourceId}</strong>
                      <span className={styles.badge}>{citation.excerptId}</span>
                    </div>
                    <div className={styles.url}>{citation.url}</div>
                  </button>
                </li>
              ))}
          </ul>
        </section>
      )}
      {state.candidates.length > 1 && (
        <section className={styles.section}>
          <h3>Revisions</h3>
          <ul className={styles.unknowns}>
            {state.candidates.map((candidate) => (
              <li key={candidate.revision}>
                Revision {candidate.revision} ({candidate.producedBy}):{' '}
                {candidate.check ? `${candidate.check.valid ? 'valid' : 'invalid'}, ${candidate.check.cases.filter((c) => c.passed).length}/${candidate.check.cases.length} agree` : 'not checked'}
              </li>
            ))}
          </ul>
        </section>
      )}
      <section className={styles.section}>
        <h3>Create</h3>
        <p className={styles.detail}>
          {passing
            ? 'Creating hands this draft, its test cases and its research record to the Create page, where you name the pack and the runtime validates the bytes before anything is written.'
            : 'Create is offered once every established case agrees with the runtime and the draft is valid.'}
        </p>
        <p className={styles.hint}>
          A verified receipt establishes that the gateway signed these bytes and sealed the session; it does not establish that a page is true, current, legally authoritative, or that it came from the site its URL names.
        </p>
        <div>
          <Button variant="primary" disabled={!passing || state.status === 'running'} onClick={onCreate}>
            Create pack from this draft
          </Button>
        </div>
      </section>
    </div>
  )
}

export function DraftTabs({ state, sources, selection, onSelect, onCreate, ...actions }: ExpectationReviewActions & { state: RunState; sources: readonly SourceRecord[]; selection: Selection; onSelect: (next: Selection) => void; onCreate: () => void }) {
  const [tab, setTab] = useState('draft')
  const pending = state.expectationIssues.filter(issue => !issue.resolved).length
  const total = state.cases.length + pending
  return (
    <section className={styles.pane} aria-label="Draft review" data-pane="draft">
      <header className={styles.paneHeader}>
        <span>Draft</span>
        <span className={styles.status}>{state.candidates.length === 0 ? 'no revision yet' : `revision ${state.candidates.at(-1)!.revision}`}</span>
      </header>
      {pending > 0 && <div className={styles.panel} role="status"><span>{pending} invalid expectation{pending === 1 ? '' : 's'} · testing paused</span><div><Button variant="quiet" onClick={() => setTab('tests')}>Review expectations</Button></div></div>}
      <Tabs
        scrollable
        label="Draft views"
        value={tab}
        onValueChange={setTab}
        tabs={[
          { value: 'draft', label: 'Draft', panel: <DraftPanel state={state} onSelect={onSelect} /> },
          { value: 'sources', label: `Sources${sources.length ? ` (${sources.length})` : ''}`, panel: <div className={styles.panel}><SourcesPanel sources={sources} selection={selection} onSelect={onSelect} /></div> },
          { value: 'tests', label: `Tests${total ? ` (${total})` : ''}`, panel: <TestsPanel state={state} onSelect={onSelect} {...actions} /> },
          { value: 'review', label: 'Review', panel: <ReviewPanel state={state} sources={sources} onCreate={onCreate} onSelect={onSelect} /> }
        ]}
      />
    </section>
  )
}
