import {retainGraphDraft,GRAPH_DRAFTS_KEY} from '../graphs/drafts'
import {policySourceDocuments,POLICY_SOURCE_INSTRUCTIONS} from '../policies/authoringSource'
import {authorizePolicyChat} from '../policies/chatAuthorization'
import {policyChatTools} from '../policies/chatTools'
import {POLICY_CHAT_INSTRUCTIONS} from '../policies/chat'
import {useDialectPublicationAutoSave} from './useDialectPublicationAutoSave'
import {retainDialectProposal,DIALECT_DRAFTS_KEY} from '../dialects/drafts'
import {usePackAutoSave} from './usePackAutoSave'
import {privateQuery} from '../data/privateQuery'
import {checkPackProposal,packReviewContext} from './packProposal'
import {planTools} from '@judgment-pack/harness/planning/tools'
import {projectMemoryTools} from '@judgment-pack/harness/memory/tools'
import {MEMORY_KEY} from '@judgment-pack/harness/memory/client'
import {dialectChatTools} from '../dialects/dialectChatTools'
import { testChatTools } from '../packs/test-workspace/chatTools'
import { readTests, updateTests, TESTS_KEY } from '../packs/test-workspace/store'
import { emptySuite } from '../packs/test-workspace/model'
import { jsonIdentity } from '../research/checkCandidate'
import { readFile } from '../files/client'
import type { McpToolResult } from '@judgment-pack/harness/engine'
import {useQueryClient} from '@tanstack/react-query'
import {approvalKey} from '../dialects/researchApproval'
import {dialectRecipeIsEditing} from '../dialects/recipeEditing'
import {DialectRecipeDraftError} from '../dialects/proposalFailure'
import { questionTool, QUESTION_INSTRUCTIONS } from '@judgment-pack/harness/questions'
import {dialectActionChatTools} from '../dialects/chatActionTools'
import {dialectRecipeChatTools} from '../dialects/chatTools'
import { gateResearchTools, researchChoice } from './researchChoice'
import { graphChatTools } from '../graphs/chatTools'
import { useMcp } from '../mcp/McpProvider'
import { useSearchConnections, useSearchPreference } from '../search/connections'
import { sourceMessage } from '../i18n/source'
import { createContext, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useConnections } from '../connections/catalog'
import { useFileListing } from '../files/queries'
import { useResearchRun } from '../research/useResearchRun'
import { recordActivity } from '../shell/consoleLog'
import { restoreLedger } from './checkpoint'
import { linkReadable, websiteReadable, linkReading } from './linkTools'
import { ChatStore, type Chat, type ChatAttachment } from './store'

const Context = createContext<ChatStore | null>(null)
export const useChatStore = () => useContext(Context)
const EMPTY = { packDrafts: [], deletedDrafts: [], chats: [], drafts: [], ready: false, saving: false, error: '', active: [], bindings: new Map(), dirty: false } as ReturnType<ChatStore['getSnapshot']>
const noop = () => () => {}
export function useChats() {
  const store = useChatStore()
  return { store, ...useSyncExternalStore(store?.subscribe ?? noop, store?.getSnapshot ?? (() => EMPTY), () => EMPTY) }
}
export function ChatProvider({ children }: { children: ReactNode }) {
  const project = useFileListing().data?.root
  const store = useMemo(() => project ? new ChatStore(project) : null, [project])
  useEffect(() => {
    if (!store) return
    void store.load()
    // Retry only initial hydration. Never reload an established transcript over
    // local edits, an active worker, a pending approval, or an uncertain save.
    const recover = () => { if (document.visibilityState !== 'hidden') void store.retryInitialLoad() }
    const timer = window.setInterval(recover, privateQuery.refetchInterval)
    window.addEventListener('focus', recover)
    document.addEventListener('visibilitychange', recover)
    return () => { clearInterval(timer); window.removeEventListener('focus', recover); document.removeEventListener('visibilitychange', recover) }
  }, [store])
  useEffect(() => store?.retain(), [store])
  return <Context.Provider value={store}><ChatWorkers />{children}</Context.Provider>
}
function ChatWorkers() {
  const { store, active, chats, drafts, dirty } = useChats()
  useEffect(() => {
    const leave = (event: BeforeUnloadEvent) => { if (dirty || store?.running) event.preventDefault() }
    const visibility = () => { if (document.visibilityState === 'hidden') void store?.flush() }
    window.addEventListener('beforeunload', leave); document.addEventListener('visibilitychange', visibility)
    return () => { window.removeEventListener('beforeunload', leave); document.removeEventListener('visibilitychange', visibility) }
  }, [store, dirty])
  return <>{active.map(id => { const chat = chats.find(chat => chat.id === id) ?? drafts.find(chat => chat.id === id); return store && chat ? <ChatWorker key={id} store={store} chat={chat} /> : null })}</>
}
const chatOf = (store: ChatStore, id: string) => { const snapshot = store.getSnapshot(); return [...snapshot.chats, ...snapshot.drafts].find(chat => chat.id === id) }
function ChatWorker({ store, chat }: { store: ChatStore; chat: Chat }) {
  const mcp = useMcp()
  const project = useFileListing().data?.root
 const queryClient=useQueryClient()
  const currentClient = useRef(mcp.client)
  currentClient.current = mcp.client
  const effective = useEffectiveConfig()
  const research = effective.config.research
  // Link reading is offered on the same terms as the composer's Add link: the
  // managed local gateway's catalog advertises the web source, or the desk-level
  // file declares one, and document processing is enabled. Read per turn, so a
  // setting changed mid-chat holds.
  const local = effective.desk?.localGateway?.status === 'ready' && !effective.desk?.decoded?.values?.research?.gateway
  const catalog = useConnections(local)
  const readable = linkReadable(research, { local, catalogWeb: catalog.web })
  const discovery=websiteReadable(research,{local,catalogWeb:catalog.web,catalogDiscovery:catalog.discovery})
  const searchConnections=useSearchConnections(), searchPreference=useSearchPreference()
  const researchEnabled=!!searchPreference.data&&!searchPreference.isError&&(chat.researchMode??searchPreference.data.value.mode)==='auto'
  const connection=searchConnections.available&&!searchConnections.isError&&searchConnections.data?.connections.find(c=>c.id===(chat.searchConnection??searchPreference.data?.value.connection)) || undefined
  const latest = useRef({ research, readable, discovery, researchEnabled, connection, mode: chat.mode })
  latest.current = { research, readable, discovery, researchEnabled, connection, mode: chat.mode }
  const draftTools = useMemo(() => { let currentTurns: Parameters<typeof researchChoice>[0]
  const choice = () => researchChoice(currentTurns, latest.current.mode, latest.current.researchEnabled)
  const links = linkReading({
    available: () => latest.current.readable,
    researchEnabled:()=>latest.current.researchEnabled && choice()!=='provided',
    search:{
      connection:()=>latest.current.connection,
      references:()=>chatOf(store,chat.id)?.searches??[],
      add:ref=>{const current=chatOf(store,chat.id);if(current)store.update(chat.id,{searches:[...(current.searches??[]),ref]})}
    },
    discoveryAvailable:()=>latest.current.discovery,
    websites:()=>chatOf(store,chat.id)?.websites??[],
    addWebsite:ref=>{const current=chatOf(store,chat.id);if(current)store.update(chat.id,{websites:[...(current.websites??[]).filter(w=>w.seed!==ref.seed),ref]})},
    config: () => latest.current.research,
    documents: () => chatOf(store, chat.id)?.documents ?? [],
    addDocument: (attachment: ChatAttachment) => {
      const current = chatOf(store, chat.id)
      if (current) store.update(chat.id, { documents: [...(current.documents ?? []).filter(file => file.id !== attachment.id), attachment] })
    },
    log: text => recordActivity(text, 'research')
  }); const graphs = graphChatTools({client: () => currentClient.current, chatId: chat.id, addDraft: async (draft,signal) => {
    const current = chatOf(store, chat.id)
    if (!current) throw new Error('This conversation is no longer available.')
    if ((current.graphDrafts?.length ?? 0) >= 16) throw new Error('This chat has 16 graph drafts. Start a new chat to create another.')
    const retained={...draft,originChatId:chat.id,...(current.policySources?.length?{policySources:current.policySources}:{})}
    await retainGraphDraft(retained,undefined,signal)
    await queryClient.invalidateQueries({queryKey:GRAPH_DRAFTS_KEY})
    signal.throwIfAborted()
    store.update(chat.id, {graphDrafts: [...(current.graphDrafts ?? []), draft]})
    await store.checkpoint(signal)
  }})
  const dialects = dialectChatTools({afterTurnId:()=>currentTurns?.id,retainAction:async(action,signal)=>{
    const current=chatOf(store,chat.id)
    if(!current||store.getSnapshot().error)throw Error('Save or recover this chat before proposing a dialect.')
    const actions=current.inputActions??[],exists=actions.some(item=>item.id===action.id)
    if(!exists&&actions.length>=32)throw Error('This chat has 32 actions. Start a new chat to propose another.')
    store.update(chat.id,{inputActions:exists?actions.map(item=>item.id===action.id?action:item):[...actions,action]})
    const binding=store.getSnapshot().bindings.get(chat.id)
    if(binding?.run)store.report(chat.id,{...binding,state:binding.run.getSnapshot()})
    await store.checkpoint(signal)
  }})
  const tests = testChatTools({
    chatId: chat.id,
    target: async (requested, signal) => {
      const current = chatOf(store, chat.id)
      if (!current) throw Error('This conversation is no longer available.')
      const owner = requested ?? current.pack?.id ?? current.draftId
      if (!owner) throw Error('Choose a pack from list_decisions or open its Tests page.')
      const snapshot = store.getSnapshot(), draft = snapshot.packDrafts.find(item => item.id === owner)
      if (draft && !draft.finalized) {
        const candidate = (current.draftId === owner ? snapshot.bindings.get(chat.id)?.state : undefined)?.candidates.at(-1) ?? draft.checkpoint.state.candidates.at(-1)
        if (!candidate) throw Error('This draft has no pack to test yet.')
        return { owner, text: candidate.text, title: draft.title, draft: true }
      }
      const config = JSON.parse((await readFile('jpack.json', signal)).content)
      if (!config.packs || !Object.hasOwn(config.packs, owner) || typeof config.packs[owner]?.path !== 'string') throw Error('Choose a declared pack or the current draft.')
      const file = await readFile(config.packs[owner].path, signal)
      return { owner, text: file.content, title: String(JSON.parse(file.content).title ?? owner), draft: false }
    },
    suite: async owner => (await readTests()).content.suites[owner] ?? emptySuite(),
    call: async (name, args) => {
      if (!currentClient.current) throw Error('Connect to the runtime to check test proposals.')
      return await currentClient.current.callTool({ name, arguments: args }) as McpToolResult
    },
    guidance: async (text, signal) => {
      if (!currentClient.current) throw Error('Connect to the runtime to read test instructions.')
      const result = await currentClient.current.getPrompt({ name: 'test_pack', arguments: { pack: text } }, { signal })
      return result.messages.flatMap(message => message.content.type === 'text' ? [message.content.text] : []).join('\n\n')
    },
    sources: () => { const current = chatOf(store, chat.id); return current?.documents ?? [] },
    request: () => currentTurns?.text ?? '',
    model: () => chatOf(store, chat.id)?.model ?? '',
    save: async (owner, proposal, expected) => {
      const reply = await updateTests(owner, suite => {
        if (jsonIdentity(suite.cases) !== jsonIdentity(expected.cases)) throw Error('The test cases changed. Read them again before proposing changes.')
        return { ...suite, proposals: [...(suite.proposals ?? []), proposal] }
      }, project)
      queryClient.setQueryData([...TESTS_KEY, project ?? null], reply)
    },
  })
  return (context: Parameters<typeof links>[0]) => {
    currentTurns = [...context.turns()].reverse().find(turn=>turn.role==='user')
    const inputs=dialectRecipeChatTools({afterTurnId:()=>[...context.turns()].reverse().find(turn=>turn.role==='user')?.id,attribution:()=>{const c=chatOf(store,chat.id);return c?.aiConnection&&c.model?{connection:c.aiConnection,model:c.model}:undefined},addDraft:async draft=>{
      const current=chatOf(store,chat.id)
      if(!current)throw new DialectRecipeDraftError('chat-unavailable')
      if((current.inputDrafts?.length??0)>=16)throw new DialectRecipeDraftError('draft-limit')
      await retainDialectProposal(chat.id,draft)
      void queryClient.invalidateQueries({queryKey:DIALECT_DRAFTS_KEY})
      const live=chatOf(store,chat.id)
      if(live)store.update(chat.id,{inputDrafts:[...(live.inputDrafts??[]),draft]})
    }})
    const inputActions=dialectActionChatTools({chatId:chat.id,beforeResearch:inputId=>{
      const current=chatOf(store,chat.id)
      if(!current||store.getSnapshot().error)throw Error('Save or recover this chat before requesting research.')
      if((current.inputActions?.length??0)>=32)throw Error('This chat has 32 input actions. Start a new chat to request another.')
      if(dialectRecipeIsEditing(inputId))throw Error('Save or discard Recipe edits before requesting research.')
    },onResearchRequested:inputId=>{void queryClient.invalidateQueries({queryKey:approvalKey(chat.id)});void queryClient.invalidateQueries({queryKey:['input-research',inputId]})},afterTurnId:()=>[...context.turns()].reverse().find(turn=>turn.role==='user')?.id,addAction:action=>{
      const current=chatOf(store,chat.id)
      if(!current)throw Error('This conversation is no longer available.')
      if((current.inputActions?.length??0)>=32)throw Error('This chat has 32 input actions. Start a new chat to propose another.')
      store.update(chat.id,{inputActions:[...(current.inputActions??[]),action]})
    }})
    const policies=policyChatTools({id:chatOf(store,chat.id)?.policy?.id,request:()=>chatOf(store,chat.id)?.policyRequest,afterTurnId:()=>currentTurns?.id,retainInterpretation:async(proposal,signal)=>{
     const current=chatOf(store,chat.id);if(!current||current.interaction==='plan'||store.getSnapshot().error)throw Error('Save or recover this conversation in Work before proposing a pack interpretation.')
     if((current.policyInterpretations?.length??0)>=16)throw Error('This conversation has 16 Policy interpretation proposals. Start a new chat to propose another.')
     store.update(chat.id,{policyInterpretations:[...(current.policyInterpretations??[]),proposal]})
     const active=store.getSnapshot().bindings.get(chat.id);if(active?.run)store.report(chat.id,{...active,state:active.run.getSnapshot()});await store.checkpoint(signal)
    },retain:async(proposal,signal)=>{
     const current=chatOf(store,chat.id);if(!current||current.interaction==='plan'||store.getSnapshot().error)throw Error('Save or recover this conversation in Work before proposing a Policy update.');
     if(!(current.policyProposals??[]).some(p=>p.id===proposal.id)&&(current.policyProposals?.length??0)>=16)throw Error('This conversation has 16 Policy proposals. Start a new chat to propose another.');
     const proposals=current.policyProposals??[];store.update(chat.id,{policyProposals:proposals.some(p=>p.id===proposal.id)?proposals.map(p=>p.id===proposal.id?proposal:p):[...proposals,proposal]});
     const active=store.getSnapshot().bindings.get(chat.id);if(active?.run)store.report(chat.id,{...active,state:active.run.getSnapshot()});await store.checkpoint(signal)
    }})
    return [...(chatOf(store,chat.id)?.interaction==='plan'?planTools(store,chat.id):[]),...projectMemoryTools(()=>{const turn=[...context.turns()].reverse().find(t=>t.role==='user');return turn?.id?{chatId:chat.id,turnId:turn.id}:undefined},async signal=>{const binding=store.getSnapshot().bindings.get(chat.id);if(binding?.run)store.report(chat.id,{...binding,state:binding.run.getSnapshot()});await store.checkpoint(signal)},()=>{void queryClient.invalidateQueries({queryKey:MEMORY_KEY})}),questionTool,...policies,...tests,...dialects,...inputs,...inputActions,...gateResearchTools(links(context), () => researchChoice([...context.turns()].reverse().find(turn=>turn.role==='user'),latest.current.mode,latest.current.researchEnabled)), ...graphs]
  }
  }, [store, chat.id,queryClient,project])
  const binding = useResearchRun({ authorizeContext:chat.policy||chat.policySources?.length?authorizePolicyChat:undefined, savedPackReview:chat.pack ? {
    context:async(basis,signal)=>{const saved=await readTests();signal.throwIfAborted();if(saved.project!==project)throw Error('The project changed during the checks.');return packReviewContext(basis,saved.content.suites[basis.packId]??emptySuite())},
    target:async signal=>{const pack=chatOf(store,chat.id)?.pack;if(!pack)throw Error('The linked pack is no longer available.');return {packId:pack.id,path:pack.path,text:(await readFile(pack.path,signal)).content}},
    check:async(candidate,signal)=>{const pack=chatOf(store,chat.id)?.pack;if(!pack||!mcp.client)throw Error('The pack or runtime is no longer available.');return checkPackProposal(candidate,async()=>{const saved=await readTests();if(saved.project!==project)throw Error('The project changed during the checks.');return saved.content.suites[pack.id]??emptySuite()},async(name,args)=>await mcp.client!.callTool({name,arguments:args}) as McpToolResult,signal)}
  }:undefined, executionContext:()=>{const c=chatOf(store,chat.id);return {...(c?.policySources?.length?{documents:policySourceDocuments(c.policySources)}:{}),phase:c?.interaction??'work',plan:c?.plans?.at(-1),...(c?.policy?{artifactProposals:false}:{})}},checkpointDelegation:async signal=>{
    const current=store.getSnapshot().bindings.get(chat.id)
    if(!current?.run)throw Error('This conversation is no longer available.')
    store.report(chat.id,{...current,state:current.run.getSnapshot()})
    await store.checkpoint(signal)
  }, connectionId:chat.aiConnection, apiThinking:chat.apiThinking, model: chat.model, reasoning: chat.reasoning, mode: chat.mode, adversarialReview: chat.adversarialReview, researchPolicy:turn=>(chatOf(store,chat.id)?.policySources?.length?POLICY_SOURCE_INSTRUCTIONS+'\n\n':'')+(chatOf(store,chat.id)?.policyInterpretationKind==='graph'?'GRAPH INTERPRETATION: Use list_decisions and the shared graph authoring tools. Propose a graph through propose_graph, using only declared saved packs. Request clarification when a required pack is missing. A graph proposal is private authoring work, not governing adoption, coverage certification or a job. Do not emit a pack proposal fence.\n\n':'')+(chatOf(store,chat.id)?.policy?POLICY_CHAT_INSTRUCTIONS+'\n\n':'')+QUESTION_INSTRUCTIONS + '\n\nUse read_dialect and propose_dialect to translate saved record columns into a saved pack’s facts and evidence. Mapping proposals require review. Use read_dialect_publication and propose_dialect_publication to check and propose a saved mapping version. Publication requires the shared explicit Ask me or one-publication Auto allowance; research permission never authorizes publication. Existing jobs keep their pins. Keep missing data unknown and do not infer structured values from prose. ' + '\n\n' + (chatOf(store,chat.id)?.input ? 'INPUTS WORKSPACE: Use the saved Dialects context and input tools for recipe and record requests. Use propose_dialect_recipe only for a new dialect; use propose_dialect_action for existing recipe changes, stopping a named run, or evaluating explicit selected records. Use request_dialect_research to request a run of a saved recipe. Array patches replace the whole field/criterion list, so read the current recipe first and preserve other entries. Proposal tools create review cards only. request_dialect_research starts work only when the server returns started under an explicit, unused user allowance; otherwise it creates a review card and you must wait. Never claim a save, start, stop, evaluation or human review happened from a proposal. Auto never permits editing recipes, bypassing input review or spending beyond the approved run limits. AI-generated records require a current human Ready for evaluation review before pack evaluation. Do not create pack/graph proposals unless explicitly requested. A recipe research run is a separate confirmed Desk operation, not ordinary chat web browsing. ' : '') + (researchChoice(turn, latest.current.mode, latest.current.researchEnabled)==='provided'
    ? 'WEB RESEARCH POLICY: Use only sources supplied in this conversation. Web search and website exploration are disabled for this conversation; do not request configuration as a workaround.'
    : `WEB RESEARCH POLICY: ${researchChoice(turn,latest.current.mode,latest.current.researchEnabled) === 'research' ? 'Research requested. For substantive research requests, use the available web tools and cite sources actually read. Ask for a source or explain a missing capability when necessary.' : 'Chat. Read supplied links directly. When broader research is needed, call search_sources or explore_website once to request the person’s choice. The host will pause before any search or exploration; stop and wait for the choice.'} ${latest.current.connection?'The configured search connection is '+latest.current.connection.provider+'.':'No web-search connection is selected. If search is needed, call search_sources once to show its setup action in chat. Supplied-link reading and website exploration are independent; read a supplied website directly, including a bare domain such as judgmentpack.org (HTTPS is assumed). Never ask the person to retype a domain with https://.'}`), draftTools, documents: () => chatOf(store, chat.id)?.documents ?? [] })
  // A newly opened chat mounts its queries at the same time as its model.
  // Do not let that first turn freeze an incomplete tool list or a false
  // 'not configured' policy while saved search settings are still loading.
  const searchBlocked = chat.researchMode === 'provided' ? ''
    : searchPreference.isPending ? sourceMessage('Loading search settings…')
    : searchPreference.isError ? sourceMessage('Search settings could not be read. Reload before sending.')
    : researchEnabled && (catalog.loading || searchConnections.loading) ? sourceMessage('Checking search connections…')
    : researchEnabled && (catalog.isError || searchConnections.isError) ? sourceMessage('Search connections could not be checked. Retry in Web search settings.') : ''
  // A saved chat that predates AI connections names no connection. Its next
  // send waits for an explicit choice rather than taking today's default.
  const legacyTargetMissing=!!effective.aiConnections&&!chat.aiConnection&&!!chat.checkpoint?.state.turns.length
  const blocked = legacyTargetMissing?sourceMessage('Choose an AI connection for this saved chat.'):binding.blocked || searchBlocked
  const initial = useRef(chat.checkpoint)
  const restoring = useRef(false)
  const [restored, setRestored] = useState(!initial.current)
  useEffect(() => {
    if (restoring.current || !initial.current || !binding.run || !binding.ledger) return
    restoring.current = true
    try {
      restoreLedger(binding.ledger, initial.current.sources)
      void binding.run.restore(initial.current.state).then(() => setRestored(true), error => store.problem(sourceMessage('Chat could not be restored: {{reason}}', { reason: error.message })))
    } catch (error) { store.problem(sourceMessage('Chat could not be restored: {{reason}}', { reason: (error as Error).message })) }
  }, [binding.run, binding.ledger, store])
  useDialectPublicationAutoSave({chat,store,binding,restored})
  usePackAutoSave({chat,store,binding,restored,project,call:async(name,args)=>{if(!mcp.client)throw Error('Runtime unavailable');return await mcp.client.callTool({name,arguments:args}) as McpToolResult}})
  useEffect(() => {
    if (restored) store.report(chat.id, {...binding, blocked})
  }, [store, chat.id, restored, binding.run, binding.state, binding.ledger, binding.sources, blocked, binding.model, binding.researchConfigured])
  return null
}
