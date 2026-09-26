import { DraftSources, declaredSources } from './DraftSources'
import { DraftComparison } from '../../packs/DraftComparison'
import type { ComponentProps, ReactNode } from 'react'
import type { NodePositions } from '../../components/RelationshipMap'
import { Message } from '../../i18n/Message'
import { systemMessage, msg, useLocale } from '../../i18n'
import { Fragment, useMemo, useRef, useState } from 'react'
import type { PackDocument } from '../../mcp/types'
import { PackLogic } from '../../packs/PackLogic'
import { projectLogic } from '../../packs/logicModel'
import { initialLogicDisplay, type LogicMode } from '../../packs/logicState'
import { PackOverview, PackQuestion } from '../../packs/PackWorkspace'
import { Button } from '../../ui/Button'
import { CodeBlock } from '../../ui/CodeBlock'
import { Disclosure } from '../../ui/Disclosure'
import { Tabs } from '../../ui/Tabs'
import type { SourceRecord } from '../ledger'
import { canCreateResearchDraft, type RunState } from '../run'
import { ExpectationReview, type ExpectationReviewActions } from './ExpectationReview'
import styles from './ResearchAuthoring.module.css'

/** What the Inspector shows: a source, an excerpt within it, or a rule of the draft. */
export type Selection = { kind: 'source'; id: string } | { kind: 'excerpt'; id: string } | { kind: 'rule'; id: string } | { kind: 'logic'; id: string } | null

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function verificationBadge(record: SourceRecord) {
  const state = record.failure !== null ? 'failed' : record.verification.state
  const label = record.failure !== null ? msg("retrieval failed") : record.verification.state === 'verified' ? msg("receipt verified") : record.verification.state === 'failed' ? msg("receipt failed") : msg("receipt unchecked")
  return (
    <span className={styles.badge} data-state={state}>
      {label}
    </span>
  )
}

export function SourcesPanel({ sources, selection, onSelect }: { sources: readonly SourceRecord[]; selection: Selection; onSelect: (next: Selection) => void }) {
  useLocale()
  if (sources.length === 0) return <p className={styles.empty}>{msg("No sources yet. Searches and reads the assistant makes appear here, each with its receipt.")}</p>
  return (
    <ul className={styles.list} aria-label={msg("Sources")}>
      {sources.map((record) => {
        const current = selection?.kind === 'source' && selection.id === record.id
        const title = record.kind === 'search' ? msg("Search: {{value0}}", { value0: record.request.query ?? '' }) : record.document?.title || record.request.url || record.id
        return (
          <li key={record.id} className={styles.row} data-current={current} aria-current={current ? 'true' : undefined}>
            <button type="button" className={styles.rowButton} onClick={() => onSelect({ kind: 'source', id: record.id })} aria-label={msg("Inspect {{value0}}", { value0: record.id })}>
              <div className={styles.rowHead}>
                <span className={styles.badge}>{record.id}</span>
                <strong>{title}</strong>
                {verificationBadge(record)}
                {record.excerpts.length > 0 && <span className={styles.badge}>{msg("{{count}} excerpts", { count: record.excerpts.length })}</span>}
              </div>
              {record.kind === 'page' && <div className={styles.url}>{record.request.url}</div>}
              {record.kind === 'search' && record.hits && <div className={styles.url}><Message text={"<0/> hit(s) from <1/>"} slots={[record.hits.length, record.request.source]} /></div>}
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
 * Decided: no target line at all where the case asserted none. `checkCandidate`
 * records `handoffTarget` on both sides only where the row carries
 * `expectedHandoffTarget`, so an absent line means "not compared" rather than
 * "no target". Printing a target the runtime returned but nothing compared
 * would put a value in a disclosure of a disagreement that is no part of the
 * disagreement, and printing `null` would assert the runtime named none. This
 * shows what was compared and nothing else.
 */
function CheckedSide({ side, label }: { side: unknown; label: string }) {
  useLocale()
  const pair = (side ?? {}) as { disposition?: unknown; handoffTarget?: unknown }
  return <>
    <CodeBlock text={JSON.stringify(pair.disposition ?? null, null, 2)} label={msg("{{value0}} disposition", { value0: label })} />
    {pair.handoffTarget !== undefined && <p className={styles.hint}><Message text={"<0/> handoff target: <1/>"} slots={[label, <code>{JSON.stringify(pair.handoffTarget)}</code>]} /></p>}
  </>
}

function RuntimeChecks({ state }: { state: RunState }) {
  useLocale()
  const latest = state.candidates.at(-1)
  const check = latest?.check ?? latest?.previousCheck
  const probes = (state.probes ?? []).filter(probe => probe.documentDigest === latest?.digest)
  return <>
    <section className={styles.section}>
      <h3>{msg('Structure check')}</h3>
      <p>{check ? (check.valid ? msg('Valid') : msg('Invalid')) : msg('Not run')}</p>
      {!latest?.check && check && <p className={styles.detail}>{msg('Saved result. Recheck the draft before creating it.')}</p>}
      {check && check.diagnostics.length > 0 && <Disclosure title={msg('Diagnostics')}><CodeBlock text={JSON.stringify(check.diagnostics, null, 2)} label={msg('Diagnostics')} /></Disclosure>}
    </section>
    {probes.length > 0 && <section className={styles.section}>
      <h3>{msg('Runtime rehearsals')}</h3>
      <p className={styles.detail}>{msg('Recorded runtime responses. These probes have no independently established pass or fail expectation.')}</p>
      {probes.map((probe, index) => <Disclosure key={index} title={msg('Rehearsal {{value0}}', { value0: index + 1 })}>
        <CodeBlock text={JSON.stringify(probe.facts, null, 2)} label={msg('Facts')} />
        {probe.evidence !== undefined && <CodeBlock text={JSON.stringify(probe.evidence, null, 2)} label={msg('Evidence')} />}
        <CodeBlock text={JSON.stringify({ disposition: probe.disposition, ...(probe.handoffTarget === undefined ? {} : { handoffTarget: probe.handoffTarget }) }, null, 2)} label={msg('Runtime answered')} />
      </Disclosure>)}
    </section>}
  </>
}

export function TestsPanel({ state, onSelect, mode = 'research', ...actions }: ExpectationReviewActions & { mode?: 'draft' | 'research'; state: RunState; onSelect: (next: Selection) => void }) {
  useLocale()
  const latest = state.candidates.at(-1)
  const check = latest?.check ?? latest?.previousCheck
  const checks = <RuntimeChecks state={state} />
  const byId = new Map((check?.cases ?? []).map((row) => [row.id, row]))
  if (state.cases.length === 0 && state.droppedCases.length === 0 && state.expectationIssues.length === 0) {
    return <div className={styles.panel}>{checks}<p className={styles.empty}>{(state.probes ?? []).some(probe => probe.documentDigest === latest?.digest) ? `${msg('Test cases')}: 0` : mode === 'draft' ? msg("Tests have not been run. Chat drafts receive a structure check. After creating the pack, use Tests to check its decisions.") : msg("No test cases yet. Research establishes cases from cited sources once a draft exists.")}</p></div>
  }
  return (
    <div className={styles.panel}>
      {checks}
      <ExpectationReview state={state} onSelect={onSelect} {...actions} />
      {check && (
        <p className={styles.detail}><Message text={"Revision <0/>: <1/> to the runtime; <2/> of <3/> cases agree with their expectations. Rehearsal only; no decision was recorded."} slots={[latest?.revision, check.valid ? msg("valid") : msg("invalid"), check.cases.filter((c) => c.passed).length, check.cases.length]} /></p>
      )}
      <div className={styles.tableWrap}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th scope="col">{msg("Case")}</th>
              <th scope="col">{msg("Expected")}</th>
              <th scope="col">{msg("Runtime answered")}</th>
              <th scope="col">{msg("Result")}</th>
              <th scope="col">{msg("Expectation from")}</th>
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
                    <td>{result === undefined ? msg("not yet checked") : result.refused ? msg("refused: {{value0}}", { value0: result.refused }) : (actual?.outcomeId ?? actual?.kind ?? '—')}</td>
                    <td>
                      {result === undefined ? (
                        <span className={styles.badge}>{msg("pending")}</span>
                      ) : (
                        <span className={styles.badge} data-state={result.passed ? 'passed' : 'disagrees'}>
                          {result.passed ? msg("agrees") : msg("disagrees")}
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
                        <Disclosure title={msg("What {{value0}} disagrees about", { value0: row.id })}>
                          <CheckedSide side={result.expected} label={msg("Expected")} />
                          <CheckedSide side={result.actual} label={msg("Runtime")} />
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
        <Disclosure title={msg("{{value0}} proposed case(s) were not admitted", { value0: state.droppedCases.length })}>
          <ul className={styles.unknowns}>
            {state.droppedCases.map((dropped) => (
              <li key={dropped.id}>
                {dropped.id}: {systemMessage(dropped.reason)}
              </li>
            ))}
          </ul>
        </Disclosure>
      )}
    </div>
  )
}

export function DraftPanel({ state, onSelect, onViewLogic, onViewSources }: { state: RunState; onSelect: (next: Selection) => void; onViewLogic?: () => void; onViewSources?: () => void }) {
  useLocale()
  const latest = state.candidates.at(-1)
  if (!latest) return <p className={styles.empty}>{msg("No draft yet.")}</p>
  const document = latest.document
  const rules = isRecord(document) && Array.isArray(document.rules) ? document.rules.filter(isRecord) : []
  return (
    <div className={styles.panel} data-overview>
      {isRecord(document) && <PackQuestion document={document as unknown as PackDocument} />}
      {isRecord(document) && <PackOverview document={document as unknown as PackDocument} onViewLogic={onViewLogic} onViewSources={onViewSources} />}
      {rules.length > 0 && (
        <section className={styles.section} aria-label={msg("Rules and their sources")}>
          <h3>{msg("Rules and their sources")}</h3>
          <ul className={styles.list}>
            {rules.map((rule, index) => {
              const id = typeof rule.id === 'string' ? rule.id : `#${index}`
              const refs = Array.isArray(rule.sourceRefs) ? (rule.sourceRefs as string[]) : []
              return (
                <li key={id} className={styles.row}>
                  <button type="button" className={styles.rowButton} onClick={() => onSelect({ kind: 'rule', id })} aria-label={msg("Inspect rule {{value0}}", { value0: id })}>
                    <div className={styles.rowHead}>
                      <strong>{id}</strong>
                      <span className={styles.badge}>{refs.length === 0 ? msg("no source") : msg("{{count}} source references", { count: refs.length })}</span>
                    </div>
                    <div className={styles.url}>{typeof rule.description === 'string' ? rule.description : ''}</div>
                  </button>
                </li>
              )
            })}
          </ul>
        </section>
      )}
      <Disclosure title={msg("Full document (JSON)")}>
        <p className={styles.detail}><Message text={"Produced by <0/> · sha256 <1/>"} slots={[latest.producedBy, latest.digest]} /></p>
        <CodeBlock text={latest.text} label={msg("Pack JSON")} />
      </Disclosure>
    </div>
  )
}

export function ReviewPanel({ state, sources, onCreate, onSelect, showCreateAction = true, mode = 'research' }: { testsPanel?: ReactNode; onTabChange?: (tab: string) => void; hideHeader?: boolean; onSelectInMain?: (next: Selection) => void; showCreateAction?: boolean; mode?: 'draft' | 'research'; state: RunState; sources: readonly SourceRecord[]; onCreate: () => void; onSelect: (next: Selection) => void }) {
  useLocale()
  const latest = state.candidates.at(-1)
  const check = latest?.check
  const passing = mode === 'research' ? canCreateResearchDraft(state) : state.status === 'ready' && !state.restored && check?.valid === true && check.documentDigest === latest?.digest
  const verified = sources.filter((s) => s.verification.state === 'verified').length
  const failed = sources.filter((s) => s.verification.state === 'failed' || s.failure !== null).length
  const untraced = state.citations.filter((c) => !c.traced)
  return (
    <div className={styles.panel}>
      <section className={styles.section}>
        <h3>{msg("Where this stands")}</h3>
        <p className={styles.detail}>{systemMessage(state.detail) || msg("Not started.")}</p>
        <dl className={styles.facts}>
          <dt>{msg("Revisions")}</dt>
          <dd>{msg("{{revisions}} ({{count}} repairs)", { revisions: state.candidates.length, count: state.revisionsUsed })}</dd>
          <dt>{msg("Test cases")}</dt>
          <dd>{state.expectationIssues.some(issue => !issue.resolved) ? msg("Blocked by invalid expectations") : check?.cases.length ? msg("{{value0}} of {{value1}} agree", { value0: check.cases.filter((c) => c.passed).length, value1: check.cases.length }) : msg("Not run")}</dd>
          <dt>{msg("Sources")}</dt>
          <dd><Message text={"<0/> recorded; <1/> with verified receipts; <2/> failed or unverified"} slots={[sources.length, verified, failed]} /></dd>
          <dt>{msg("Citations")}</dt>
          <dd><Message text={"<0/> of <1/> traced to a recorded excerpt"} slots={[state.citations.length - untraced.length, state.citations.length]} /></dd>
        </dl>
      </section>
      <DraftComparison candidates={state.candidates} />
      {state.unknowns.length > 0 && (
        <section className={styles.section}>
          <h3>{msg("Assumptions and open questions")}</h3>
          <ul className={styles.unknowns}>
            {state.unknowns.map((line, index) => (
              <li key={index}>{line}</li>
            ))}
          </ul>
        </section>
      )}
      {untraced.length > 0 && (
        <section className={styles.section}>
          <h3>{msg("Citations that could not be traced")}</h3>
          <ul className={styles.unknowns}>
            {untraced.map((citation) => (
              <li key={citation.sourceId}>
                <strong>{citation.sourceId}</strong>: {systemMessage(citation.reason)}
                {citation.url ? ` (${citation.url})` : ''}
              </li>
            ))}
          </ul>
        </section>
      )}
      {state.citations.some((c) => c.traced) && (
        <section className={styles.section}>
          <h3>{msg("Traced citations")}</h3>
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
          <h3>{msg("Revisions")}</h3>
          <ul className={styles.unknowns}>
            {state.candidates.map((candidate) => (
              <li key={candidate.revision}><Message text={"Revision <0/> (<1/>):<2/><3/>"} slots={[candidate.revision, candidate.producedBy, ' ', candidate.check ? msg('{{status}}, {{passed}}/{{total}} agree', { status: candidate.check.valid ? 'valid' : 'invalid', passed: candidate.check.cases.filter(c => c.passed).length, total: candidate.check.cases.length }) : msg("not checked")]} /></li>
            ))}
          </ul>
        </section>
      )}
      <section className={styles.section}>
        <h3>{msg("Create")}</h3>
        <p className={styles.detail}>
          {passing
            ? mode === 'research' ? msg("Review the name and open questions, then create the pack with its checked cases and research record.") : msg("The runtime validated the structure. Review the draft before creating. Source research and behavioral tests have not been run.")
            : msg("Create is offered once every established case agrees with the runtime and the draft is valid.")}
        </p>
        <p className={styles.hint}>
          {mode === 'research' && <>{msg("A verified receipt establishes that the gateway signed these bytes and sealed the session; it does not establish that a page is true, current, legally authoritative, or that it came from the site its URL names.")}</>}
        </p>
        {showCreateAction && <div>
          <Button variant="primary" disabled={!passing || state.status === 'running'} onClick={onCreate}>{msg("Review and create")}</Button>
        </div>}
      </section>
    </div>
  )
}

function DraftLogic({ state, selection, onSelect, onInspect, active }: { state: RunState; selection: Selection; onSelect: (next: Selection) => void; onInspect: (next: Selection) => void; active: boolean }) {
  const locale = useLocale()
  const document = state.candidates.at(-1)?.document
  const model = useMemo(() => document && isRecord(document) ? projectLogic(document as unknown as PackDocument) : null, [document, locale])
  const [mode, setMode] = useState<LogicMode>('map')
  const [query, setQuery] = useState('')
  const [display, setDisplay] = useState(() => initialLogicDisplay(false))
  const [viewport, setViewport] = useState({ x: 0, y: 0, zoom: 1 })
  const [nodePositions, setNodePositions] = useState<NodePositions>({})
  const scroll = useRef(0)
  if (!model) return <p className={styles.empty}>{msg("No draft yet.")}</p>
  const select = (id: string) => onSelect({ kind: 'logic', id })
  return <div className={styles.logicPanel} data-mode={mode}><PackLogic active={active} model={model} at={selection?.kind === 'logic' ? selection.id : null} groupId={null}
    select={select} inspect={id => onInspect({ kind: 'logic', id })} mode={mode} onMode={setMode} query={query} onQuery={setQuery} display={display} onDisplay={setDisplay}
    viewport={viewport} onViewport={setViewport} nodePositions={nodePositions} onNodePositionsChange={setNodePositions} listScroll={scroll} /></div>
}

export function DraftTabs({ testsPanel, onTabChange, state, sources, selection, onSelect, onCreate, showCreateAction = true, mode = 'research', hideHeader = false, onSelectInMain, documents = [], files = [], onRead, ...actions }: ExpectationReviewActions & Omit<ComponentProps<typeof DraftSources>, 'document'> & { testsPanel?: ReactNode; onTabChange?: (tab: string) => void; hideHeader?: boolean; onSelectInMain?: (next: Selection) => void; showCreateAction?: boolean; mode?: 'draft' | 'research'; state: RunState; sources: readonly SourceRecord[]; selection: Selection; onSelect: (next: Selection) => void; onCreate: () => void }) {
  useLocale()
  const [tab, setTabState] = useState('draft')
  const setTab = (next: string) => { setTabState(next); onTabChange?.(next) }
  const pending = state.expectationIssues.filter(issue => !issue.resolved).length
  const total = state.cases.length + pending
  const document = state.candidates.at(-1)?.document
  const sourceCount = sources.length + documents.length + files.length + declaredSources(document).length
  return (
    <section className={styles.pane} aria-label={msg("Draft review")} data-pane="draft" data-document={hideHeader || undefined}>
      {!hideHeader && <header className={styles.paneHeader}>
        <span>{typeof (state.candidates.at(-1)?.document as { title?: unknown })?.title === 'string' ? (state.candidates.at(-1)!.document as { title: string }).title : msg("Draft")}</span>
        <span className={styles.status}>{state.candidates.length === 0 ? msg("no revision yet") : msg("revision {{value0}}", { value0: state.candidates.at(-1)!.revision })}</span>
      </header>}
      {pending > 0 && <div className={styles.panel} role="status"><span>{msg("{{count}} invalid expectations · testing paused", { count: pending })}</span><div><Button variant="quiet" onClick={() => setTab('tests')}>{msg("Review expectations")}</Button></div></div>}
      <Tabs
        scrollable keepMounted fillPanel={tab === 'tests' && testsPanel ? 'tests' : 'logic'}
        variant={hideHeader ? 'page' : 'pane'}
        label={msg("Draft views")}
        value={tab}
        onValueChange={setTab}
        tabs={[
          { value: 'draft', label: msg("Overview"), panel: <DraftPanel state={state} onSelect={onSelect} onViewLogic={() => setTab('logic')} onViewSources={() => setTab('sources')} /> },
          { value: 'logic', label: msg("Logic"), panel: <DraftLogic state={state} selection={selection} onSelect={onSelectInMain ?? onSelect} onInspect={onSelect} active={tab === 'logic'} /> },
          { value: 'sources', label: sourceCount ? msg('Sources ({{count}})', { count: sourceCount }) : msg('Sources'), panel: <div className={styles.panel}><DraftSources document={document} documents={documents} files={files} onRead={onRead} />{(sources.length > 0 || sourceCount === 0) && <SourcesPanel sources={sources} selection={selection} onSelect={onSelect} />}</div> },
          { value: 'tests', label: total ? msg('Tests ({{count}})', { count: total }) : msg('Tests'), panel: testsPanel ?? <TestsPanel mode={mode} state={state} onSelect={onSelect} {...actions} /> },
          { value: 'review', label: msg("Review"), panel: <ReviewPanel showCreateAction={showCreateAction} mode={mode} state={state} sources={sources} onCreate={onCreate} onSelect={onSelect} /> }
        ]}
      />
    </section>
  )
}
