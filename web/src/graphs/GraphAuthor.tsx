import { useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { useMcp } from '../mcp/McpProvider'
import { AUTHOR_GRAPH_PROMPT, usePromptNames } from '../mcp/prompts'
import { readFile } from '../files/client'
import { useFileContent } from '../files/queries'
import { assistantReady, useAssistantSlot } from '../assistant/useAssistantSlot'
import { selectedAssistant } from '../assistant/target'
import { useAssistantRun } from '../assistant/useAssistantRun'
import { ProposalDiffView } from '../assistant/ProposalDiffView'
import { diffProposal } from '../assistant/proposalDiff'
import { Button } from '../ui/Button'
import { GraphLabels } from './GraphLabels'
import { shownMessage } from './client'
import { graphHostTools, graphPost, type GraphOffer } from './author'

/** Model events only fill a proposal. Preview and confirmation are owner actions. */
export function GraphAuthor({ graphId }: { graphId?: string }) {
  useLocale()
  const names = usePromptNames()
  if (names.isPending) return <p>{msg('Reading runtime authoring prompts…')}</p>
  if (!names.data?.includes(AUTHOR_GRAPH_PROMPT)) return <p>{msg('This runtime advertises no author_graph prompt.')}</p>
  return <GraphAuthorReady graphId={graphId} />
}

function GraphAuthorReady({ graphId }: { graphId?: string }) {
  const slot = useAssistantSlot()
  if (!assistantReady(slot)) return <p>{msg('Configure an assistant and choose a model to author a graph.')}</p>
  return <GraphAuthorSession key={graphId ?? 'new'} graphId={graphId} />
}

function GraphAuthorSession({ graphId }: { graphId?: string }) {
  const slot = useAssistantSlot()
  const selected = selectedAssistant(slot)
  const { client } = useMcp()
  const queries = useQueryClient()
  const config = useFileContent('jpack.json')
  const hostTools = useMemo(graphHostTools, [])
  const run = useAssistantRun({ endpoint: slot.endpoint, agent: slot.agent, model: selected?.model ?? '', engine: slot.engine, thinking: slot.thinking, purpose: 'graph', hostTools })
  const [open, setOpen] = useState(false)
  const [chosen, setChosen] = useState<string[]>([])
  const [relationship, setRelationship] = useState('')
  const [id, setId] = useState(graphId ?? '')
  const [path, setPath] = useState('')
  const [description, setDescription] = useState('')
  const [content, setContent] = useState('')
  const [base, setBase] = useState('')
  const [original, setOriginal] = useState('')
  const [promptText, setPromptText] = useState('')
  const [offer, setOffer] = useState<GraphOffer | null>(null)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState<boolean | null>(null)
  const [busy, setBusy] = useState(false)
  const lifetime = useRef(new AbortController())
  useEffect(() => { const controller = new AbortController(); lifetime.current = controller; return () => controller.abort() }, [])
  const declared = useMemo(() => {
    try { return JSON.parse(config.data?.content ?? '{}') as { packs?: Record<string, { path: string }>; graphs?: Record<string, { path: string }> } }
    catch { return {} }
  }, [config.data?.content])
  const proposal = run.events.find(event => event.type === 'proposal')
  useEffect(() => {
    if (proposal?.type !== 'proposal') return
    const document = proposal.document as { graph?: unknown; id?: string; description?: string }
    if (!document.graph || typeof document.graph !== 'object') { setError(msg('The assistant returned no graph document.')); return }
    setContent(JSON.stringify(document.graph, null, 2) + '\n')
    if (!graphId && typeof document.id === 'string') setId(document.id)
    if (typeof document.description === 'string') setDescription(document.description)
    setOffer(null)
  }, [proposal, graphId])
  const working = busy || run.status === 'running'
  async function act(action: () => Promise<void>) {
    setBusy(true); setError(''); setSaved(null)
    try { await action() } catch (cause) { if (!lifetime.current.signal.aborted) setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy(false) }
  }
  async function start() {
    setOffer(null); setContent(''); setBase(''); setOriginal('')
    const signal = lifetime.current.signal
    const packs = await Promise.all(chosen.map(name => readFile(declared.packs![name]!.path, signal)))
    let draft = ''
    if (graphId) {
      const entry = declared.graphs?.[graphId]
      if (!entry) throw new Error(msg('The configured graph could not be read.'))
      const file = await readFile(entry.path, signal)
      setPath(entry.path); setBase(file.sha256); setOriginal(file.content)
      draft = '\n\nCurrent graph document to revise:\n```json\n' + file.content + '\n```'
    }
    const answer = await client!.getPrompt({ name: AUTHOR_GRAPH_PROMPT, arguments: { relationship, packs: '[' + packs.map(pack => pack.content).join(',') + ']' } })
    signal.throwIfAborted()
    const text = answer.messages.map(message => message.content.type === 'text' ? message.content.text : '').join('\n\n')
    setPromptText(text)
    run.start(text + draft)
  }
  return <section aria-label={msg('Author graph')}>
    <Button onClick={() => setOpen(!open)}>{msg('Author graph')}</Button>
    {open && <>
      <p>{graphId ? msg('The configured model provider receives your relationship, chosen packs, current graph document, prompt and tool answers. The proposal remains for you to review; no rows file is written.') : msg('The configured model provider receives your relationship, chosen packs, prompt and tool answers. The proposal remains for you to review; no rows file is written.')}</p>
      <fieldset disabled={working}>
        <legend>{msg('Choose packs')}</legend>
        {Object.keys(declared.packs ?? {}).map(name => <label key={name}><input type="checkbox" checked={chosen.includes(name)} onChange={event => setChosen(event.target.checked ? [...chosen, name] : chosen.filter(value => value !== name))} />{name}</label>)}
        <label>{msg('Relationship')}<textarea rows={4} value={relationship} onChange={event => setRelationship(event.target.value)} /></label>
        <Button disabled={!chosen.length || !relationship.trim() || !client} onClick={() => void act(start)}>{msg('Propose graph')}</Button>
      </fieldset>
      {run.status === 'running' && <Button onClick={run.stop}>{msg('Stop')}</Button>}
      {promptText && <details><summary>{msg('Runtime author_graph prompt and rows guidance')}</summary><pre lang="en">{shownMessage(promptText)}</pre></details>}
      {run.events.filter(event => event.type === 'message').map((event, index) => event.type === 'message' && <p key={index}>{shownMessage(event.text)}</p>)}
      {proposal?.type === 'proposal' && proposal.unknowns.map((unknown, index) => <p key={index}>{shownMessage(unknown)}</p>)}
      {content && <fieldset disabled={working} onChange={() => setOffer(null)}>
        <legend>{msg('Graph proposal')}</legend>
        <p>{msg('A model proposal with runtime findings and a plan does not establish that the composition is faithful to your policy.')}</p>
        <label>{msg('Configured graph id')}<input value={id} disabled={!!graphId} onChange={event => setId(event.target.value)} /></label>
        <label>{msg('Graph path')}<input value={path} disabled={!!graphId} onChange={event => setPath(event.target.value)} /></label>
        {!graphId && <label>{msg('Description')}<input value={description} onChange={event => setDescription(event.target.value)} /></label>}
        {original && <details><summary>{msg('Graph before')}</summary><pre>{original}</pre></details>}
        <label>{msg('Graph bytes to write')}<textarea rows={16} spellCheck={false} value={content} onChange={event => setContent(event.target.value)} /></label>
        <Button disabled={!id || !path || !!run.failure || run.events.some(event => event.type === 'error')} onClick={() => void act(async () => setOffer(await graphPost<GraphOffer>('proposal', { id, path, content, description, baseSha256: base }, lifetime.current.signal)))}>{msg('Review graph write')}</Button>
      </fieldset>}
      {offer && <GraphConfirmation offer={offer} busy={working} onConfirm={() => void act(async () => {
        const accepted = offer; setOffer(null)
        try {
          await graphPost('write', accepted, lifetime.current.signal)
          setSaved(accepted.hasLock)
        } finally {
          await queries.invalidateQueries()
        }
      })} />}
      {(error || run.failure) && <p role="alert">{shownMessage(error || run.failure || '')}</p>}
      {run.events.filter(event => event.type === 'error').map((event, index) => event.type === 'error' && <p role="alert" key={index}>{shownMessage(event.message)}</p>)}
      {saved !== null && <p>{saved ? msg('The graph was written. Review and lock updates the reviewed set.') : msg('The graph was written.')}</p>}
    </>}
  </section>
}

export function GraphConfirmation({ offer, busy, onConfirm }: { offer: GraphOffer; busy: boolean; onConfirm: () => void }) {
  return <section aria-label={msg('Confirm graph write')}>
    <h3>{msg('Graph bytes to write')}</h3><code>{offer.path}</code><pre>{offer.content}</pre>
    <h3>{msg('Findings')}</h3><GraphLabels labels={JSON.parse(offer.findings)} /><pre lang="en">{offer.findings}</pre>
    <h3>{msg('Plan')}</h3><GraphLabels labels={JSON.parse(offer.plan)} /><pre lang="en">{offer.plan}</pre>
    {!offer.baseSha256 && <><h3>{msg('jpack.json before')}</h3><pre>{offer.before}</pre><h3>{msg('Whole jpack.json to write')}</h3><pre>{offer.configContent}</pre><ProposalDiffView diff={diffProposal(offer.before, JSON.parse(offer.configContent))} /></>}
    {offer.hasLock && <><p>{msg('The project keeps a reviewed set. Review and lock updates it.')}</p><p>{offer.baseSha256
      ? msg('Deciding runs of this graph are refused as document-drift until the next Review and lock.')
      : msg('A change to jpack.json holds every pack: until the next lock, the runtime refuses every deciding run by decision id. Rehearsals and tests are not affected.')}</p></>}
    <Button disabled={busy} onClick={onConfirm}>{msg('Confirm graph write')}</Button>
  </section>
}
