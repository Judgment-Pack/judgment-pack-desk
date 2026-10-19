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
 for(const path of ['assistant/useAssistantRun.ts','research/useResearchRun.ts'])expect(readFileSync(resolve(root,path),'utf8')).toContain('configuration:settings.current.contextConfiguration')
 for(const file of ['input_research_model.go','portable_research_worker.go'])expect(readFileSync(resolve(root,'../../internal/desk',file),'utf8')).toContain('s.runBackgroundModel(')
 const portable=readFileSync(resolve(root,'../../internal/desk/portable_research_worker.go'),'utf8')
 for(const boundary of ['fitResearchContextWithInstructions(', 'researchContextRead(', 'researchAttempt(', 'portableResearchChatWork(', 'pauseResearchForBudget('])expect(portable).toContain(boundary)
 const backend=resolve(root,'../../internal/desk')
 const runners=readdirSync(backend).filter(p=>p.endsWith('.go')&&!p.endsWith('_test.go')&&/runner\.Run\(/.test(readFileSync(resolve(backend,p),'utf8'))).sort()
 expect(runners).toEqual(['agent_run.go','background_model.go'])
 for(const file of runners){const text=readFileSync(resolve(backend,file),'utf8');expect(text.indexOf('prepareAgentContext(')).toBeLessThan(text.indexOf('runner.Run('))}
 for(const file of ['background_model.go','modelrelay.go'])expect(readFileSync(resolve(backend,file),'utf8')).toContain('prepareModelContext(')
})

it('keeps future conversational progress and request accounting in the shared execution boundary',()=>{
 const context=harness('context/session.js')
 expect(context).toContain('ExecutionController')
 expect(context).toContain('context.execution.claim()')
 expect(context).toContain('session.context?.checkpoint?.(')
 const run=readFileSync(resolve(root,'research/useResearchRun.ts'),'utf8')
 expect(run).toContain('checkpoint:request.conversation?async checkpoint=>')
 const managed=harness('context/managed.js')
 expect(managed).toContain('context.execution.observe(event)')
 expect(managed).toMatch(/type:\s*'budget_paused'/)
})

it('keeps future AI failures behind the shared contract and recovery UI',()=>{
 for(const file of ['engines/codex/index.js','engines/vercel/loop.js','context/managed.js'])expect(harness(file)).toContain('failureEvent(')
 expect(readFileSync(resolve(root,'assistant/useAssistantRun.ts'),'utf8')).toContain('failureEvent(')
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
 for(const file of ['research/run.ts','dialects/chatTools.ts','graphs/chatTools.ts','policies/chatTools.ts'])expect(readFileSync(resolve(root,file),'utf8')).toContain('contextPolicy:')
})

it('routes artifact correction and scoped automatic saving through shared host boundaries',()=>{
 for(const file of ['research/run.ts','assistant/proposalWorkflow.ts'])expect(readFileSync(resolve(root,file),'utf8')).toContain('correctArtifact(')
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
