import { useEffect, useRef, useState } from 'react'
import { msg, useLocale } from '../i18n'
import { memberBytes } from '../admin/memberBytes'
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
  return { raw, answer: answer as Labels & { status: string } }
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
  return <section aria-label={msg('Graph rehearsal')}>
    <h2>{msg('Rehearsal')}</h2>
    <p>{msg('A rehearsal shows the runtime’s experimental answer for the facts the owner typed. It does not establish a decision, a record in the trail, a reviewed set consulted, which bytes were read, or that the facts are true.')}</p>
    <p>{msg('Neither the plan nor a rehearsal carries a digest of the graph document, so each is shown as its own answer, beside the document, and never joined to it. artifact.bundleDigest is the runtime’s bundle’s digest, not the graph’s.')}</p>
    <p>{msg('The trail is silent about rehearsals. Desk keeps neither the inputs nor the result: they live on the page until it is left.')}</p>
    <label>{msg('Inputs by node id')}
      <textarea value={inputs} disabled={busy} spellCheck={false} rows={12} onChange={event => { setInputs(event.target.value); setResult(undefined); setError(undefined) }} />
    </label>
    <p className="quiet">{msg('Enter a JSON object keyed by node id, each entry with optional facts and evidence members. Maximum 4 MiB.')}</p>
    <Button onClick={() => void run()} disabled={busy}>{busy ? msg('Rehearsing…') : msg('Rehearse')}</Button>
    {error && <pre role="alert" lang="en">{error}</pre>}
    {result && <>
      <GraphLabels labels={result.answer} />
      <p lang="en">{result.answer.status}</p>
      <pre lang="en">{result.raw}</pre>
    </>}
  </section>
}
