// Architecture tripwires: new AI features must enter the same boundaries.
//
// The harness (engines, context, execution, failures) is the package
// `@judgment-pack/harness` now, so its half of each tripwire reads the modules
// the Desk installs and bundles, the package's `dist/`, rather than source
// paths under `assistant/`. Emitted JavaScript is printed by the compiler, so a
// needle about spacing is a pattern rather than a string; a needle the Desk
// writes is read as it always was.
import {readFileSync,readdirSync} from 'node:fs'
import {resolve,relative} from 'node:path'
import {expect,it} from 'vitest'
import {parse} from '@babel/parser'
const root=resolve(import.meta.dirname,'../..')
const harnessRoot=resolve(root,'../node_modules/@judgment-pack/harness/dist')
const harness=(path:string)=>readFileSync(resolve(harnessRoot,path),'utf8')
const backend=resolve(root,'../../internal/desk')
const goSource=(path:string)=>readFileSync(resolve(backend,path),'utf8')
const browser=(path:string)=>readFileSync(resolve(root,path),'utf8')
function files(dir:string,pattern=/\.tsx?$/):string[]{return readdirSync(dir,{withFileTypes:true}).flatMap(e=>e.isDirectory()?files(resolve(dir,e.name),pattern):pattern.test(e.name)&&!e.name.includes('.test.')?[resolve(dir,e.name)]:[])}
function sdkImports(base:string,paths:string[]){
 const imports:string[]=[]
 for(const path of paths){
  const file=parse(readFileSync(path,'utf8'),{sourceType:'module',plugins:['typescript','jsx']})
  file.program.body.forEach(node=>{
   if(node.type!=='ImportDeclaration'||node.importKind==='type')return
   const module=node.source.value
   if(module==='ai'||module.startsWith('@ai-sdk/'))imports.push(relative(base,path))
  })
 }
 return [...new Set(imports)].sort()
}
it('confines model SDK imports to the managed adapters (type imports excluded)',()=>{
 expect(sdkImports(root,files(root))).toEqual([])
 expect(sdkImports(harnessRoot,files(harnessRoot,/\.js$/).filter(path=>!path.includes('/certification/')))).toEqual(['engines/vercel/loop.js'])
})
it('keeps both frontend and backend execution paths inside the context boundary',()=>{
 for(const path of ['engines/codex/index.js','engines/vercel/loop.js']){
  const source=harness(path);expect(source).toContain('managedContext(');expect(source).toContain('context.result(');expect(source).toContain('context.beginCriticPhase()')
 }
 // Saved browser surfaces observe runs; the canonical owner reads the retained profile.
 for(const path of ['assistant_execution.go','assistant_authoring_execution.go']){
  const source=goSource(path)
  for(const boundary of ['Profile.Context', 'input.Configuration, err = runcontext.ReadConfiguration(value)'])expect(source).toContain(boundary)
 }
 for(const boundary of ['savedChatRun(store,chat)', 'useSyncExternalStore(run.subscribe,run.getSnapshot,run.getSnapshot)', 'void run.reconnect()'])expect(browser('assistant/serverRuns/useSavedChatRun.ts')).toContain(boundary)
 for(const file of ['input_research_model.go','portable_research_worker.go'])expect(readFileSync(resolve(root,'../../internal/desk',file),'utf8')).toContain('s.runBackgroundModel(')
 const portable=readFileSync(resolve(root,'../../internal/desk/portable_research_worker.go'),'utf8')
 for(const boundary of ['fitResearchContextWithInstructions(', 'researchContextRead(', 'researchAttempt(', 'portableResearchChatWork(', 'pauseResearchForBudget('])expect(portable).toContain(boundary)
 const backend=resolve(root,'../../internal/desk')
 const runners=readdirSync(backend).filter(p=>p.endsWith('.go')&&!p.endsWith('_test.go')&&/runner\.Run\(/.test(readFileSync(resolve(backend,p),'utf8'))).sort()
 expect(runners).toEqual(['agent_run.go','assistant_execution_agent.go','background_execution.go'])
 for(const file of ['agent_run.go']){const text=readFileSync(resolve(backend,file),'utf8');expect(text).toContain('checkAgentWireContext(');expect(text.indexOf('checkAgentWireContext(')).toBeLessThan(text.indexOf('runner.Run('))}
 // Background dispatch uses the canonical Host/context and retains separate
 // source custody and final byte guards in its reserved workflow owner.
 const background=readFileSync(resolve(backend,'background_execution.go'),'utf8')
 for(const boundary of ['s.assistantPromptHost(ctx)', 'runcontext.Input{', 'CurrentRequest:', 'Context: input, Host: host', 'Authorize: b.authorize', 'policy.Check(string(request.Body))', 'policyGuard.Check(bridge.Prompt, bridge.Instructions, string(toolJSON))', 'bridge.BeforeGeneration', 'b.dispatch(ctx)'])expect(background).toContain(boundary)
 // The private Go factory uses the published Agent/context Host boundary;
 // legacy preparation here would assemble instructions a second time.
 const factory=readFileSync(resolve(backend,'assistant_execution.go'),'utf8')
 for(const boundary of ['runcontext.Input{','CurrentRequest:','Host: host','assistantCanonicalAgent(agentadapter.New())','delegate.With(base,'])expect(factory).toContain(boundary)
 const agent=readFileSync(resolve(backend,'assistant_execution_agent.go'),'utf8')
 for(const boundary of ['e.Authorize(', 'enabled(ctx, request.Model)', 'bridge.BeforeGeneration', 'return dispatch.dispatch(ctx)'])expect(agent).toContain(boundary)
 for(const file of ['modelrelay.go'])expect(readFileSync(resolve(backend,file),'utf8')).toContain('checkModelWireContext(')
 const legacyContext=readFileSync(resolve(backend,'ai_context.go'),'utf8')
 for(const forbidden of ['json.NewDecoder(', 'memoryRequestQuery(', 'agentRecallQuery(', 'request.Instructions +='])expect(legacyContext).not.toContain(forbidden)
 const guard=readFileSync(resolve(backend,'ai_wire_guard.go'),'utf8')
 for(const forbidden of ['memoryBackground(', 's.aiContext(', 'request.Instructions +=', 'body["messages"]', 'body["contents"]'])expect(guard).not.toContain(forbidden)
 const brief=readFileSync(resolve(backend,'assistant_brief_execution.go'),'utf8')
 for(const boundary of ['runcontext.ContextualSession(', 'Purpose: "brief"', 'e.assistantAgent()', 'e.assistantModel()', 'e.holdAssistantDocuments(', 'e.service.server.recheckAssistantDocument('])expect(brief).toContain(boundary)
 for(const forbidden of ['e.assistantRuntime(', 'delegate.With(', 'Children:', 'runBackgroundModel('])expect(brief).not.toContain(forbidden)
 const pane=readFileSync(resolve(root,'briefs/BriefPane.tsx'),'utf8')
 for(const boundary of ['serverRunClient(', 'client.briefReference(', 'client.commitBrief(', '<ExecutionProgress', '<ResponseRecovery'])expect(pane).toContain(boundary)
 expect(pane).not.toContain('useAssistantRun(')
 const diagnostic=readFileSync(resolve(backend,'assistant_diagnostic_execution.go'),'utf8')
 for(const boundary of ['runcontext.ContextualSession(', 'Agent: &runcontext.AgentSession{', 'ArtifactProposals:', 'e.assistantAgent()', 'assistantCanonicalAgent(agentadapter.New())', 'e.assistantRuntime(', 'assistantDiagnosticSucceeded('])expect(diagnostic).toContain(boundary)
 // Native diagnostics keep provider-managed accounting and the admitted custody.
 for(const boundary of ['Memory: &off', 'CurrentRequest:', 'e.Authorize(ctx)', 'Granted: []string{"get_schema"}', 'len(offer.Tools) != 1', 'input.Authorize, input.Checkpoint, input.Execution = e.Authorize, e.Checkpoint, e.record.Checkpoint', 'e.Checkpoint(ctx, event.ExecutionCheckpoint.Checkpoint)', 'e.writer.boundEvent(persist, event,', 'case event.BudgetPaused != nil:', 'eventErr = errAssistantRunScope'])expect(diagnostic).toContain(boundary)
 for(const forbidden of ['Execution.LimitTo(', 'RequestCap:', 'delegate.With(', 'runBackgroundModel(', 'e.assistantModel(', 'HostTools:'])expect(diagnostic).not.toContain(forbidden)
 const settings=readFileSync(resolve(root,'assistant/AssistantSettings.tsx'),'utf8')
 expect(settings).toContain('useConnectionDiagnostic(');expect(settings).not.toContain('useAssistantRun(')

})

it('keeps future conversational progress and request accounting in the shared execution boundary',()=>{
 const context=harness('context/session.js')
 expect(context).toContain('ExecutionController')
 expect(context).toContain('context.execution.claim()')
 expect(context).toContain('session.context?.checkpoint?.(')
 const owner=goSource('assistant_execution.go')
 for(const boundary of ['cp.ID != e.record.Checkpoint.ID || cp.RequestKey != e.record.Binding.ExecutionKey', 'return e.Checkpoint(ctx, cp)', 'saveCheckpoint(ctx, event.ExecutionCheckpoint.Checkpoint)', 'saveCheckpoint(ctx, event.BudgetPaused.Checkpoint)', 'Authorize: e.Authorize, Checkpoint: saveCheckpoint, Execution: e.record.Checkpoint'])expect(owner).toContain(boundary)
 const writer=goSource('assistant_run_store_writer.go')
 for(const boundary of ['readAssistantRunCheckpoint(state, run.Binding)', 'cp.Used < run.Checkpoint.Used', 'run.Checkpoint = cp'])expect(writer).toContain(boundary)
 for(const boundary of ['client.events(observations,this.scope!.signal)', 'this.set(this.present(observation.snapshot'])expect(browser('assistant/serverRuns/controller.ts')).toContain(boundary)
 expect(browser('assistant/serverRuns/presentation.ts')).toContain('execution:snapshot.checkpoint')
 const managed=harness('context/managed.js')
 expect(managed).toContain('context.execution.observe(event)')
 expect(managed).toMatch(/type:\s*'budget_paused'/)
})

it('keeps future AI failures behind the shared contract and recovery UI',()=>{
 for(const file of ['engines/codex/index.js','engines/vercel/loop.js','context/managed.js'])expect(harness(file)).toContain('failureEvent(')
 // Transport errors normalize at the canonical adapter; the browser reads typed events.
 for(const boundary of ['failure.Normalize(err, "agent", time.Time{})', 'failure.Authored(code, detail, "agent").Failure'])expect(goSource('assistant_execution_agent.go')).toContain(boundary)
 expect(browser('assistant/serverRuns/controller.ts')).toContain("normalizeFailure(cause,'desk')")
 expect(browser('assistant/serverRuns/presentation.ts')).toContain('failure=readFailure(e.failure)')
 expect(goSource('assistant_run_store_writer.go')).toContain('appendAssistantRunEvent(run, nextAssistantRunIdentity(run, "event"), event)')
 // The page reads the package's copy of the Go-side catalogue, so the copy it bundles is the Desk's catalogue.
 expect(harness('failures.js')).toContain("from './contract/catalog.json'")
 expect(JSON.parse(harness('contract/catalog.json'))).toEqual(JSON.parse(readFileSync(resolve(root,'../../internal/aifailure/catalog.json'),'utf8')))
 expect(readFileSync(resolve(root,'chat/checkpoint.ts'),'utf8')).toContain('readFailure(')
 expect(readFileSync(resolve(root,'chat/ChatPanel.tsx'),'utf8')).toContain('<ResponseRecovery')
 const backend=resolve(root,'../../internal/desk')
 for(const file of ['background_model.go','agent_run.go'])expect(readFileSync(resolve(backend,file),'utf8')).toContain('aifailure.')
 expect(readFileSync(resolve(backend,'input_research_retry.go'),'utf8')).toContain('*aifailure.Failure')
 const sdk=harness('engines/vercel/loop.js')
 expect(sdk).not.toContain('function describe(')
 expect(sdk).toContain('maxRetries: 0')
})

it('keeps working-context retention independent of chat topics and feature tool names',()=>{
 const projection=harness('context/session.js')
 const controller=harness('execution/controller.js')
 expect(projection).toContain('contextPolicy?.reference')
 expect(projection).not.toMatch(/p\.toolName\s*===\s*'get_authoring_instructions'/)
 expect(controller).toMatch(/policy\.immutable\s*===\s*true\s*&&\s*readOnly/)
 expect(controller).not.toMatch(/\[\s*'get_authoring_instructions',\s*'get_schema',\s*'read_context'\s*\]/)
 for(const file of ['dialects/chatTools.ts','graphs/chatTools.ts','policies/chatTools.ts'])expect(browser(file)).toContain('contextPolicy:')
 // Retention is declared on current canonical host offers, including immutable read-only contracts.
 for(const file of ['assistant_dialect_action_registry.go','assistant_graph_tools.go','assistant_policy_tools.go'])expect(goSource(file)).toContain(file==='assistant_graph_tools.go'?'ContextPolicy = &runcontext.ToolContextPolicy{Reference: true}':'ContextPolicy: &runcontext.ToolContextPolicy{Reference: true}')
 expect(goSource('assistant_dialect_recipe_tools_store.go')).toContain('tool.ContextPolicy = &runcontext.ToolContextPolicy{Immutable: true, Reference: true}')
})

it('routes artifact correction and scoped automatic saving through shared host boundaries',()=>{
 // Saved creation corrections now belong to the canonical durable workflow.
 const creation=goSource('assistant_creation_execution.go')
 for(const boundary of ['receipt.Proposal = &proposal', 'e.retainAssistantCreation(ctx, input, receipt)', 'run.Creation.Proposal.Token != input.Action.Token', 'run.Historical', '!run.ChatProjection.Acknowledged', 'proposal.Scope != input.Scope', 'approveAssistantCreationCorrection(ctx, input.Scope'])expect(creation).toContain(boundary)
 const adapter=goSource('assistant_creation_adapter.go')
 for(const boundary of ['e.executeAssistantCreation(ctx, e.reviewAssistantCreation)', 'if handled || err != nil', 'execution.Adapter.Run(ctx, execution.Run)', 'run.Checkpoint.Status != "complete"', 'if proposal != nil', 'cp.Status = "running"', 'e.Checkpoint(ctx, cp)'])expect(adapter).toContain(boundary)
 const correction=goSource('assistant_creation_workflow.go')
 for(const boundary of ['issue.Proposal == nil || fresh(ctx) != nil', 'assistantCreationCorrectedCase(scope, issue, *issue.Proposal, token, cases)', 'validateAssistantCreationExpectations(ctx, []assistantCreationCase{replacement}, call)', 'check, err := retest(ctx, staged)', 'assistantCreationSuccessfulCheck(check, scope.CandidateDigest, staged)', 'return commit(ctx, staged, check)'])expect(correction).toContain(boundary)
 expect(correction.indexOf('check, err := retest(ctx, staged)')).toBeLessThan(correction.indexOf('return commit(ctx, staged, check)'))
 expect(harness('execution/checkArtifact.js')).toContain('correctArtifact(')
 expect(readFileSync(resolve(root,'dialects/dialectChatTools.ts'),'utf8')).toContain('checkArtifact(')
 const core=harness('execution/correctArtifact.js')
 expect(core).not.toMatch(/model\.call|writeFile|MAX_TURNS|extendExecution/)
 for(const file of ['chat/usePackAutoSave.ts','chat/useDialectPublicationAutoSave.ts'])expect(readFileSync(resolve(root,file),'utf8')).toContain('useAutomaticAction(')
 expect(readFileSync(resolve(root,'dialects/savePublication.ts'),'utf8')).toContain('actionApprovalAPI<')
 expect(readFileSync(resolve(root,'chat/ChatProvider.tsx'),'utf8')).toContain('useDialectPublicationAutoSave(')
 const server=readFileSync(resolve(root,'../../internal/desk/assistant_approval.go'),'utf8')
 expect(server).toContain('assistantActionAdapters')
 expect(server).toContain('s.commitWriteLocked(')
})
