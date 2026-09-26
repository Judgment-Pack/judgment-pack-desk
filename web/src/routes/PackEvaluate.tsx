import { sourceMessage } from '../i18n/source'
import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { PACK_TERMS } from '../packs/terminology'
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { DispositionDiff } from '../components/DispositionDiff'
import { EvaluationRaw, EvaluationView } from '../components/EvaluationView'
import { Empty } from '../components/primitives'
import { RefusalPanel } from '../components/RefusalPanel'
import { useMcp } from '../mcp/McpProvider'
import { useEvaluate, usePacks, usePack } from '../mcp/queries'
import type { EvaluationRun, PackSummary } from '../mcp/types'
import { PageBody } from '../ui/PageLayout'
import { Button } from '../ui/Button'
import { Field, FieldGroup } from '../ui/Field'
import { TextArea } from '../ui/TextArea'
import { Tabs } from '../ui/Tabs'
import { PackHeader, PackQuestion, TestNavigation } from '../packs/PackWorkspace'
import { recordActivity } from '../shell/consoleLog'
import { publishPackRun, usePackRun, type PackRunSnapshot } from '../packs/runContext'
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
  useLocale()
  const { packId } = useParams<{ packId: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const pack = usePack(packId)
  const explanation = usePackRun(packId)
  const { status, rehearsalSupported, known: capabilitiesKnown } = useMcp()
  const { data: inventory } = usePacks()
  const evaluate = useEvaluate()

  const [facts, setFacts] = useState('{}')
  const [evidence, setEvidence] = useState('{}')
  const [evidenceSupplied, setEvidenceSupplied] = useState(false)
  const [history, setHistory] = useState<(EvaluationRun & { packBytes?: string })[]>([])
  const [tab, setTab] = useState<ResultTab>('reading')
  const generation = useRef(0)
  useEffect(() => {
    generation.current += 1
    evaluate.reset()
    const saved = explanation.data?.packId === packId ? explanation.data : undefined
    setFacts(saved?.run.facts ?? '{}'); setEvidence(saved?.run.evidence ?? '{}')
    setEvidenceSupplied(saved?.run.evidence !== undefined)
    setHistory(saved ? [{ ...saved.run, packBytes: saved.packBytes }] : [])
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
    const packBytes = pack.data?.raw
    recordActivity(sourceMessage('Running pack evaluation…'))
    evaluate.mutate(
      { ...(packBytes === undefined ? { source: 'pack_id' as const, packId } : { source: 'pack' as const, pack: packBytes }), facts, evidence: evidenceSupplied ? evidence : undefined },
      {
        onSuccess: (completed) => {
          recordActivity(sourceMessage('Pack evaluation completed. Results are available in Test.'))
          if (ticket !== generation.current) return
          setHistory((runs) => [...runs, { ...completed, packBytes }])
          // Keep edits made while this request was in flight. The result is
          // bound to completed.facts/evidence; drifted labels the difference.
          setTab('reading')
        },
        onError: () => recordActivity(sourceMessage('Pack evaluation failed. See Test for the runtime response.'))
      }
    )
  }

  const explain = () => {
    if (!current || !packId || current.packBytes === undefined) return
    const snapshot: PackRunSnapshot = { id: crypto.randomUUID(), packId, packBytes: current.packBytes, run: current }
    publishPackRun(queryClient, snapshot)
    navigate(`/packs/${encodeURIComponent(packId)}?view=logic&layout=map&run=${encodeURIComponent(snapshot.id)}`)
  }

  const revert = () => {
    if (!current) return
    setFacts(current.facts)
    setEvidenceSupplied(current.evidence !== undefined)
    if (current.evidence !== undefined) setEvidence(current.evidence)
  }

  return (
    <article data-layout="page">
      <PackHeader packId={packId ?? ''} document={pack.data?.document} current="test" />
      <PageBody width="wide">
      <PackQuestion document={pack.data?.document} />
      <div className={styles.workspace}>
      <TestNavigation packId={packId ?? ''} hasMatrix={Boolean(summary?.matrix || summary?.matrixPath)} />
      <details className={styles.notice}>
        <summary>{rehearsalSupported ? msg("Rehearsal · evaluates the loaded pack snapshot without appending an audit record.") : msg("Runtime behavior · this run may append an audit record.")}</summary>
        <p className="note note-warn"><Message text={"<0/> This runs the runtime's<1/> tool, which may change or be removed without a compatibility promise. It authorizes nothing and executes nothing.<2/><3/>"} slots={[<strong>{msg("Experimental surface.")}</strong>, <code>{msg("experimental_evaluate")}</code>, ' ', rehearsalSupported ? (
            <>{msg("Every run here is declared a rehearsal (ADR-0028): the evaluation is identical, no audit record is appended, no reviewed set is consulted, and the payload carries the label.")}</>
          ) : capabilitiesKnown ? (
            <><Message text={"This runtime predates the rehearsal declaration (jpack 0.18.0), so in a project whose <0/> declares an audit directory, each completed run appends one record to it."} slots={[<code>{msg("jpack.json")}</code>]} /></>
          ) : (
            <><Message text={"This desk could not read the runtime's tool listing, so whether it accepts the rehearsal declaration is unknown rather than known to be no. Runs are sent without it, which means that in a project whose <0/> declares an audit directory, each completed run may append one record to it."} slots={[<code>{msg("jpack.json")}</code>]} /></>
          )]} /></p>
      </details>
      <div className={styles.columns}>
      <section className={styles.inputs} aria-label={msg("Test inputs")}>
        <h2>{msg("Try inputs")}</h2>
        <p className="quiet">{msg("Supply facts to explore an outcome. This is an exploratory run; a pass or fail requires saved expectations.")}</p>
        <PackReference summary={summary} />
        <FieldGroup>
        <Field label={msg("Facts")} error={factsError} hint={msg("JSON values at the fact paths used by the pack. Omitted values remain unknown.")}>
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
            <span>{msg("Supply an evidence document. Unchecked, the key is omitted entirely and every declared requirement is unknown.")}</span>
          </label>
          {evidenceSupplied && (
            <>
              <label htmlFor="evidence-editor"><Message text={"<0/> — requirement id to<1/><2/>, <3/>, or <4/>."} slots={[<strong>{msg("Evidence")}</strong>, ' ', <code>{msg("present")}</code>, <code>{msg("absent")}</code>, <code>{msg("unknown")}</code>]} /></label>
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
                {evidenceError ?? msg("valid JSON")}
              </p>
            </>
          )}
        </div>

        </FieldGroup>
        <div className={styles.actions}>
          <Button variant="primary" disabled={!runnable || evaluate.isPending} onClick={run}>
            {evaluate.isPending
              ? msg("Evaluating…")
              : history.length === 0
                ? msg("Run evaluation")
                : msg("Re-evaluate")}
          </Button>
          {drifted && (
            <Button variant="quiet" onClick={revert}>{msg("Restore last run inputs")}</Button>
          )}
          {status !== 'ready' && <span className="quiet">{msg("waiting for the runtime connection")}</span>}
        </div>
      </section>
      <section className={styles.results} aria-label={msg("Test results")}>
      <h2>{msg("Result")}</h2>
      {current && <Button onClick={explain} disabled={current.packBytes === undefined}>{msg("Explain on map")}</Button>}
      {current && current.packBytes === undefined && <p className="quiet">{msg("This result has no captured pack revision, so a trace cannot be attached to the map.")}</p>}
      {drifted && <p className={styles.stale} role="status">{msg("Inputs changed since this result. Run again to evaluate the current inputs.")}</p>}
      {evaluate.isPending && <p role="status">{msg("Evaluating the submitted inputs…")}</p>}
      {evaluate.error && <RefusalPanel error={evaluate.error} />}

      {current ? (
        <>
          <p className="meta">
            <span><Message text={"run <0/> of this page<1/>"} slots={[history.length, drifted ? msg("; the editors have moved since") : '']} /></span>
          </p>
          <Tabs label={msg("Result view")} value={tab} onValueChange={(next) => setTab(next as ResultTab)} tabs={[
            { value: 'reading', label: msg("Outcome & trace"), panel: <>
              {previous && <DispositionDiff previous={previous.payload} current={current.payload} />}
              <EvaluationView payload={current.payload} />
            </> },
            { value: 'raw', label: msg("Raw response"), panel: <EvaluationRaw raw={current.raw} /> }
          ]} />
        </>
      ) : (
        !evaluate.isPending && (
          <Empty>{msg("No evaluation yet. Supply a facts document and run one — the pack, the facts, and the evidence are the whole input.")}</Empty>
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
  useLocale()
  if (!summary) return null
  const facts = summary.consultedFactPaths ?? []
  const evidence = summary.evidenceRequirements ?? []
  if (facts.length === 0 && evidence.length === 0) return null
  return (
    <div className="card">
      {facts.length > 0 && (
        <p className="reference">
          <span className="reference-label">{msg("Consulted fact paths")}</span>
          {facts.map((path) => (
            <code key={path} className="id">
              {path}
            </code>
          ))}
        </p>
      )}
      {evidence.length > 0 && (
        <p className="reference">
          <span className="reference-label">{PACK_TERMS.evidenceRequirements.label}</span>
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
