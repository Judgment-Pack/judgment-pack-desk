import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { HostTool } from '@judgment-pack/harness/engine'
import { PROPOSE_PACK_SCHEMA } from '@judgment-pack/harness/engines/contract'
import { useResearchRun } from './useResearchRun'
import type { AuthoringMode } from './mode'

const fake = vi.hoisted(() => ({ engine:'test', allowed:['test-model','second-model'], models:{isPending:false,isError:false,error:new Error('Models unavailable'),data:{models:[{id:'test-model'},{id:'second-model'}]}}, bind:vi.fn((slot:any)=>({effort:slot.agent?.effort})), session: vi.fn(), tools: vi.fn(), policy: vi.fn(() => 'WEB POLICY FOR THIS MESSAGE'), load: vi.fn(), gateway: { authority: 'test', signer: { public: 'test' } } }))
vi.mock('../documents/client', async (original) => ({ ...(await original<typeof import('../documents/client')>()), loadDocument: fake.load }))
vi.mock('../assistant/providers',()=>({useProviderModels:()=>fake.models}))
vi.mock('../assistant/target', async () => ({ bindExecution: fake.bind, selectedAssistant: () => ({ models: fake.allowed, model: 'test-model', tools: [] }) }))
vi.mock('../assistant/useAssistantSlot', () => ({ assistantReady: () => true, useAssistantSlot: () => ({ state: 'ready', endpoint: 'https://example.invalid', keyStatus: 'stored', keyPresent: true, engine: fake.engine, thinking: 'off', agent:{model:'test-model',provider:'openai',authMethod:'subscription',tools:[],effort:'medium'}, ...host.slot }) }))
vi.mock('../assistant/pickedModel', () => ({ usePickedModel: () => ({ model: 'test-model', models: ['test-model'] }) }))
vi.mock('@judgment-pack/harness/engines/index', () => ({ loadEngine: async () => ({}) }))
vi.mock('@judgment-pack/harness/session', () => ({ runAssistantSession: fake.session, openAssistantConnection: () => ({ ready: Promise.resolve({ tools: [], callTool: async () => ({}) }), close: async () => {} }) }))
vi.mock('../files/queries', () => ({ useFileListing: () => ({ data: { root: '/test' } }) }))
// The rest of the module stays the Desk's own: the harness's host wiring reads its error and sentence.
vi.mock('../mcp/session', async original => ({ ...await original<typeof import('../mcp/session')>(), sessionBearer: async () => 'test-session' }))
const host = vi.hoisted(() => ({ slot: {} as Record<string, string>, mcp: { status: 'disconnected', client: null } as { status: string; client: unknown }, memory: vi.fn(async (): Promise<boolean | undefined> => undefined), runtime: vi.fn(async (): Promise<{ identity: string; version: string } | undefined> => undefined) }))
vi.mock('../mcp/McpProvider', () => ({ useMcp: () => host.mcp }))
vi.mock('../assistant/runContext', () => ({ memoryInUse: host.memory, runtimeIdentityOf: host.runtime }))
vi.mock('../mcp/prompts', () => ({ AUTHOR_PACK_PROMPT: 'author_pack', TEST_PACK_PROMPT: 'test_pack', usePromptNames: () => ({ data: [] }), usePromptText: () => ({}) }))
vi.mock('../config/DeskConfigProvider', () => ({ useEffectiveConfig: () => ({ config: { research: { gateway: fake.gateway, sources: { search: null, read: null }, limits: { seconds: 30 } } } }) }))
vi.mock('../shell/consoleLog', () => ({ recordActivity: () => {} }))
afterEach(() => { cleanup(); vi.clearAllMocks();host.mcp={status:'disconnected',client:null};host.slot={};host.memory.mockImplementation(async()=>undefined);host.runtime.mockImplementation(async()=>undefined);fake.engine='test';fake.allowed=['test-model','second-model'];fake.models.isPending=false;fake.models.isError=false })

it('offers current web tools and policy on the first Research message and its follow-up', async () => {
  const search: HostTool = { name: 'search_sources', description: 'Fixture search', inputSchema: {}, execute: vi.fn(async () => ({ content: [] })) }
  const read: HostTool = { ...search, name: 'read_link' }
  fake.tools.mockReturnValue([search, read])
  fake.session.mockImplementation(async (_engine, request, deliver) => {
    expect(request.hostTools.map((tool: HostTool) => tool.name)).toEqual(['get_authoring_instructions', 'search_sources', 'read_link', 'propose_pack'])
    expect(request.prompt).toContain('WEB POLICY FOR THIS MESSAGE')
    await request.hostTools.find((tool: HostTool) => tool.name === 'search_sources').execute({ query: 'public policy' }, request.signal)
    deliver({ type: 'message', text: 'Here is the research summary.' })
  })
  const { result, rerender } = renderHook(({ mode }: { mode: AuthoringMode }) => useResearchRun({ mode, draftTools: fake.tools, researchPolicy: fake.policy }), { initialProps: { mode: 'draft' as AuthoringMode } })
  rerender({ mode: 'web-research' })
  expect(result.current.blocked).toBe('')
  act(() => result.current.run!.start('Research public policy', []))
  await waitFor(() => expect(result.current.state.status).toBe('complete'))
  act(() => result.current.run!.send('Find another source'))
  await waitFor(() => expect(fake.session).toHaveBeenCalledTimes(2))
  await waitFor(() => expect(result.current.state.status).toBe('complete'))
  expect(search.execute).toHaveBeenCalledTimes(2)
  expect(result.current.state.candidates).toEqual([])
})

it('keeps supplied-link reading usable without a search provider', async () => {
  fake.tools.mockReturnValue([{ name: 'read_link', execute: vi.fn() }])
  fake.session.mockImplementation(async (_engine, request, deliver) => {
    expect(request.hostTools.map((tool: HostTool) => tool.name)).toEqual(['get_authoring_instructions', 'read_link', 'propose_pack'])
    deliver({ type: 'message', text: 'Please supply a URL, or configure a search connection.' })
  })
  const { result } = renderHook(() => useResearchRun({ mode: 'web-research', draftTools: fake.tools, researchPolicy: fake.policy }))
  expect(result.current.blocked).toBe('')
  act(() => result.current.run!.start('Research this', []))
  await waitFor(() => expect(result.current.state.status).toBe('complete'))
})

it('asks a chat draft to trace its citations again whenever the documents the chat keeps change', async () => {
  const { AuthoringRun } = await import('./run')
  const changed = vi.spyOn(AuthoringRun.prototype, 'basisChanged')
  const file = (pages: number[]) => [{ id: 'kept', name: 'Kept', text: '', document: { id: 'kept', digest: 'sha256:' + 'a'.repeat(64), pages, allowPartial: false } }]
  const { rerender } = renderHook(({ pages }: { pages: number[] }) => useResearchRun({ mode: 'draft', draftTools: fake.tools, documents: () => file(pages) }), { initialProps: { pages: [1] } })
  const after = changed.mock.calls.length
  rerender({ pages: [1] })
  expect(changed.mock.calls.length).toBe(after)
  rerender({ pages: [1, 2] })
  expect(changed.mock.calls.length).toBe(after + 1)
  changed.mockRestore()
})

it('ends its run\'s traces at rest when its owner unmounts', async () => {
  const { AuthoringRun } = await import('./run')
  const detached = vi.spyOn(AuthoringRun.prototype, 'detach')
  const attached = vi.spyOn(AuthoringRun.prototype, 'attach')
  const { unmount } = renderHook(() => useResearchRun({ mode: 'draft', draftTools: fake.tools }))
  expect(attached).toHaveBeenCalled()
  expect(detached).not.toHaveBeenCalled()
  unmount()
  expect(detached).toHaveBeenCalledOnce()
  detached.mockRestore()
  attached.mockRestore()
})

it('traces a reopened chat draft through its own ports: the configured pin, the kept documents, and a load its unmount ends', async () => {
  const fixture = (await import('../documents/__fixtures__/web-snapshot.json')).default
  const record = structuredClone(fixture) as unknown as import('../documents/record').DocumentRecord
  const quote = 'First fact & second.'
  const reference = { id: '12345678-1234-1234-1234-000000000001', digest: 'sha256:' + '1'.repeat(64), pages: [1], allowPartial: false }
  const kept = [{ id: reference.id, name: 'Example', text: '', document: reference }]
  const pack = { specVersion: '0.2.0-draft', id: 'https://example.org/p', version: '0.1.0', title: 'Kept page', sources: [{ id: 'page', title: 'Page', locator: { kind: 'uri', value: record.provenance.source.url }, citation: { location: `attachment:${reference.id}/${reference.digest}/page/1`, excerpt: quote } }] }
  const { INITIAL_STATE } = await import('./run')
  const saved = { ...INITIAL_STATE, status: 'needs-input' as const, turns: [{ id: 't1', role: 'user' as const, kind: 'brief' as const, text: 'Draft it.', at: '2026-09-30T00:00:00Z' }],
    candidates: [{ revision: 1, producedBy: 'conversation' as const, document: pack, text: JSON.stringify(pack), digest: '' }] }
  let release!: () => void
  const signals: AbortSignal[] = []
  fake.load.mockImplementation((_reference: unknown, _pin: unknown, signal: AbortSignal) => {
    signals.push(signal)
    return new Promise((resolve) => { release = () => resolve({ record, digest: reference.digest, object: { version: 1, original: { name: 'Example', mediaType: 'text/plain', bytes: '', sha256: '' } } }) })
  })
  const { result, rerender, unmount } = renderHook(({ documents }: { documents: typeof kept }) => useResearchRun({ mode: 'draft', draftTools: fake.tools, documents: () => documents }), { initialProps: { documents: kept } })
  await act(() => result.current.run!.restore(saved))
  await waitFor(() => expect(fake.load).toHaveBeenCalledOnce())
  // Verified under the pin the desk is configured with, as it stands.
  expect(fake.load.mock.calls[0]![0]).toEqual(reference)
  expect(fake.load.mock.calls[0]![1]).toEqual({ authority: 'test', signer: { public: 'test' } })
  act(() => release())
  await waitFor(() => expect(result.current.state.citations).toMatchObject([{ sourceId: 'page', traced: true }]))
  // The chat stops keeping the document: traced again, through the hook alone.
  rerender({ documents: [] })
  await waitFor(() => expect(result.current.state.citations).toMatchObject([{ sourceId: 'page', traced: false }]))
  // A load still held when the owner goes away is ended with it.
  rerender({ documents: kept })
  await waitFor(() => expect(fake.load).toHaveBeenCalledTimes(2))
  expect(signals[1]!.aborted).toBe(false)
  unmount()
  expect(signals[1]!.aborted).toBe(true)
})

it('traces a reopened chat draft again under a pin that moves while it loads, and the trace it superseded loads nothing more', async () => {
  const fixture = (await import('../documents/__fixtures__/web-snapshot.json')).default
  const record = structuredClone(fixture) as unknown as import('../documents/record').DocumentRecord
  const quote = 'First fact & second.'
  const reference = (n: number) => ({ id: `12345678-1234-1234-1234-00000000000${n}`, digest: 'sha256:' + String(n).repeat(64), pages: [1], allowPartial: false })
  const kept = [1, 2].map(n => ({ id: reference(n).id, name: `Example ${n}`, text: '', document: reference(n) }))
  const pack = { specVersion: '0.2.0-draft', id: 'https://example.org/p', version: '0.1.0', title: 'Kept pages',
    sources: [1, 2].map(n => ({ id: `page-${n}`, title: 'Page', locator: { kind: 'uri', value: record.provenance.source.url }, citation: { location: `attachment:${reference(n).id}/${reference(n).digest}/page/1`, excerpt: quote } })) }
  const { INITIAL_STATE } = await import('./run')
  const saved = { ...INITIAL_STATE, status: 'needs-input' as const, turns: [{ id: 't1', role: 'user' as const, kind: 'brief' as const, text: 'Draft it.', at: '2026-09-30T00:00:00Z' }],
    candidates: [{ revision: 1, producedBy: 'conversation' as const, document: pack, text: JSON.stringify(pack), digest: '' }] }
  const A = { authority: 'gateway-a', signer: { public: 'a' } }, B = { authority: 'gateway-b', signer: { public: 'b' } }
  fake.gateway = A
  let release!: () => void
  const verified = (ref: { digest: string }) => ({ record, digest: ref.digest, object: { version: 1, original: { name: 'Example', mediaType: 'text/plain', bytes: '', sha256: '' } } })
  fake.load.mockImplementation((ref: { id: string; digest: string }) => ref.id.endsWith('1') && fake.load.mock.calls.length === 1
    ? new Promise(resolve => { release = () => resolve(verified(ref)) }) : Promise.resolve(verified(ref)))
  const { result, rerender } = renderHook(() => useResearchRun({ mode: 'draft', draftTools: fake.tools, documents: () => kept }))
  await act(() => result.current.run!.restore(saved))
  await waitFor(() => expect(fake.load).toHaveBeenCalledOnce())
  // The configured pin moves while the first document loads.
  fake.gateway = B
  rerender()
  act(() => release())
  await waitFor(() => expect(result.current.state.tracing).toBe(false))
  // The move superseded the trace under A, which asked for nothing more once
  // it was; the trace that landed verified both documents under B.
  expect(fake.load.mock.calls.map(call => [(call[0] as { id: string }).id.slice(-1), (call[1] as { authority: string }).authority])).toEqual([['1', 'gateway-a'], ['1', 'gateway-b'], ['2', 'gateway-b']])
  expect(result.current.state.citations.map(citation => citation.traced)).toEqual([true, true])
  expect(result.current.state.tracedBasis).toBe(result.current.run!.basisNow())
  fake.gateway = { authority: 'test', signer: { public: 'test' } }
})

it('passes chat reasoning into each turn independently of Chat or Research mode',async()=>{
 fake.tools.mockReturnValue([])
 fake.session.mockImplementation(async(_engine,_request,deliver)=>{deliver({type:'message',text:'Connection check.'})})
 const {result,rerender}=renderHook(({mode,effort}:{mode:AuthoringMode;effort:'low'|'high'|null})=>useResearchRun({mode,reasoning:{model:'test-model',effort},draftTools:fake.tools}),{initialProps:{mode:'draft' as AuthoringMode,effort:'low' as 'low'|'high'|null}})
 act(()=>result.current.run!.start('First',[]));await waitFor(()=>expect(result.current.state.status).toBe('complete'))
 expect(fake.session.mock.lastCall?.[1].effort).toBe('low')
 rerender({mode:'web-research',effort:'high'})
 act(()=>result.current.run!.send('Second'));await waitFor(()=>expect(result.current.state.status).toBe('complete'))
 expect(fake.session.mock.lastCall?.[1].effort).toBe('high')
 rerender({mode:'draft',effort:null})
 act(()=>result.current.run!.send('Third'));await waitFor(()=>expect(result.current.state.status).toBe('complete'))
 expect(fake.session.mock.lastCall?.[1].effort).toBeUndefined()
})

it('uses the chat-selected subscription model for the actual turn without inheriting another model effort',async()=>{
 fake.engine='codex';fake.tools.mockReturnValue([])
 fake.session.mockImplementation(async(_engine,_request,deliver)=>deliver({type:'message',text:'Selected model ran.'}))
 const {result}=renderHook(()=>useResearchRun({model:'second-model',mode:'draft',reasoning:{model:'test-model',effort:'high'},draftTools:fake.tools}))
 expect(result.current.model).toBe('second-model');expect(result.current.blocked).toBe('')
 act(()=>result.current.run!.start('Test',[]));await waitFor(()=>expect(result.current.state.status).toBe('complete'))
 expect(fake.bind.mock.lastCall?.[0].agent).toMatchObject({model:'second-model',effort:undefined,tools:[]})
 expect((fake.bind.mock.lastCall as unknown[])?.[1]).toBe('second-model')
 expect(fake.session.mock.lastCall?.[1].effort).toBeUndefined()
})
it('blocks an unavailable saved subscription model instead of substituting the default',async()=>{
 fake.allowed=['test-model','removed-model'];fake.engine='codex';fake.tools.mockReturnValue([])
 const {result}=renderHook(()=>useResearchRun({model:'removed-model',mode:'draft',draftTools:fake.tools}))
 expect(result.current.model).toBe('removed-model');expect(result.current.blocked).toContain('This model is no longer available')
 act(()=>result.current.run!.start('Test',[]));await waitFor(()=>expect(result.current.state.status).not.toBe('running'))
 expect(fake.session).not.toHaveBeenCalled();expect(fake.bind).not.toHaveBeenCalled()
})
it('waits for the subscription catalog and keeps read failures visible before sending',()=>{
 fake.engine='codex';fake.models.isPending=true
 const {result,rerender}=renderHook(()=>useResearchRun({model:'second-model',mode:'draft',draftTools:fake.tools}))
 expect(result.current.blocked).toBe('Loading models…')
 fake.models.isPending=false;fake.models.isError=true;rerender();expect(result.current.blocked).toBe('Models unavailable')
})

it('blocks a removed grant before binding a chat-selected subscription model',async()=>{
 fake.engine='codex';fake.allowed=['test-model'];fake.tools.mockReturnValue([])
 const {result}=renderHook(()=>useResearchRun({model:'second-model',mode:'draft',draftTools:fake.tools}))
 expect(result.current.blocked).toBe('Choose an enabled model in Admin › Assistant.')
 act(()=>result.current.run!.start('Test',[]));await waitFor(()=>expect(result.current.state.status).not.toBe('running'))
 expect(fake.session).not.toHaveBeenCalled();expect(fake.bind).not.toHaveBeenCalled()
})

it('blocks a chat-selected API model removed from this desk allowlist',async()=>{
 fake.engine='vercel';fake.allowed=['test-model'];fake.tools.mockReturnValue([])
 const {result}=renderHook(()=>useResearchRun({model:'second-model',mode:'draft',draftTools:fake.tools}))
 expect(result.current.model).toBe('second-model');expect(result.current.blocked).toBe('Choose an enabled model in Admin › Assistant.')
 act(()=>result.current.run!.start('Test',[]));await waitFor(()=>expect(result.current.state.status).not.toBe('running'))
 expect(fake.session).not.toHaveBeenCalled();expect(fake.bind).not.toHaveBeenCalled()
})

it('merges private source context with original conversation documents once on each resumed turn',async()=>{
 const {policySourceDocuments}=await import('../policies/authoringSource')
 const source={version:1 as const,policyId:'policy_'+'a'.repeat(48),revisionId:'revision_'+'b'.repeat(48),candidateDigest:'c'.repeat(64),content:{sha256:'c'.repeat(64),bytes:4},title:'Policy',artifactVersion:'1.0.0',narrative:'Text'}
 const authorize=vi.fn(async()=>{})
 fake.tools.mockReturnValue([])
 fake.session.mockImplementation(async(_engine,request,deliver)=>{
  expect(request.context.documents.map((doc:{id:string})=>doc.id)).toEqual(['conversation','policy-source-0'])
  expect(request.context.documents[0].value.turns.at(-1).text).toContain('Interpret')
  expect(request.context.documents[1].value).toEqual(source)
  expect(request.context.authorize).toBe(authorize)
  deliver({type:'message',text:'Review the ambiguous requirement.'})
 })
 const {result}=renderHook(()=>useResearchRun({mode:'draft',draftTools:fake.tools,authorizeContext:authorize,executionContext:()=>({documents:policySourceDocuments([source])})}))
 expect(fake.session).not.toHaveBeenCalled()
 act(()=>result.current.run!.start('Interpret selected Policy',[]))
 await waitFor(()=>expect(result.current.state.status).toBe('complete'))
 act(()=>result.current.run!.send('Interpret the remaining clause'))
 await waitFor(()=>expect(fake.session).toHaveBeenCalledTimes(2))
 await waitFor(()=>expect(result.current.state.status).toBe('complete'))
})

it('offers propose_pack with the package\'s schema where a turn may propose a pack, and not in a conversation without artifact proposals',async()=>{
 fake.tools.mockReturnValue([])
 const offered:string[][]=[]
 fake.session.mockImplementation(async(_engine,request,deliver)=>{
  offered.push(request.hostTools.map((tool:HostTool)=>tool.name))
  const propose=request.hostTools.find((tool:HostTool)=>tool.name==='propose_pack')
  if(propose){expect(propose.inputSchema).toEqual(PROPOSE_PACK_SCHEMA);expect(propose.effect).toBe('proposal');expect(propose.delegation).toBeUndefined();await expect(propose.execute({document:{}},request.signal)).rejects.toThrow('read by the engine')}
  deliver({type:'message',text:'Answered.'})
 })
 const pack=renderHook(()=>useResearchRun({mode:'draft',draftTools:fake.tools,executionContext:()=>({phase:'work'})}))
 act(()=>pack.result.current.run!.start('Draft a pack',[]));await waitFor(()=>expect(pack.result.current.state.status).toBe('complete'))
 pack.unmount()
 const policy=renderHook(()=>useResearchRun({mode:'draft',draftTools:fake.tools,executionContext:()=>({phase:'work',artifactProposals:false})}))
 act(()=>policy.result.current.run!.start('Review the Policy',[]));await waitFor(()=>expect(policy.result.current.state.status).toBe('complete'))
 policy.unmount()
 expect(offered).toEqual([['get_authoring_instructions','propose_pack'],['get_authoring_instructions']])
 // An independent reviewer is never offered it: in a pack chat, a Policy chat, Plan and on the research page.
 // Each scope's turn port is driven as the run drives it, once as the author and once as the reviewer.
 const scopes:{name:string;options:Parameters<typeof useResearchRun>[0]}[]=[
  {name:'pack chat',options:{mode:'draft',draftTools:fake.tools,executionContext:()=>({phase:'work'})}},
  {name:'Policy chat',options:{mode:'draft',draftTools:fake.tools,executionContext:()=>({phase:'work',artifactProposals:false})}},
  {name:'Plan',options:{mode:'draft',draftTools:fake.tools,executionContext:()=>({phase:'plan'})}},
  {name:'research page',options:undefined}
 ]
 const seen:Record<string,{author:string[];reviewer:string[]}>={}
 for(const scope of scopes){
  const hook=renderHook(()=>useResearchRun(scope.options))
  const turn=(hook.result.current.run as unknown as {ports:{turn:(request:import('./run').TurnRequest,signal:AbortSignal,deliver:(event:unknown)=>void)=>Promise<void>}}).ports.turn
  const names:string[][]=[]
  fake.session.mockImplementation(async(_engine,request,deliver)=>{names.push(request.hostTools.map((tool:HostTool)=>tool.name));deliver({type:'end'})})
  for(const reviewer of [false,true])await act(()=>turn({prompt:'Write the cases.',hostTools:[],reviewer,conversation:!reviewer},new AbortController().signal,()=>{}))
  seen[scope.name]={author:names[0]!,reviewer:names[1]!}
  hook.unmount()
 }
 for(const scope of scopes)expect(seen[scope.name]!.reviewer,scope.name).toEqual([])
 expect(Object.fromEntries(scopes.map(scope=>[scope.name,seen[scope.name]!.author.includes('propose_pack')]))).toEqual({'pack chat':true,'Policy chat':false,'Plan':true,'research page':true})
 // Plan offers no proposal tool: the package's planning filter drops propose_pack by its effect.
 const {planningTool}=await import('@judgment-pack/harness/planning/policy')
 const {proposePackTool}=await import('../assistant/proposePack')
 expect(planningTool(proposePackTool())).toBe(false)
})

it('hands each turn the chat\'s context, the Runtime it reaches and the memory setting, and an independent reviewer none of the chat\'s',async()=>{
 const {ChatContext}=await import('@judgment-pack/harness/context/chat')
 const chat=new ChatContext()
 const identity={identity:'f'.repeat(64),version:'0.35.0'}
 host.mcp={status:'ready',client:{callTool:vi.fn()}}
 host.runtime.mockImplementation(async()=>identity)
 host.memory.mockImplementation(async()=>false)
 fake.tools.mockReturnValue([])
 const hook=renderHook(()=>useResearchRun({mode:'draft',draftTools:fake.tools,executionContext:()=>({phase:'work',chat})}))
 const turn=(hook.result.current.run as unknown as {ports:{turn:(request:import('./run').TurnRequest,signal:AbortSignal,deliver:(event:unknown)=>void)=>Promise<void>}}).ports.turn
 const contexts:Record<string,unknown>[]=[]
 fake.session.mockImplementation(async(_engine,request,deliver)=>{contexts.push(request.context);deliver({type:'end'})})
 for(const reviewer of [false,true])await act(()=>turn({prompt:'Write the cases.',hostTools:[],reviewer,conversation:!reviewer},new AbortController().signal,()=>{}))
 expect(contexts[0]).toMatchObject({runtime:identity,memory:false})
 expect(contexts[0]!.chat).toBe(chat)
 // The reviewer works from a scope of its own: the chat's ledger and read cache are not its to read.
 expect(contexts[1]).toMatchObject({runtime:identity,memory:false})
 expect(contexts[1]!.chat).toBeUndefined()
 // A memory setting that cannot be read, and a Runtime with no release identity, are left unsaid: the harness keeps the reserve and caches such reads for the run.
 host.runtime.mockImplementation(async()=>undefined);host.memory.mockImplementation(async()=>undefined)
 await act(()=>turn({prompt:'Write the cases.',hostTools:[],reviewer:false,conversation:true},new AbortController().signal,()=>{}))
 expect(contexts[2]).not.toHaveProperty('runtime')
 expect(contexts[2]).not.toHaveProperty('memory')
 hook.unmount()
})

it('declares the authoring instructions as a read that depends on the Runtime that serves it',async()=>{
 fake.tools.mockReturnValue([])
 let declared:unknown
 fake.session.mockImplementation(async(_engine,request,deliver)=>{declared=request.hostTools.find((tool:HostTool)=>tool.name==='get_authoring_instructions')?.contextPolicy;deliver({type:'message',text:'Answered.'})})
 const {result}=renderHook(()=>useResearchRun({mode:'draft',draftTools:fake.tools,executionContext:()=>({phase:'work'})}))
 act(()=>result.current.run!.start('Draft a pack',[]));await waitFor(()=>expect(result.current.state.status).toBe('complete'))
 expect(declared).toEqual({immutable:true,reference:true,runtime:true})
})

it('declares the saved tests and the conversation\'s cases to a critic that will run, and to no other turn',async()=>{
 fake.tools.mockReturnValue([])
 const test={source:'test' as const,id:'saved',facts:{claim:60},expectedDisposition:{kind:'decline',reasons:[],handoff:null}}
 const savedTests=vi.fn(async()=>[test])
 const sessions:Record<string,unknown>[]=[]
 fake.session.mockImplementation(async(_engine,request,deliver)=>{sessions.push(request);deliver({type:'end'})})
 const drive=async(options:Parameters<typeof useResearchRun>[0],reviewer=false)=>{
  const hook=renderHook(()=>useResearchRun(options))
  const turn=(hook.result.current.run as unknown as {ports:{turn:(request:import('./run').TurnRequest,signal:AbortSignal,deliver:(event:unknown)=>void)=>Promise<void>}}).ports.turn
  await act(()=>turn({prompt:'Draft it.',hostTools:[],reviewer,conversation:!reviewer},new AbortController().signal,()=>{}))
  hook.unmount()
  return sessions.at(-1)!
 }
 expect((await drive({mode:'draft',draftTools:fake.tools,adversarialReview:true,savedTests})).criticCases).toEqual([test])
 // No critic asked for: nothing is read, nothing declared.
 savedTests.mockClear()
 expect(await drive({mode:'draft',draftTools:fake.tools,savedTests})).not.toHaveProperty('criticCases')
 expect(savedTests).not.toHaveBeenCalled()
 // An independent reviewer runs no critic.
 expect(await drive({mode:'draft',draftTools:fake.tools,adversarialReview:true,savedTests},true)).not.toHaveProperty('criticCases')
 expect(savedTests).not.toHaveBeenCalled()
 // Tests that cannot be read leave the critic to review the proposal alone; they do not end the turn.
 const unreadable=vi.fn(async():Promise<never>=>{throw new Error('Test storage cannot be read.')})
 expect(await drive({mode:'draft',draftTools:fake.tools,adversarialReview:true,savedTests:unreadable})).not.toHaveProperty('criticCases')
 expect(unreadable).toHaveBeenCalled()
})

it('binds every run, an independent reviewer\'s too, to the connection its slot resolved',async()=>{
 fake.tools.mockReturnValue([])
 host.slot={connectionId:'ai-111111111111111111111111',connectionRevision:'a'.repeat(64),connectionName:'Open weights'}
 const hook=renderHook(()=>useResearchRun({mode:'draft',draftTools:fake.tools,executionContext:()=>({phase:'work'})}))
 const turn=(hook.result.current.run as unknown as {ports:{turn:(request:import('./run').TurnRequest,signal:AbortSignal,deliver:(event:unknown)=>void)=>Promise<void>}}).ports.turn
 const contexts:Record<string,unknown>[]=[]
 fake.session.mockImplementation(async(_engine,request,deliver)=>{contexts.push(request.context);deliver({type:'end'})})
 for(const reviewer of [false,true])await act(()=>turn({prompt:'Write the cases.',hostTools:[],reviewer,conversation:!reviewer},new AbortController().signal,()=>{}))
 for(const context of contexts)expect(context.connection).toEqual({connectionId:'ai-111111111111111111111111',connectionRevision:'a'.repeat(64),connectionName:'Open weights'})
 expect(contexts).toHaveLength(2)
 // A slot with no connection of its own binds none.
 host.slot={};hook.rerender()
 await act(()=>turn({prompt:'Write the cases.',hostTools:[],reviewer:false,conversation:true},new AbortController().signal,()=>{}))
 expect(contexts[2]).not.toHaveProperty('connection')
 hook.unmount()
})
