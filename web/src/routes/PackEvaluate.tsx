import { useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { DispositionDiff } from '../components/DispositionDiff'
import { EvaluationRaw, EvaluationView } from '../components/EvaluationView'
import { Empty } from '../components/primitives'
import { RefusalPanel } from '../components/RefusalPanel'
import { useMcp } from '../mcp/McpProvider'
import { useEvaluate, usePacks } from '../mcp/queries'
import type { EvaluationRun, PackSummary } from '../mcp/types'
import { PageHeader, PageBody } from '../ui/PageLayout'
import { Button } from '../ui/Button'
import { Field, FieldGroup } from '../ui/Field'
import { TextArea } from '../ui/TextArea'
import { Tabs } from '../ui/Tabs'
import { PackNavigation, TestNavigation } from '../packs/PackWorkspace'
import { recordActivity } from '../shell/consoleLog'
import styles from './PackEvaluate.module.css'

type ResultTab = 'reading' | 'raw'

/**
 * Run one pack over documents the user supplies, and read the payload.
 *
 * The documents go over the wire as text, exactly as `experimental_evaluate`
 * takes them: the tool's `facts` and `evidence` arguments are JSON documents
 * and not paths, so the what-if loop needs nothing from the chassis — the page
 * edits the text and calls the tool again.
 *
 * Absence of an evidence document is the key omitted entirely. A key present
 * with an empty string is a *supplied* empty document, which is not a JSON text
 * and is refused as malformed-input, so the two are kept apart here rather than
 * collapsed into one empty box.
 */
export function PackEvaluate() {
  const { packId } = useParams<{ packId: string }>()
  const { status, rehearsalSupported, known: capabilitiesKnown } = useMcp()
  const { data: inventory } = usePacks()
  const evaluate = useEvaluate()

  const [facts, setFacts] = useState('{}')
  const [evidence, setEvidence] = useState('{}')
  const [evidenceSupplied, setEvidenceSupplied] = useState(false)
  const [history, setHistory] = useState<EvaluationRun[]>([])
  const [tab, setTab] = useState<ResultTab>('reading')
  const generation = useRef(0)
  useEffect(() => {
    generation.current += 1
    evaluate.reset()
    setFacts('{}'); setEvidence('{}'); setEvidenceSupplied(false); setHistory([])
    return () => { generation.current += 1 }
  }, [packId])

  const factsError = useMemo(() => jsonError(facts), [facts])
  const evidenceError = useMemo(
    () => (evidenceSupplied ? jsonError(evidence) : null),
    [evidence, evidenceSupplied]
  )

  const summary = inventory?.packs?.find((pack) => pack.id === packId)
  const current = history[history.length - 1]
  const previous = history[history.length - 2]
  const drifted =
    current !== undefined &&
    (current.facts !== facts ||
      current.evidence !== (evidenceSupplied ? evidence : undefined))

  const runnable =
    status === 'ready' && Boolean(packId) && factsError === null && evidenceError === null

  const run = () => {
    if (!packId || !runnable || evaluate.isPending) return
    const ticket = generation.current
    recordActivity('Running pack evaluation…')
    evaluate.mutate(
      { source: 'pack_id', packId, facts, evidence: evidenceSupplied ? evidence : undefined },
      {
        onSuccess: (completed) => {
          recordActivity('Pack evaluation completed. Results are available in Test.')
          if (ticket !== generation.current) return
          setHistory((runs) => [...runs, completed])
          // Keep edits made while this request was in flight. The result is
          // bound to completed.facts/evidence; drifted labels the difference.
          setTab('reading')
        },
        onError: () => recordActivity('Pack evaluation failed. See Test for the runtime response.')
      }
    )
  }

  const revert = () => {
    if (!current) return
    setFacts(current.facts)
    setEvidenceSupplied(current.evidence !== undefined)
    if (current.evidence !== undefined) setEvidence(current.evidence)
  }

  return (
    <article data-measure="wide" data-layout="page">
      <PageHeader title="Packs" context={packId} />
      <PackNavigation packId={packId ?? ''} current="test" />
      <PageBody width="wide">
      <div className={styles.workspace}>
      <TestNavigation packId={packId ?? ''} hasMatrix={Boolean(summary?.matrix || summary?.matrixPath)} />
      <details className={styles.notice}>
        <summary>{rehearsalSupported ? 'Rehearsal · runs the saved pack without appending an audit record.' : 'Runtime behavior · this run may append an audit record.'}</summary>
        <p className="note note-warn">
          <strong>Experimental surface.</strong> This runs the runtime's
          <code> experimental_evaluate</code> tool, which may change or be removed
          without a compatibility promise. It authorizes nothing and executes
          nothing.{' '}
          {rehearsalSupported ? (
            <>
              Every run here is declared a rehearsal (ADR-0028): the evaluation
              is identical, no audit record is appended, no reviewed set is
              consulted, and the payload carries the label.
            </>
          ) : capabilitiesKnown ? (
            <>
              This runtime predates the rehearsal declaration (jpack 0.18.0), so
              in a project whose <code>jpack.json</code> declares an audit
              directory, each completed run appends one record to it.
            </>
          ) : (
            <>
              This desk could not read the runtime's tool listing, so whether it
              accepts the rehearsal declaration is unknown rather than known to
              be no. Runs are sent without it, which means that in a project
              whose <code>jpack.json</code> declares an audit directory, each
              completed run may append one record to it.
            </>
          )}
        </p>
      </details>
      <div className={styles.columns}>
      <section className={styles.inputs} aria-label="Test inputs">
        <h2>Try inputs</h2>
        <p className="quiet">Supply facts to explore an outcome. This is an exploratory run; a pass or fail requires saved expectations.</p>
        <PackReference summary={summary} />
        <FieldGroup>
        <Field label="Facts" error={factsError} hint="JSON values at the fact paths used by the pack. Omitted values remain unknown.">
          {(wiring) => <TextArea {...wiring} rows={7} value={facts}
            spellCheck={false} onChange={(event) => setFacts(event.target.value)} />}
        </Field>

        <div className="editor">
          <label className="checkbox">
            <input
              type="checkbox"
              checked={evidenceSupplied}
              onChange={(event) => setEvidenceSupplied(event.target.checked)}
            />
            <span>
              Supply an evidence document. Unchecked, the key is omitted entirely
              and every declared requirement is unknown.
            </span>
          </label>
          {evidenceSupplied && (
            <>
              <label htmlFor="evidence-editor">
                <strong>Evidence</strong> — requirement id to{' '}
                <code>present</code>, <code>absent</code>, or <code>unknown</code>.
              </label>
              <TextArea
                id="evidence-editor"
                aria-invalid={Boolean(evidenceError)}
                aria-describedby="evidence-status"
                spellCheck={false}
                rows={6}
                value={evidence}
                onChange={(event) => setEvidence(event.target.value)}
              />
              <p id="evidence-status" className={evidenceError ? 'editor-status editor-status-bad' : 'editor-status'}>
                {evidenceError ?? 'valid JSON'}
              </p>
            </>
          )}
        </div>

        </FieldGroup>
        <div className={styles.actions}>
          <Button variant="primary" disabled={!runnable || evaluate.isPending} onClick={run}>
            {evaluate.isPending
              ? 'Evaluating…'
              : history.length === 0
                ? 'Run evaluation'
                : 'Re-evaluate'}
          </Button>
          {drifted && (
            <Button variant="quiet" onClick={revert}>
              Restore last run inputs
            </Button>
          )}
          {status !== 'ready' && <span className="quiet">waiting for the runtime connection</span>}
        </div>
      </section>
      <section className={styles.results} aria-label="Test results">
      <h2>Result</h2>
      {drifted && <p className={styles.stale} role="status">Inputs changed since this result. Run again to evaluate the current inputs.</p>}
      {evaluate.isPending && <p role="status">Evaluating the submitted inputs…</p>}
      {evaluate.error && <RefusalPanel error={evaluate.error} />}

      {current ? (
        <>
          <p className="meta">
            <span>
              run {history.length} of this page{drifted ? '; the editors have moved since' : ''}
            </span>
          </p>
          <Tabs label="Result view" value={tab} onValueChange={(next) => setTab(next as ResultTab)} tabs={[
            { value: 'reading', label: 'Outcome & trace', panel: <>
              {previous && <DispositionDiff previous={previous.payload} current={current.payload} />}
              <EvaluationView payload={current.payload} />
            </> },
            { value: 'raw', label: 'Raw response', panel: <EvaluationRaw raw={current.raw} /> }
          ]} />
        </>
      ) : (
        !evaluate.isPending && (
          <Empty>
            No evaluation yet. Supply a facts document and run one — the pack, the
            facts, and the evidence are the whole input.
          </Empty>
        )
      )}
      </section>
      </div>
      </div>
      </PageBody>
    </article>
  )
}

/**
 * What the pack reads, as the runtime's inventory reports it: the fact pointers
 * its conditions consult and the ids of the evidence it declares. It is a
 * reference for writing the documents beside it, not a template — the values
 * are the author's to supply, and a value nobody can source is better left out
 * so the pack escalates than invented so it decides.
 */
function PackReference({ summary }: { summary?: PackSummary }) {
  if (!summary) return null
  const facts = summary.consultedFactPaths ?? []
  const evidence = summary.evidenceRequirements ?? []
  if (facts.length === 0 && evidence.length === 0) return null
  return (
    <div className="card">
      {facts.length > 0 && (
        <p className="reference">
          <span className="reference-label">Consulted fact paths</span>
          {facts.map((path) => (
            <code key={path} className="id">
              {path}
            </code>
          ))}
        </p>
      )}
      {evidence.length > 0 && (
        <p className="reference">
          <span className="reference-label">Evidence requirements</span>
          {evidence.map((id) => (
            <code key={id} className="id">
              {id}
            </code>
          ))}
        </p>
      )}
    </div>
  )
}

function jsonError(text: string): string | null {
  try {
    JSON.parse(text)
    return null
  } catch (cause) {
    return cause instanceof Error ? cause.message : String(cause)
  }
}
