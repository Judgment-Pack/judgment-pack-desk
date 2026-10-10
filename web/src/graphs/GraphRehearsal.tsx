import { useQuery } from '@tanstack/react-query'
import { Field } from '../ui/Field'
import { TextArea } from '../ui/TextArea'
import { Disclosure } from '../ui/Disclosure'
import { compareGraphRevision, hasGraphProvenance, type GraphProvenance } from './provenance'
import styles from './GraphWorkspace.module.css'
import { useEffect, useRef, useState } from 'react'
import { msg, useLocale } from '../i18n'
import { arrayItemsBytes, memberBytes } from '../admin/memberBytes'
import { deskFetch } from '../files/client'
import { Button } from '../ui/Button'
import { GraphLabels } from './GraphLabels'
import type { GraphLabels as Labels } from './client'

const INPUT_LIMIT = 4 * 1024 * 1024

export async function rehearseGraph(id: string, inputs: string, signal?: AbortSignal) {
  if (new TextEncoder().encode(inputs).length > INPUT_LIMIT) throw new Error(msg('The rehearsal inputs exceed 4 MiB.'))
  let parsed: unknown
  try { parsed = JSON.parse(inputs) } catch { /* Refused below. */ }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error(msg('The rehearsal inputs must be a JSON object keyed by node id.'))
  const response = await deskFetch(`/api/graphs/evaluate?id=${encodeURIComponent(id)}`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: inputs, signal,
  })
  const envelope = await response.text()
  const body = JSON.parse(envelope)
  const raw = memberBytes(envelope, 'answer')
  if (!response.ok) throw new Error(body.error ?? msg('The rehearsal could not be run.'))
  const answer = body.answer
  if (raw === undefined || answer?.command !== 'experimental graph evaluate' || typeof answer.status !== 'string' || answer.rehearsal !== true) {
    throw new Error(msg('The runtime did not return rehearsal: true.'))
  }
  // Keep the answer text: JSON.stringify would change numbers and escapes in
  // dispositions. No document is joined to this answer.
  return { raw, answer: answer as Labels & GraphProvenance & { status: string } }
}

export function GraphRehearsal({ graphId }: { graphId: string }) {
  useLocale()
  const [inputs, setInputs] = useState('{}')
  const [result, setResult] = useState<Awaited<ReturnType<typeof rehearseGraph>>>()
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const active = useRef<AbortController | null>(null)
  useEffect(() => () => { active.current?.abort() }, [])
  const run = async () => {
    if (busy) return
    const controller = new AbortController()
    active.current = controller
    setBusy(true); setResult(undefined); setError(undefined)
    try {
      const next = await rehearseGraph(graphId, inputs, controller.signal)
      if (!controller.signal.aborted) setResult(next)
    } catch (cause) {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : msg('The rehearsal could not be run.'))
    } finally {
      if (!controller.signal.aborted) setBusy(false)
    }
  }
  return <section className={styles.workspace} aria-label={msg('Graph rehearsal')}>
    <h2>{msg('Rehearsal')}</h2>
    <p className={styles.hint}>{msg('Try supplied facts against this graph. Every node runs; a rehearsal creates no decision audit record and takes no external action.')}</p>
    <Disclosure title={msg('About rehearsal')}>
      <p>{msg('The runtime evaluates the supplied inputs. A matching byte revision does not establish that the policy or facts are correct, or that the project was reviewed.')}</p>
      <p>{msg('Inputs and results on this page last until you leave. Rehearsals requested in chat are retained with the conversation and sent to its selected model provider.')}</p>
    </Disclosure>
    <Field label={msg('Inputs by node id')} hint={msg('Enter a JSON object keyed by node id, each entry with optional facts and evidence members. Maximum 4 MiB.')}>{w =>
      <TextArea {...w} value={inputs} disabled={busy} spellCheck={false} rows={12} onChange={event => { setInputs(event.target.value); setResult(undefined); setError(undefined) }}/>
    }</Field>
    <Button onClick={() => void run()} disabled={busy}>{busy ? msg('Rehearsing…') : msg('Rehearse')}</Button>
    {error && <pre role="alert">{error}</pre>}
    {result && <>
      {hasGraphProvenance(result.answer) ? <RevisionCheck graphId={graphId} answer={result.answer}/> : <p className={styles.hint}>{msg("This runtime did not identify all evaluated file revisions. Results are shown separately from the current graph.")}</p>}
      <Disclosure title={msg("Runtime details")}><GraphLabels labels={result.answer} /></Disclosure>
      <p lang="en">{result.answer.status}</p>
      <RehearsalMembers raw={result.raw} />
      <pre lang="en">{result.raw}</pre>
    </>}
  </section>
}

// Member names and values belong to the runtime. Keep numbers, escaping,
// whitespace and order within each value exactly as the runtime printed them.
function RehearsalMembers({ raw }: { raw: string }) {
  const names = { disposition: msg('disposition'), node: msg('node'), factFeeds: msg('factFeeds'), evidenceFeeds: msg('evidenceFeeds'), trace: msg('trace'), handoffs: msg('handoffs') }
  const member = (source: string, name: 'disposition' | 'node' | 'factFeeds' | 'evidenceFeeds' | 'trace' | 'handoffs') => {
    const value = memberBytes(source, name)
    return value === undefined ? null : <li key={name} style={{ whiteSpace: 'pre-wrap' }}><span>{names[name]}</span>: <code lang="en">{value}</code></li>
  }
  const nodes = memberBytes(raw, 'nodes')
  return <ul>
    {member(raw, 'disposition')}
    {nodes !== undefined && <li><span>{msg('nodes')}</span><ul>
      {arrayItemsBytes(nodes).map((node, index) => <li key={index}><ul>
        {member(node, 'node')}
        {member(node, 'disposition')}
        {member(node, 'factFeeds')}
        {member(node, 'evidenceFeeds')}
        {member(node, 'trace')}
      </ul></li>)}
    </ul></li>}
    {member(raw, 'handoffs')}
  </ul>
}

function RevisionCheck({graphId, answer}: {graphId: string; answer: GraphProvenance}) {
  const check = useQuery({queryKey: ['desk-graph-revision', graphId, answer], queryFn: ({signal}) => compareGraphRevision(graphId, answer, signal), retry: false, refetchOnWindowFocus: true})
  return <div className={`${styles.feedback} ${styles.toolbar}`} role="status">
    <span>{check.isPending ? msg('Checking evaluated file revisions…') : check.isError ? msg('Current file revisions could not be checked.') : check.data === 'matching' ? msg('The evaluated graph, configuration and packs match the files just read.') : msg('The project has changed since this rehearsal. Run again to test the current files.')}</span>
    <Button variant="quiet" disabled={check.isFetching} onClick={() => void check.refetch()}>{msg('Check revisions')}</Button>
  </div>
}
