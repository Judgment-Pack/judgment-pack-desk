import { useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { DispositionDiff } from '../components/DispositionDiff'
import { EvaluationRaw, EvaluationView } from '../components/EvaluationView'
import { Empty, ErrorBox, Pill, Section } from '../components/primitives'
import { useMcp } from '../mcp/McpProvider'
import { ToolRefusal, useEvaluate, usePacks } from '../mcp/queries'
import type { EvaluationRun, PackSummary } from '../mcp/types'

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
  const { status } = useMcp()
  const { data: inventory } = usePacks()
  const evaluate = useEvaluate()

  const [facts, setFacts] = useState('{}')
  const [evidence, setEvidence] = useState('{}')
  const [evidenceSupplied, setEvidenceSupplied] = useState(false)
  const [history, setHistory] = useState<EvaluationRun[]>([])
  const [tab, setTab] = useState<ResultTab>('reading')

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
    if (!packId || !runnable) return
    evaluate.mutate(
      { packId, facts, evidence: evidenceSupplied ? evidence : undefined },
      {
        onSuccess: (completed) => {
          setHistory((runs) => [...runs, completed])
          // The editors hold the documents that produced what is on screen, so
          // the next what-if starts from the run being read rather than from
          // whatever was typed after it.
          setFacts(completed.facts)
          if (completed.evidence !== undefined) setEvidence(completed.evidence)
          setTab('reading')
        }
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
    <article className="detail">
      <nav className="crumbs">
        <Link to="/">Packs</Link>
        <span aria-hidden="true">/</span>
        <Link to={`/packs/${encodeURIComponent(packId ?? '')}`}>{packId}</Link>
        <span aria-hidden="true">/</span>
        <span>Evaluate</span>
      </nav>

      <header className="detail-head">
        <h1>Evaluate {packId}</h1>
        <p className="note note-warn">
          <strong>Experimental surface.</strong> This runs the runtime's
          <code> experimental_evaluate</code> tool, which may change or be removed
          without a compatibility promise. It authorizes nothing and executes
          nothing — and in a project whose <code>jpack.json</code> declares an
          audit directory, each completed run appends one record to it.
        </p>
      </header>

      <Section title="Documents">
        <PackReference summary={summary} />
        <div className="editor">
          <label htmlFor="facts-editor">
            <strong>Facts</strong> — one JSON document; the pack's{' '}
            <code>fact.path</code> pointers descend into it.
          </label>
          <textarea
            id="facts-editor"
            className={factsError ? 'code-editor code-editor-bad' : 'code-editor'}
            spellCheck={false}
            rows={14}
            value={facts}
            onChange={(event) => setFacts(event.target.value)}
          />
          <p className={factsError ? 'editor-status editor-status-bad' : 'editor-status'}>
            {factsError ?? 'valid JSON'}
          </p>
        </div>

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
              <textarea
                id="evidence-editor"
                className={evidenceError ? 'code-editor code-editor-bad' : 'code-editor'}
                spellCheck={false}
                rows={6}
                value={evidence}
                onChange={(event) => setEvidence(event.target.value)}
              />
              <p className={evidenceError ? 'editor-status editor-status-bad' : 'editor-status'}>
                {evidenceError ?? 'valid JSON'}
              </p>
            </>
          )}
        </div>

        <div className="actions">
          <button type="button" className="button" disabled={!runnable || evaluate.isPending} onClick={run}>
            {evaluate.isPending
              ? 'Evaluating…'
              : history.length === 0
                ? 'Run evaluation'
                : 'Re-evaluate'}
          </button>
          {drifted && (
            <button type="button" className="link-button" onClick={revert}>
              Revert to the documents of the last run
            </button>
          )}
          {status !== 'ready' && <span className="quiet">waiting for the runtime connection</span>}
        </div>
      </Section>

      {evaluate.error && <RefusalPanel error={evaluate.error} />}

      {current ? (
        <>
          <div className="tabs" role="tablist">
            <TabButton current={tab} value="reading" onSelect={setTab}>
              Payload
            </TabButton>
            <TabButton current={tab} value="raw" onSelect={setTab}>
              Raw JSON
            </TabButton>
          </div>
          <p className="meta">
            <span>
              run {history.length} of this page{drifted ? '; the editors have moved since' : ''}
            </span>
          </p>
          {tab === 'reading' ? (
            <>
              {previous && <DispositionDiff previous={previous.payload} current={current.payload} />}
              <EvaluationView payload={current.payload} />
            </>
          ) : (
            <EvaluationRaw raw={current.raw} />
          )}
        </>
      ) : (
        !evaluate.isPending && (
          <Empty>
            No evaluation yet. Supply a facts document and run one — the pack, the
            facts, and the evidence are the whole input.
          </Empty>
        )
      )}
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

/**
 * A refused evaluation. It carries no disposition at all, so this panel reports
 * only the §8.4 class and phase the runtime assigned and the message it wrote —
 * never a substitute answer.
 */
function RefusalPanel({ error }: { error: Error }) {
  const envelope = error instanceof ToolRefusal ? error.envelope : undefined
  const evaluationError = envelope?.evaluationError
  return (
    <Section title="Refused">
      <ErrorBox title="The runtime refused this evaluation" error={error} />
      {evaluationError && (
        <p className="meta">
          <Pill tone="strong">class: {evaluationError.class}</Pill>
          <Pill>phase: {evaluationError.phase}</Pill>
          <Pill tone="quiet">evaluator {evaluationError.evaluatorSpecVersion}</Pill>
        </p>
      )}
      {envelope?.diagnostics?.length ? (
        <ul className="cards">
          {envelope.diagnostics.map((diagnostic, index) => (
            <li className="card" key={`${diagnostic.code ?? 'diagnostic'}:${index}`}>
              <div className="card-head">
                {diagnostic.code && <code className="id">{diagnostic.code}</code>}
                {diagnostic.severity && <Pill tone="quiet">{diagnostic.severity}</Pill>}
                {diagnostic.codeStability && <Pill tone="quiet">{diagnostic.codeStability}</Pill>}
              </div>
              {diagnostic.message && <p>{diagnostic.message}</p>}
            </li>
          ))}
        </ul>
      ) : null}
      <p className="note">A refusal carries no disposition. Nothing above is an answer.</p>
    </Section>
  )
}

function TabButton({
  current,
  value,
  onSelect,
  children
}: {
  current: ResultTab
  value: ResultTab
  onSelect: (tab: ResultTab) => void
  children: string
}) {
  const selected = current === value
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      className={selected ? 'tab tab-on' : 'tab'}
      onClick={() => onSelect(value)}
    >
      {children}
    </button>
  )
}

/** The parse failure, or null when the text is JSON. */
function jsonError(text: string): string | null {
  try {
    JSON.parse(text)
    return null
  } catch (cause) {
    return cause instanceof Error ? cause.message : String(cause)
  }
}
