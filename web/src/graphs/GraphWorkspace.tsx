import {LoadingState} from '../ui/LoadingState'
import {PageLoading} from '../ui/PageLoading'
import { DefinitionBrief } from '../briefs/DefinitionBrief'
import { DefinitionHistory } from '../definitions/DefinitionHistory'
import { GraphSources } from './GraphSources'
import { DefinitionUsage } from '../usage/DefinitionUsage'
import { DefinitionJsonDialog } from '../definitions/DefinitionJsonDialog'
import { GraphReview } from '../briefs/GraphReview'
import { lazy, Suspense, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { msg } from '../i18n'
import { useChats } from '../chat/ChatProvider'
import { useFileContent } from '../files/queries'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { MoveToFolderItem, ShowFolders } from '../packs/folders/FolderBrowser'
import { Menu, OptionsMenu } from '../ui/Menu'
import { useDraftActions } from '../packs/drafts/DraftActions'
import { IconDetails, IconTrash } from '../shell/icons'
import { GraphOverview } from './GraphOverview'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Dialog, DialogActions } from '../ui/Dialog'
import { PageHeader } from '../ui/PageLayout'
import { Tabs } from '../ui/Tabs'
import { useInspectorPortal } from '../shell/InspectorSlot'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { useShellState } from '../shell/paneState'
import { useBuilderPreference } from '../builders/BuilderSplit'
import { GraphAssistant } from './GraphAssistant'
import { readGraphDocument } from '../mcp/graphDocument'
import { CodeBlock } from '../ui/CodeBlock'
import { ErrorBox, Pill } from '../components/primitives'
import { GraphComposition } from './GraphComposition'
import { GraphConfirmation } from './GraphAuthor'
import { graphPost, type GraphOffer, type GraphProposal } from './author'
import { graphDraftHref, validGraphDraft, type GraphDraft } from './drafts'
import styles from './GraphWorkspace.module.css'
import shared from '../definitions/DefinitionWorkspace.module.css'

const CodeEditor = lazy(() => import('../files/CodeEditor'))
type Config = {packs?: Record<string, {path: string}>; graphs?: Record<string, {path: string; description?: string}>}
export function GraphWorkspace({graphId, tests}: {graphId?: string; tests?: ReactNode}) {
  const [search] = useSearchParams()
  const {chats, drafts, ready} = useChats()
  const config = useFileContent('jpack.json')
  let declared: Config = {}, malformed = false
  try {declared = JSON.parse(config.data?.content ?? '{}'); if (!declared || typeof declared !== 'object' || Array.isArray(declared)) {declared = {}; malformed = true}} catch {malformed = true}
  const path = graphId && Object.hasOwn(declared.graphs ?? {}, graphId) ? declared.graphs![graphId]?.path : undefined
  const file = useFileContent(path)
  const chatId = search.get('chat'), draftId = search.get('draft')
  const draftOwner = search.get('draftChat') ?? chatId
  const draft = [...chats, ...drafts].find(chat => chat.id === draftOwner)?.graphDrafts?.find(draft => draft.draftId === draftId)
  if (config.error) return <ErrorBox title={msg('Could not read project configuration')} error={config.error}/>
  if (malformed) return <p role="alert">{msg('Repair jpack.json before composing a graph.')}</p>
  if (!config.data || draftId && !ready || graphId && path && !file.data && !file.error) return <PageLoading>{msg('Loading graph…')}</PageLoading>
  if (draftId && !draft) return <p role="alert">{msg('This graph draft is no longer available in the conversation.')}</p>
  if (graphId && !path) return <p role="alert">{msg('This graph is not declared in the project.')}</p>
  if (file.error) return <ErrorBox title={msg('Could not read graph')} error={file.error}/>
  const packs = Object.keys(declared.packs ?? {})
  const chosen = search.getAll('pack').filter(id => packs.includes(id))
  const initial: GraphProposal = draft ?? (file.data && graphId ? {id: graphId, path: file.data.path, content: file.data.content, description: declared.graphs?.[graphId]?.description, baseSha256: file.data.sha256} : {id: '', path: '', content: JSON.stringify({formatVersion: '1', id: 'new-graph', version: '0.1.0', nodes: Object.fromEntries(chosen.map(id => [id, {pack: id}])), edges: [], result: chosen[chosen.length - 1] ?? ''}, null, 2) + '\n'})
  return <GraphEditor key={draftId ?? graphId ?? 'new'} initial={initial} tests={tests} packs={packs} chatId={chatId ?? undefined} draftChatId={draftOwner ?? undefined} draft={draft}/>
}
export function GraphEditor({initial, packs, chatId, draftChatId = chatId, draft, tests}: {initial: GraphProposal; packs: string[]; chatId?: string; draftChatId?: string; draft?: GraphDraft; tests?: ReactNode}) {
  const {store} = useChats(), navigate = useNavigate(), queries = useQueryClient()
  const request=useDraftActions(), menuTrigger=useRef<HTMLButtonElement>(null), menuHandoff=useRef(false)
  // Private draft metadata is not part of the server's closed proposal wire.
  const initialProposal: GraphProposal = {id: initial.id, path: initial.path, content: initial.content, description: initial.description, baseSha256: initial.baseSha256}
  const [proposal, setProposal] = useState(initialProposal)
  const [view, setView] = useState('build')
  const [showJson, setShowJson] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false), [sourceOpen, setSourceOpen] = useState(false)
  const [briefVisited, setBriefVisited] = useState(false)
  useEffect(() => { if (view === 'brief') setBriefVisited(true) }, [view])
  const [selection, setSelection] = useState('')
  const [assistantChat, setAssistantChat] = useState(chatId)
  const [paneOpen, setPaneOpen] = useBuilderPreference<boolean>('graph:assistant-open', true)
  const shell = useShellState()
  const paneWidth = shell.inspectorWidth ?? 400, setPaneWidth = shell.resizeInspector, resetPane = shell.resetInspectorWidth
  useInspectorPresentation(useMemo(() => ({workspaceTools: true, title: msg('Assistant'), contextTitle: proposal.id || msg('New graph'), open: paneOpen,
    onOpenChange: setPaneOpen, width: Math.min(640, Math.max(320, paneWidth)), onResize: setPaneWidth, onReset: resetPane, minimumMainWidth: 560, maximumWidth: 640
  }), [proposal.id, paneOpen, paneWidth, setPaneOpen, setPaneWidth, resetPane]))
  const [baseline, setBaseline] = useState(() => JSON.stringify(initialProposal))
  const [past, setPast] = useState<string[]>([]), [future, setFuture] = useState<string[]>([])
  const [offer, setOffer] = useState<GraphOffer | null>(null), [error, setError] = useState(''), [busy, setBusy] = useState(false), [saved, setSaved] = useState(false)
  const [findings, setFindings] = useState<string>()
  const active = useRef<AbortController | null>(null)
  const retainedIdentity = useRef({chatId, draftId: draft?.draftId ?? crypto.randomUUID(), createdAt: draft?.createdAt ?? new Date().toISOString()})
  useEffect(() => () => active.current?.abort(), [])
  const dirty = !saved && JSON.stringify(proposal) !== baseline
  const clearGuard = useDirtyGuard(dirty, msg('Leave this graph and discard unsaved changes?'), {busy, shouldBlock: ({currentLocation, nextLocation}) => currentLocation.pathname !== nextLocation.pathname || ['draft', 'view'].some(key => new URLSearchParams(currentLocation.search).get(key) !== new URLSearchParams(nextLocation.search).get(key))})
  const change = (patch: Partial<GraphProposal>) => {
    if (Object.entries(patch).some(([key, value]) => proposal[key as keyof GraphProposal] !== value)) {setPast(before => [...before.slice(-49), JSON.stringify(proposal)]); setFuture([])}
    setProposal({...proposal, ...patch}); setOffer(null); setFindings(undefined); setSaved(false)
  }
  const undo = (redo: boolean) => {
    const source = redo ? future : past, value = source.at(-1)
    if (value === undefined) return
    if (redo) {setFuture(source.slice(0, -1)); setPast([...past, JSON.stringify(proposal)])} else {setPast(source.slice(0, -1)); setFuture([...future, JSON.stringify(proposal)])}
    setProposal(JSON.parse(value) as GraphProposal); setOffer(null); setFindings(undefined); setSaved(false)
  }
  async function act(task: (signal: AbortSignal) => Promise<void>) {
    if (busy) return
    const controller = new AbortController(); active.current = controller; setBusy(true); setError('')
    try {await task(controller.signal)} catch (cause) {if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : String(cause))}
    finally {if (!controller.signal.aborted) setBusy(false)}
  }
  const retain = () => {
    if (!store || !store.canCreate && !retainedIdentity.current.chatId) throw new Error(msg('Chat history is not ready to retain this draft.'))
    const retained: GraphDraft = {...proposal, id: proposal.id || 'new-graph', path: proposal.path || `${proposal.id || 'new-graph'}.graph.json`, draftId: retainedIdentity.current.draftId, createdAt: retainedIdentity.current.createdAt, saved: false}
    if (!validGraphDraft(retained)) throw new Error(msg('Check the graph id, path and draft size before keeping this draft.'))
    const targetId = assistantChat ?? retainedIdentity.current.chatId
    const target = targetId ? [...store.getSnapshot().chats, ...store.getSnapshot().drafts].find(chat => chat.id === targetId) : store.create()
    if (!target) throw new Error(msg('The conversation is no longer available.'))
    store.retainChat(target.id)
    retainedIdentity.current.chatId = target.id
    store.update(target.id, {graph: {id: retained.id, path: retained.path, workspace: proposal.baseSha256 ? proposal.id : retainedIdentity.current.draftId}})
    const previous = target.graphDrafts ?? []
    if (!previous.some(item => item.draftId === retained.draftId) && previous.length >= 16) throw new Error(msg('This chat has 16 graph drafts. Start a new chat to create another.'))
    store.update(target.id, {graphDrafts: [...previous.filter(item => item.draftId !== retained.draftId), retained]})
    return {target, retained}
  }
  const retainAndOpen = async (signal: AbortSignal) => {
    const {target, retained} = retain()
    if (store!.getSnapshot().error) store!.retrySave()
    // An earlier autosave may still be in flight. Wait before flushing the
    // revision containing this draft; do not navigate on a skipped flush.
    if (store!.getSnapshot().saving) await new Promise<void>((resolve, reject) => {
      const abort = () => {unsubscribe(); reject(signal.reason)}
      const unsubscribe = store!.subscribe(() => {if (!store!.getSnapshot().saving) {unsubscribe(); signal.removeEventListener('abort', abort); resolve()}})
      signal.addEventListener('abort', abort, {once: true})
      if (signal.aborted) abort()
    })
    signal.throwIfAborted()
    if (!await store!.flush()) throw new Error(store!.getSnapshot().error || msg('The draft could not be saved. Try again.'))
    signal.throwIfAborted()
    setBaseline(JSON.stringify(proposal))
    clearGuard()
    navigate(graphDraftHref(target.id, retained.draftId))
  }
  const changeId = (id: string) => {
    let content = proposal.content
    try {const document = JSON.parse(content); if (document.id === (proposal.id || 'new-graph')) content = JSON.stringify({...document, id}, null, 2) + '\n'} catch { /* Source repair remains explicit. */ }
    change({id, content, ...(!proposal.path || proposal.path === `${proposal.id}.graph.json` ? {path: `${id}.graph.json`} : {})})
  }
  const assistant = useInspectorPortal(<GraphAssistant proposal={proposal} workspace={initial.baseSha256 ? initial.id : retainedIdentity.current.draftId}
    openDraftId={retainedIdentity.current.draftId} chatId={chatId ?? assistantChat} onChat={setAssistantChat} onApply={change} busy={busy} selection={selection}/>)
  const parsed = readGraphDocument(proposal.content)
  const check = () => void act(async signal => {const result = await graphPost<{answer: unknown}>('validate', {content: proposal.content}, signal); if (!signal.aborted) setFindings(JSON.stringify(result.answer, null, 2))})
  const review = () => {
    if (!proposal.id || !proposal.path) {setSettingsOpen(true); setError(msg('Choose a graph id and path before reviewing the write.')); return}
    void act(async signal => {const result = await graphPost<GraphOffer>('proposal', proposal, signal); if (!signal.aborted) setOffer(result)})
  }
  return <article className={styles.builder} data-layout="page" aria-label={msg('Compose graph')}>
    {assistant}
    <PageHeader variant="title" leading={<ShowFolders/>} title={proposal.id || msg('New graph')} meta={<><Pill tone="quiet">{dirty ? msg('Unsaved changes') : proposal.baseSha256 ? msg('Saved') : msg('Draft')}</Pill> <Pill tone="quiet">{msg('Experimental')}</Pill></>}
      actions={<><OptionsMenu label={msg('Graph options')} triggerRef={menuTrigger} onOpenChange={open=>{if(open)menuHandoff.current=false}} onCloseAutoFocus={event=>{if(menuHandoff.current)event.preventDefault()}}>
        {(draft || proposal.baseSha256) && <MoveToFolderItem id={draft && !draft.saved ? 'graph-draft:'+draft.draftId : 'graph:'+proposal.id} opener={menuTrigger} onSelect={()=>{menuHandoff.current=true}}/>}
        <Menu.Item icon={<IconDetails/>} onSelect={()=>{menuHandoff.current=true;setShowJson(true)}}>{msg('View JSON')}</Menu.Item>
        <Menu.Item icon={<IconDetails/>} disabled={busy} onSelect={()=>{menuHandoff.current=true;setSettingsOpen(true)}}>{msg('Graph settings…')}</Menu.Item>
        <Menu.Item icon={<IconDetails/>} disabled={busy} onSelect={()=>{menuHandoff.current=true;setSourceOpen(true)}}>{msg('Edit JSON…')}</Menu.Item>
        <Menu.Item icon={<IconDetails/>} disabled={busy || !store} onSelect={() => void act(retainAndOpen)}>{msg('Keep draft')}</Menu.Item>
        {draft && !draft.saved && draftChatId && <><Menu.Separator/><Menu.Item destructive icon={<IconTrash/>} disabled={busy || !request || !store} onSelect={()=>{
          menuHandoff.current=true
          request?.({kind:'delete',opener:menuTrigger.current??undefined,onDeleted:clearGuard,item:{key:'graph-draft:'+draftChatId+':'+draft.draftId,id:'graph-draft:'+draft.draftId,title:proposal.id,kind:'graph',status:'draft',href:'',chatId:draftChatId,draftId:draft.draftId}})
        }}>{msg('Delete draft…')}</Menu.Item></>}
      </OptionsMenu>
        <Button variant="primary" disabled={busy || saved} onClick={review}>{busy ? msg('Working…') : msg('Review & save')}</Button></>}/>
    <DefinitionJsonDialog open={showJson} onOpenChange={setShowJson} text={proposal.content} title={msg('Graph JSON')} openerRef={menuTrigger}/>
    {error && <p role="alert" className={styles.feedback}>{error}</p>}
    <Tabs overflow label={msg('Graph builder')} variant="page" value={view} onValueChange={setView} scrollable keepMounted fillPanel={view} tabs={[
      {value: 'overview', label: msg('Overview'), panel: <div className={styles.viewBody}><GraphOverview document={parsed.ok ? parsed.document : undefined} description={proposal.description} onViewLogic={() => setView('build')}/></div>},
      {value: 'build', label: msg('Logic'), panel: <GraphComposition content={proposal.content} onChange={content => change({content})} packs={packs} disabled={busy} onSelection={setSelection}
        tools={<><Button disabled={busy || !past.length} variant="quiet" onClick={() => undo(false)}>{msg('Undo')}</Button><Button disabled={busy || !future.length} variant="quiet" onClick={() => undo(true)}>{msg('Redo')}</Button><Button disabled={busy} variant="quiet" onClick={check}>{msg('Check')}</Button></>}/>},
      {value: 'sources', label: msg('Sources'), panel: view === 'sources' ? <div className={styles.viewBody}><GraphSources document={parsed.ok ? parsed.document : undefined}/></div> : null},
      {value: 'tests', label: msg('Tests'), panel: <div className={styles.viewBody}>
        <p className={styles.hint}>{msg('Tests run against saved graph and pack files. Unsaved changes in this builder are not included.')}</p>
        {proposal.baseSha256 && tests ? tests : <p>{msg('Save this graph before running its tests.')}</p>}
      </div>},
      {value: 'review', label: msg('Review'), panel: <div className={styles.viewBody}>{view === 'review' && <GraphReview
        owner={draft && !draft.saved ? draft.draftId : initial.id} title={proposal.id || msg('New graph')} content={proposal.content} draft={!proposal.baseSha256}
        blocked={dirty ? msg('Unsaved changes') : !draft && !proposal.baseSha256 ? msg('Draft not retained') : undefined}
        actions={<><Button disabled={busy} onClick={check}>{msg('Check')}</Button></>}/>}</div>},
      {value: 'brief', label: msg('Brief'), panel: <div className={styles.viewBody}>{(view === 'brief' || briefVisited) && (!draft && !proposal.baseSha256 ? <p>{msg('Keep this draft before generating a brief.')}</p> : <DefinitionBrief kind={draft && !draft.saved ? 'graph-draft' : 'graph'} owner={draft && !draft.saved ? draft.draftId : initial.id} title={proposal.id || msg('New graph')} content={proposal.content} blocked={dirty ? msg('Keep or save these changes before generating a brief.') : !draft && !proposal.baseSha256 ? msg('Keep this draft before generating a brief.') : undefined}/>)}</div>},
      {value: 'history', label: msg('History'), panel: view === 'history' ? <DefinitionHistory kind="graph" owner={draft && !draft.saved ? draft.draftId : initial.id} documentId={parsed.ok ? parsed.document.id : undefined} draft={!proposal.baseSha256} graphDraft={draft}/> : null},
      {value: 'usage', label: msg('Usage'), panel: view === 'usage' ? <DefinitionUsage kind="graph"/> : null},
    ]}/>
    <Dialog open={settingsOpen} onOpenChange={setSettingsOpen} title={msg('Graph settings')} openerRef={menuTrigger}
      footer={<DialogActions><Button onClick={()=>setSettingsOpen(false)}>{msg('Done')}</Button></DialogActions>}>
      <div className={styles.settings}><Field label={msg('Configured graph id')}>{w => <Input {...w} value={proposal.id} disabled={busy || Boolean(proposal.baseSha256)} onChange={e => changeId(e.target.value)}/>}</Field>
        <Field label={msg('Graph path')}>{w => <Input {...w} value={proposal.path} disabled={busy || Boolean(proposal.baseSha256)} onChange={e => change({path: e.target.value})}/>}</Field>
        <Field label={msg('Description')}>{w => <Input {...w} value={proposal.description ?? ''} disabled={busy || Boolean(proposal.baseSha256)} onChange={e => change({description: e.target.value})}/>}</Field>
<p className={styles.hint}>{msg('Changes stay in the draft until you review and save.')}</p></div>
    </Dialog>
    <Dialog open={sourceOpen} onOpenChange={setSourceOpen} title={msg('Edit graph JSON')} size="wide" openerRef={menuTrigger}
      description={msg('Changes stay in the draft until you review and save.')}
      footer={<DialogActions><Button onClick={()=>setSourceOpen(false)}>{msg('Done')}</Button></DialogActions>}>
      <div className={shared.editor}><Suspense fallback={<LoadingState inset>{msg('Loading editor…')}</LoadingState>}><CodeEditor id="graph-source" path="graph.json" value={proposal.content} wrap readOnly={busy} onChange={content => change({content})} onSave={()=>{setSourceOpen(false);review()}} onFormat={() => {try {change({content: JSON.stringify(JSON.parse(proposal.content), null, 2) + '\n'})} catch {setError(msg('Fix the JSON before formatting.'))}}}/></Suspense></div>
    </Dialog>
    <Dialog open={findings !== undefined} onOpenChange={open => {if (!open) setFindings(undefined)}} title={msg('Runtime findings')} description={msg('Validation checks the draft structure, not whether the policy is correct.')}>
      {findings && <CodeBlock text={findings} label={msg('Runtime findings')}/>}<DialogActions><Button onClick={() => setFindings(undefined)}>{msg('Close')}</Button></DialogActions>
    </Dialog>
    <Dialog open={offer !== null} onOpenChange={open => {if (!open && !busy) setOffer(null)}} title={msg('Review & save')} description={msg('Review the exact graph and configuration changes before writing project files.')}>
    {offer && <GraphConfirmation offer={offer} busy={busy} onConfirm={() => void act(async signal => {const accepted = offer; setOffer(null); try {await graphPost('write', accepted, signal)} finally {await queries.invalidateQueries()}  if (signal.aborted) return; setSaved(true); if (draftChatId && draft && store) {const chat = [...store.getSnapshot().chats, ...store.getSnapshot().drafts].find(c => c.id === draftChatId); if (chat) store.update(draftChatId, {graphDrafts: chat.graphDrafts?.map(d => d.draftId === draft.draftId ? {...d, ...proposal, saved: true} : d)})} for (const chat of [...(store?.getSnapshot().chats ?? []), ...(store?.getSnapshot().drafts ?? [])]) {
      if (chat.graph?.workspace === (initial.baseSha256 ? initial.id : retainedIdentity.current.draftId)) store?.update(chat.id, {graph: {id: proposal.id, path: proposal.path, workspace: proposal.id}})
    } clearGuard(); navigate(`/graphs/${encodeURIComponent(proposal.id)}${assistantChat ? '?chat=' + encodeURIComponent(assistantChat) : ''}`)})}/>}
      <DialogActions><Button disabled={busy} onClick={() => setOffer(null)}>{msg('Cancel')}</Button></DialogActions>
    </Dialog>
  </article>
}
