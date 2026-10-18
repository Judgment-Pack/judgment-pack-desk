import {LoadingState} from '../ui/LoadingState'
import {PageLoading} from '../ui/PageLoading'
import { DefinitionBrief } from '../briefs/DefinitionBrief'
import { DefinitionHistory } from '../definitions/DefinitionHistory'
import { GraphSources } from './GraphSources'
import { DefinitionUsage } from '../usage/DefinitionUsage'
import { DefinitionJsonDialog } from '../definitions/DefinitionJsonDialog'
import { GraphReview } from '../briefs/GraphReview'
import { lazy, Suspense, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useQuery,useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { msg } from '../i18n'
import { useChats } from '../chat/ChatProvider'
import {readFile} from '../files/client'
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
import { GraphRequestError,graphPost, type GraphOffer, type GraphProposal } from './author'
import { graphDraftHref, validGraphDraft,retainGraphDraft,readGraphDrafts,sameGraphDraft,graphDraftsQuery,GRAPH_DRAFTS_KEY,retainedGraphDrafts,graphDraftKey,type GraphDraft } from './drafts'
import styles from './GraphWorkspace.module.css'
import {uniquePolicySources,validPolicySources,type PolicyAuthoringSource} from '../policies/authoringSource'
import shared from '../definitions/DefinitionWorkspace.module.css'

const CodeEditor = lazy(() => import('../files/CodeEditor'))
type Config = {packs?: Record<string, {path: string}>; graphs?: Record<string, {path: string; description?: string}>}
export function GraphWorkspace({graphId, tests}: {graphId?: string; tests?: ReactNode}) {
  const [search] = useSearchParams()
  const {chats, drafts, ready} = useChats()
  const retained=useQuery(graphDraftsQuery())
  const config = useFileContent('jpack.json')
  let declared: Config = {}, malformed = false
  try {declared = JSON.parse(config.data?.content ?? '{}'); if (!declared || typeof declared !== 'object' || Array.isArray(declared)) {declared = {}; malformed = true}} catch {malformed = true}
  const path = graphId && Object.hasOwn(declared.graphs ?? {}, graphId) ? declared.graphs![graphId]?.path : undefined
  const file = useFileContent(path)
  const chatId = search.get('chat'), draftId = search.get('draft')
  const draftOwner = search.get('draftChat') ?? chatId
  const entries=retainedGraphDrafts(retained.data?.content,[...chats,...drafts])
  const draft=entries.find(draft=>draft.originChatId===draftOwner&&draft.draftId===draftId)
  const persisted=retained.data?.content.drafts.find(d=>d.originChatId===draftOwner&&d.draftId===draftId)
  const policySources=uniquePolicySources((draft?[draft]:entries.filter(d=>(d.saved||d.writePending)&&d.id===graphId&&d.path===path&&d.content===file.data?.content)).flatMap(d=>d.policySources??[]))
  if(retained.error&&!retained.data)return <ErrorBox title={msg('Could not read graph drafts')} error={retained.error}/>
  if (config.error) return <ErrorBox title={msg('Could not read project configuration')} error={config.error}/>
  if (malformed) return <p role="alert">{msg('Repair jpack.json before composing a graph.')}</p>
  if (!config.data || retained.isPending || draftId && !ready || graphId && path && !file.data && !file.error) return <PageLoading>{msg('Loading graph…')}</PageLoading>
  if (draftId && !draft) return <p role="alert">{msg('This graph draft is no longer available.')}</p>
  if (graphId && !path) return <p role="alert">{msg('This graph is not declared in the project.')}</p>
  if (file.error) return <ErrorBox title={msg('Could not read graph')} error={file.error}/>
  const packs = Object.keys(declared.packs ?? {})
  const chosen = search.getAll('pack').filter(id => packs.includes(id))
  const initial: GraphProposal = draft ?? (file.data && graphId ? {id: graphId, path: file.data.path, content: file.data.content, description: declared.graphs?.[graphId]?.description, baseSha256: file.data.sha256} : {id: '', path: '', content: JSON.stringify({formatVersion: '1', id: 'new-graph', version: '0.1.0', nodes: Object.fromEntries(chosen.map(id => [id, {pack: id}])), edges: [], result: chosen[chosen.length - 1] ?? ''}, null, 2) + '\n'})
  return <GraphEditor key={draft?graphDraftKey(draft):graphId??'new'} initial={initial} tests={tests} packs={packs} chatId={chatId ?? undefined} draftChatId={draftOwner ?? undefined} draft={draft} persistedDraft={persisted} policySources={policySources}/>
}
export function GraphEditor({initial, packs, chatId, draftChatId = chatId, draft, tests,persistedDraft,policySources:incomingPolicySources=[]}: {initial: GraphProposal; packs: string[]; chatId?: string; draftChatId?: string; draft?: GraphDraft; tests?: ReactNode;persistedDraft?:GraphDraft;policySources?:PolicyAuthoringSource[]}) {
  const {store} = useChats(), navigate = useNavigate(), queries = useQueryClient()
  const retainedRevision=useRef(persistedDraft)
  const [policySources]=useState(incomingPolicySources)
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
  const [pending,setPending]=useState(!!draft?.writePending),[publicWritten,setPublicWritten]=useState(false),[intentUnconfirmed,setIntentUnconfirmed]=useState(false),[notCommitted,setNotCommitted]=useState(false)
  const pendingCandidate=useRef<GraphDraft|undefined>(draft?.writePending?draft:undefined)
  const active = useRef<AbortController | null>(null)
  const retainedIdentity = useRef({chatId:draftChatId??chatId, draftId: draft?.draftId ?? crypto.randomUUID(), createdAt: draft?.createdAt ?? new Date().toISOString()})
  useEffect(() => () => active.current?.abort(), [])
  const dirty = !saved && JSON.stringify(proposal) !== baseline
  const clearGuard = useDirtyGuard(dirty, msg('Leave this graph and discard unsaved changes?'), {busy, shouldBlock: ({currentLocation, nextLocation}) => {const current=new URLSearchParams(currentLocation.search),next=new URLSearchParams(nextLocation.search),owner=(params:URLSearchParams)=>params.has('draft')?(params.get('draftChat')??params.get('chat')):undefined;return currentLocation.pathname!==nextLocation.pathname||['draft','view'].some(key=>current.get(key)!==next.get(key))||owner(current)!==owner(next)}})
  const change = (patch: Partial<GraphProposal>) => {
    if(pending||intentUnconfirmed)return
    if (Object.entries(patch).some(([key, value]) => proposal[key as keyof GraphProposal] !== value)) {setPast(before => [...before.slice(-49), JSON.stringify(proposal)]); setFuture([])}
    setProposal({...proposal, ...patch}); setOffer(null); setFindings(undefined); setSaved(false)
  }
  const undo = (redo: boolean) => {
    if(pending||intentUnconfirmed)return
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
  const retain = async (signal:AbortSignal) => {
    if(!validPolicySources(policySources))throw Error(msg('Review the graph Policy sources before opening an assistant. The exact basis must contain at most eight distinct, nonconflicting sources.'))
    if (!store || !store.canCreate && !retainedIdentity.current.chatId) throw new Error(msg('Chat history is not ready to retain this draft.'))
    const retained: GraphDraft = {...proposal, id: proposal.id || 'new-graph', path: proposal.path || `${proposal.id || 'new-graph'}.graph.json`, draftId: retainedIdentity.current.draftId, createdAt: retainedIdentity.current.createdAt, saved: false}
    if (!validGraphDraft(retained)) throw new Error(msg('Check the graph id, path and draft size before keeping this draft.'))
    const targetId = assistantChat ?? retainedIdentity.current.chatId
    const target = (targetId ? [...store.getSnapshot().chats, ...store.getSnapshot().drafts].find(chat => chat.id === targetId) : undefined) ?? store.create()
    if (!target) throw new Error(msg('The conversation is no longer available.'))
    store.retainChat(target.id)
    retainedIdentity.current.chatId ??= target.id
    retained.originChatId=retainedIdentity.current.chatId
    if(policySources.length)retained.policySources=policySources
    const result=await retainGraphDraft(retained,retainedRevision.current,signal)
    retainedRevision.current=result.content.drafts.find(d=>graphDraftKey(d)===graphDraftKey(retained))
    queries.setQueryData(GRAPH_DRAFTS_KEY,result)
    store.update(target.id, {graph: {id: retained.id, path: retained.path, workspace: proposal.baseSha256 ? proposal.id : graphDraftKey(retained)},...(policySources.length?{policySources}:{})})
    const previous = target.graphDrafts ?? []
    if (!previous.some(item => item.draftId === retained.draftId) && previous.length >= 16) throw new Error(msg('This chat has 16 graph drafts. Start a new chat to create another.'))
    if(target.id===retained.originChatId){const historical:GraphDraft={...proposal,id:retained.id,path:retained.path,draftId:retained.draftId,createdAt:retained.createdAt,saved:false};store.update(target.id, {graphDrafts: [...previous.filter(item => item.draftId !== retained.draftId), historical]})}
    return {target, retained}
  }
  const retainAndOpen = async (signal: AbortSignal) => {
    const {target, retained} = await retain(signal)
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
    navigate(graphDraftHref(retained.originChatId??target.id, retained.draftId))
  }
  const changeId = (id: string) => {
    let content = proposal.content
    try {const document = JSON.parse(content); if (document.id === (proposal.id || 'new-graph')) content = JSON.stringify({...document, id}, null, 2) + '\n'} catch { /* Source repair remains explicit. */ }
    change({id, content, ...(!proposal.path || proposal.path === `${proposal.id}.graph.json` ? {path: `${id}.graph.json`} : {})})
  }
  const assistant = useInspectorPortal(<GraphAssistant proposal={proposal} workspace={initial.baseSha256 ? initial.id : retainedIdentity.current.chatId?retainedIdentity.current.chatId+':'+retainedIdentity.current.draftId:retainedIdentity.current.draftId}
    openDraftId={retainedIdentity.current.draftId} chatId={chatId ?? assistantChat} onChat={setAssistantChat} onApply={change} busy={busy||pending||intentUnconfirmed} selection={selection} policySources={policySources}/>)
  const parsed = readGraphDocument(proposal.content)
  const check = () => void act(async signal => {const result = await graphPost<{answer: unknown}>('validate', {content: proposal.content}, signal); if (!signal.aborted) setFindings(JSON.stringify(result.answer, null, 2))})
  const review = () => {
    if(pending||intentUnconfirmed)return
    if (!proposal.id || !proposal.path) {setSettingsOpen(true); setError(msg('Choose a graph id and path before reviewing the write.')); return}
    void act(async signal => {const result = await graphPost<GraphOffer>('proposal', proposal, signal); if (!signal.aborted) setOffer(result)})
  }
  const finishHistory=async (candidate:GraphDraft,signal:AbortSignal,reconcile:boolean)=>{
    if(reconcile){
      const file=await readFile(candidate.path,signal),config=JSON.parse((await readFile('jpack.json',signal)).content) as Config
      const declaration=config.graphs&&Object.hasOwn(config.graphs,candidate.id)?config.graphs[candidate.id]:undefined
      if(file.content!==candidate.content||declaration?.path!==candidate.path||(declaration.description??'')!==(candidate.description??''))throw Error(msg('The saved graph or its registration differs from this attempted save. Keep the retained candidate and review the project files before finishing history.'))
    }
    const finalized={...candidate,saved:true};delete finalized.writePending
    const completed=await retainGraphDraft(finalized,retainedRevision.current,signal)
    retainedRevision.current=completed.content.drafts.find(d=>graphDraftKey(d)===graphDraftKey(finalized));queries.setQueryData(GRAPH_DRAFTS_KEY,completed)
    setPending(false);setIntentUnconfirmed(false);setSaved(true)
    if(draftChatId&&draft&&store){const owner=[...store.getSnapshot().chats,...store.getSnapshot().drafts].find(c=>c.id===draftChatId);if(owner)store.update(draftChatId,{graphDrafts:owner.graphDrafts?.map(d=>d.draftId===draft.draftId?{...d,...proposal,saved:true}:d)})}
    for(const chat of [...(store?.getSnapshot().chats??[]),...(store?.getSnapshot().drafts??[])])if(chat.graph?.workspace===(initial.baseSha256?initial.id:graphDraftKey(candidate))||chat.graph?.workspace===retainedIdentity.current.draftId)store?.update(chat.id,{graph:{id:candidate.id,path:candidate.path,workspace:candidate.id}})
    clearGuard();navigate(`/graphs/${encodeURIComponent(candidate.id)}${assistantChat?'?chat='+encodeURIComponent(assistantChat):''}`)
  }
  const confirmWrite=async(signal:AbortSignal)=>{
    if(!offer||pending||intentUnconfirmed)return
    const accepted=offer,kept=await retain(signal),registration=(JSON.parse(accepted.configContent) as Config).graphs?.[accepted.id]
    if(registration?.path!==accepted.path)throw Error(msg('The reviewed graph registration changed. Review the graph again before saving.'))
    const candidate={...kept.retained,description:registration.description,writePending:true}
    if(!validGraphDraft(candidate))throw Error(msg('Check the graph id, path and draft size before keeping this draft.'))
    pendingCandidate.current=candidate;setOffer(null)
    try{
      const intent=await retainGraphDraft(candidate,retainedRevision.current,signal)
      retainedRevision.current=intent.content.drafts.find(d=>graphDraftKey(d)===graphDraftKey(candidate));queries.setQueryData(GRAPH_DRAFTS_KEY,intent)
    }catch{
      setIntentUnconfirmed(true)
      throw Error(msg('The private save intent could not be confirmed. No graph write was sent. Recover the retained draft before continuing.'))
    }
    setPending(true);setBaseline(JSON.stringify(proposal))
    try{
      const result=await graphPost<{graphWritten:boolean;declared:boolean}>('write',accepted,signal)
      if(!result.graphWritten||!result.declared)throw Error('unconfirmed graph write')
      setPublicWritten(true)
    }catch(cause){
      if(cause instanceof GraphRequestError&&cause.notCommitted){setNotCommitted(true);throw Error(msg('The graph write was refused before any project file changed. Create a revised draft to review again.'))}
      throw Error(msg('The graph write outcome is unresolved. Its exact candidate is retained. Check the saved graph and finish private history; the write will not be replayed.'))
    }finally{await queries.invalidateQueries()}
    try{await finishHistory(candidate,signal,false)}catch{throw Error(msg('The graph file was saved. Its private history still needs reconciliation. Finish saving history without writing the graph again.'))}
  }
  const recoverIntent=async(signal:AbortSignal)=>{
    const candidate=pendingCandidate.current;if(!candidate)return
    const reply=await readGraphDrafts(signal),held=reply.content.drafts.find(d=>graphDraftKey(d)===graphDraftKey(candidate))
    if(!held)throw Error(msg('The retained draft changed. Reopen it before continuing.'))
    const plain={...candidate};delete plain.writePending
    if(held.writePending&&sameGraphDraft(held,candidate)){retainedRevision.current=held;setPending(true);setNotCommitted(true)}
    else if(sameGraphDraft(held,plain)){retainedRevision.current=held;setPending(false)}
    else if(held.saved){retainedRevision.current=held;await finishHistory(candidate,signal,true)}
    else throw Error(msg('The retained draft changed. Reopen it before continuing.'))
    queries.setQueryData(GRAPH_DRAFTS_KEY,reply);setIntentUnconfirmed(false)
  }
  const revisedCopy=async(signal:AbortSignal)=>{
    const candidate=pendingCandidate.current;if(!candidate||!notCommitted)return
    const next={...candidate,draftId:crypto.randomUUID(),createdAt:new Date().toISOString(),saved:false};delete next.writePending
    const reply=await retainGraphDraft(next,undefined,signal);queries.setQueryData(GRAPH_DRAFTS_KEY,reply);clearGuard();navigate(graphDraftHref(next.originChatId!,next.draftId))
  }
  return <article className={styles.builder} data-layout="page" aria-label={msg('Compose graph')}>
    {assistant}
    <PageHeader variant="title" leading={<ShowFolders/>} title={proposal.id || msg('New graph')} meta={<><Pill tone="quiet">{pending||intentUnconfirmed?msg('Save needs reconciliation'):dirty ? msg('Unsaved changes') : proposal.baseSha256 ? msg('Saved') : msg('Draft')}</Pill> <Pill tone="quiet">{msg('Experimental')}</Pill></>}
      actions={<><OptionsMenu label={msg('Graph options')} triggerRef={menuTrigger} onOpenChange={open=>{if(open)menuHandoff.current=false}} onCloseAutoFocus={event=>{if(menuHandoff.current)event.preventDefault()}}>
        {(draft || proposal.baseSha256) && <MoveToFolderItem id={draft && !draft.saved ? 'graph-draft:'+draft.draftId : 'graph:'+proposal.id} opener={menuTrigger} onSelect={()=>{menuHandoff.current=true}}/>}
        <Menu.Item icon={<IconDetails/>} onSelect={()=>{menuHandoff.current=true;setShowJson(true)}}>{msg('View JSON')}</Menu.Item>
        <Menu.Item icon={<IconDetails/>} disabled={busy} onSelect={()=>{menuHandoff.current=true;setSettingsOpen(true)}}>{msg('Graph settings…')}</Menu.Item>
        <Menu.Item icon={<IconDetails/>} disabled={busy} onSelect={()=>{menuHandoff.current=true;setSourceOpen(true)}}>{msg('Edit JSON…')}</Menu.Item>
        <Menu.Item icon={<IconDetails/>} disabled={busy || !store || pending || intentUnconfirmed} onSelect={() => void act(retainAndOpen)}>{msg('Keep draft')}</Menu.Item>
        {draft && !draft.saved && !draft.writePending && !pending && draftChatId && <><Menu.Separator/><Menu.Item destructive icon={<IconTrash/>} disabled={busy || !request || !store} onSelect={()=>{
          menuHandoff.current=true
          request?.({kind:'delete',opener:menuTrigger.current??undefined,onDeleted:clearGuard,item:{key:'graph-draft:'+draftChatId+':'+draft.draftId,id:'graph-draft:'+draft.draftId,title:proposal.id,kind:'graph',status:'draft',href:'',chatId:draftChatId,draftId:draft.draftId}})
        }}>{msg('Delete draft…')}</Menu.Item></>}
      </OptionsMenu>
        <Button variant="primary" disabled={busy || saved || pending || intentUnconfirmed || !validPolicySources(policySources)} onClick={review}>{busy ? msg('Working…') : msg('Review & save')}</Button></>}/>
    <DefinitionJsonDialog open={showJson} onOpenChange={setShowJson} text={proposal.content} title={msg('Graph JSON')} openerRef={menuTrigger}/>
    {!validPolicySources(policySources)&&<p role="alert">{msg('Review the graph Policy sources before opening an assistant. The exact basis must contain at most eight distinct, nonconflicting sources.')}</p>}
    {(pending||intentUnconfirmed)&&<div role="status"><p>{intentUnconfirmed?msg('No graph write was sent. Recover the private save intent before continuing.'):publicWritten?msg('The graph file was saved. Finish retaining its private history.'):msg('A reviewed graph save needs reconciliation. Check the exact saved file before finishing private history.')}</p>
      {intentUnconfirmed?<Button disabled={busy} onClick={()=>void act(recoverIntent)}>{msg('Recover save intent')}</Button>:<Button disabled={busy} onClick={()=>void act(signal=>finishHistory(pendingCandidate.current!,signal,true))}>{msg('Finish saving history')}</Button>}
      {notCommitted&&!intentUnconfirmed&&<Button disabled={busy} onClick={()=>void act(revisedCopy)}>{msg('Create revised draft')}</Button>}
    </div>}
    {error && <p role="alert" className={styles.feedback}>{error}</p>}
    <Tabs overflow label={msg('Graph builder')} variant="page" value={view} onValueChange={setView} scrollable keepMounted fillPanel={view} tabs={[
      {value: 'overview', label: msg('Overview'), panel: <div className={styles.viewBody}><GraphOverview document={parsed.ok ? parsed.document : undefined} description={proposal.description} onViewLogic={() => setView('build')}/></div>},
      {value: 'build', label: msg('Logic'), panel: <GraphComposition content={proposal.content} onChange={content => change({content})} packs={packs} disabled={busy||pending||intentUnconfirmed} onSelection={setSelection}
        tools={<><Button disabled={busy || !past.length} variant="quiet" onClick={() => undo(false)}>{msg('Undo')}</Button><Button disabled={busy || !future.length} variant="quiet" onClick={() => undo(true)}>{msg('Redo')}</Button><Button disabled={busy} variant="quiet" onClick={check}>{msg('Check')}</Button></>}/>},
      {value: 'sources', label: msg('Sources'), panel: view === 'sources' ? <div className={styles.viewBody}><GraphSources document={parsed.ok ? parsed.document : undefined} policySources={policySources}/></div> : null},
      {value: 'tests', label: msg('Tests'), panel: <div className={styles.viewBody}>
        <p className={styles.hint}>{msg('Tests run against saved graph and pack files. Unsaved changes in this builder are not included.')}</p>
        {proposal.baseSha256 && tests ? tests : <p>{msg('Save this graph before running its tests.')}</p>}
      </div>},
      {value: 'review', label: msg('Review'), panel: <div className={styles.viewBody}>{view === 'review' && <GraphReview
        owner={draft && !draft.saved ? graphDraftKey(draft) : initial.id} title={proposal.id || msg('New graph')} content={proposal.content} draft={!proposal.baseSha256}
        blocked={pending||intentUnconfirmed?msg('Save needs reconciliation'):dirty ? msg('Unsaved changes') : !retainedRevision.current && !proposal.baseSha256 ? msg('Draft not retained') : undefined}
        actions={<><Button disabled={busy} onClick={check}>{msg('Check')}</Button></>}/>}</div>},
      {value: 'brief', label: msg('Brief'), panel: <div className={styles.viewBody}>{(view === 'brief' || briefVisited) && (!retainedRevision.current && !proposal.baseSha256 ? <p>{msg('Keep this draft before generating a brief.')}</p> : <DefinitionBrief kind={draft && !draft.saved ? 'graph-draft' : 'graph'} owner={draft && !draft.saved ? graphDraftKey(draft) : initial.id} title={proposal.id || msg('New graph')} content={proposal.content} blocked={dirty ? msg('Keep or save these changes before generating a brief.') : !retainedRevision.current && !proposal.baseSha256 ? msg('Keep this draft before generating a brief.') : undefined}/>)}</div>},
      {value: 'history', label: msg('History'), panel: view === 'history' ? <DefinitionHistory kind="graph" owner={draft && !draft.saved ? graphDraftKey(draft) : initial.id} documentId={parsed.ok ? parsed.document.id : undefined} draft={!proposal.baseSha256} graphDraft={draft}/> : null},
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
      <div className={shared.editor}><Suspense fallback={<LoadingState inset>{msg('Loading editor…')}</LoadingState>}><CodeEditor id="graph-source" path="graph.json" value={proposal.content} wrap readOnly={busy||pending||intentUnconfirmed} onChange={content => change({content})} onSave={()=>{setSourceOpen(false);review()}} onFormat={() => {try {change({content: JSON.stringify(JSON.parse(proposal.content), null, 2) + '\n'})} catch {setError(msg('Fix the JSON before formatting.'))}}}/></Suspense></div>
    </Dialog>
    <Dialog open={findings !== undefined} onOpenChange={open => {if (!open) setFindings(undefined)}} title={msg('Runtime findings')} description={msg('Validation checks the draft structure, not whether the policy is correct.')}>
      {findings && <CodeBlock text={findings} label={msg('Runtime findings')}/>}<DialogActions><Button onClick={() => setFindings(undefined)}>{msg('Close')}</Button></DialogActions>
    </Dialog>
    <Dialog open={offer !== null} onOpenChange={open => {if (!open && !busy) setOffer(null)}} title={msg('Review & save')} description={msg('Review the exact graph and configuration changes before writing project files.')}>
    {offer && <GraphConfirmation offer={offer} busy={busy} onConfirm={()=>void act(confirmWrite)}/>}
      <DialogActions><Button disabled={busy} onClick={() => setOffer(null)}>{msg('Cancel')}</Button></DialogActions>
    </Dialog>
  </article>
}
