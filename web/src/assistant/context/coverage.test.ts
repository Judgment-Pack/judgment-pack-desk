// Architecture tripwires: new AI features must enter the same boundaries.
import {readFileSync,readdirSync} from 'node:fs'
import {resolve,relative} from 'node:path'
import {expect,it} from 'vitest'
import {parse} from '@babel/parser'
const root=resolve(import.meta.dirname,'../..')
function files(dir:string):string[]{return readdirSync(dir,{withFileTypes:true}).flatMap(e=>e.isDirectory()?files(resolve(dir,e.name)):/\.tsx?$/.test(e.name)&&!e.name.includes('.test.')?[resolve(dir,e.name)]:[])}
it('confines model SDK imports to the managed adapters (type imports excluded)',()=>{
 const imports:string[]=[]
 for(const path of files(root)){
  if(path.includes('/conformance/'))continue
  const file=parse(readFileSync(path,'utf8'),{sourceType:'module',plugins:['typescript','jsx']})
  file.program.body.forEach(node=>{
   if(node.type!=='ImportDeclaration'||node.importKind==='type')return
   const module=node.source.value
   if(module==='ai'||module.startsWith('@ai-sdk/'))imports.push(relative(root,path))
  })
 }
 expect([...new Set(imports)].sort()).toEqual(['assistant/engines/vercel/loop.ts'])
})
it('keeps both frontend and backend execution paths inside the context boundary',()=>{
 for(const path of ['assistant/engines/codex/index.ts','assistant/engines/vercel/loop.ts']){
  const source=readFileSync(resolve(root,path),'utf8');expect(source).toContain('managedContext(');expect(source).toContain('context.result(');expect(source).toContain('context.beginIsolatedPhase()')
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
 const context=readFileSync(resolve(root,'assistant/context/session.ts'),'utf8')
 expect(context).toContain('ExecutionController')
 expect(context).toContain('context.execution.claim()')
 expect(context).toContain('session.context?.checkpoint?.(')
 const run=readFileSync(resolve(root,'research/useResearchRun.ts'),'utf8')
 expect(run).toContain('checkpoint:request.conversation?async checkpoint=>')
 const managed=readFileSync(resolve(root,'assistant/context/managed.ts'),'utf8')
 expect(managed).toContain('context.execution.observe(event)')
 expect(managed).toContain("type:'budget_paused'")
})

it('keeps future AI failures behind the shared contract and recovery UI',()=>{
 for(const file of ['assistant/engines/codex/index.ts','assistant/engines/vercel/loop.ts','assistant/context/managed.ts','assistant/useAssistantRun.ts'])expect(readFileSync(resolve(root,file),'utf8')).toContain('failureEvent(')
 expect(readFileSync(resolve(root,'assistant/failures.ts'),'utf8')).toContain('internal/aifailure/catalog.json')
 expect(readFileSync(resolve(root,'chat/checkpoint.ts'),'utf8')).toContain('readFailure(')
 expect(readFileSync(resolve(root,'chat/ChatPanel.tsx'),'utf8')).toContain('<ResponseRecovery')
 const backend=resolve(root,'../../internal/desk')
 for(const file of ['background_model.go','agent_run.go'])expect(readFileSync(resolve(backend,file),'utf8')).toContain('aifailure.')
 expect(readFileSync(resolve(backend,'input_research_retry.go'),'utf8')).toContain('*aifailure.Failure')
 const sdk=readFileSync(resolve(root,'assistant/engines/vercel/loop.ts'),'utf8')
 expect(sdk).not.toContain('function describe(')
 expect(sdk).toContain('maxRetries: 0')
})

it('keeps working-context retention independent of chat topics and feature tool names',()=>{
 const projection=readFileSync(resolve(root,'assistant/context/session.ts'),'utf8')
 const controller=readFileSync(resolve(root,'assistant/execution/controller.ts'),'utf8')
 expect(projection).toContain('contextPolicy?.reference')
 expect(projection).not.toContain("p.toolName==='get_authoring_instructions'")
 expect(controller).toContain('policy.immutable===true&&readOnly')
 expect(controller).not.toContain("['get_authoring_instructions','get_schema','read_context']")
 for(const file of ['research/run.ts','dialects/chatTools.ts','graphs/chatTools.ts','policies/chatTools.ts'])expect(readFileSync(resolve(root,file),'utf8')).toContain('contextPolicy:')
})

it('routes artifact correction and scoped automatic saving through shared host boundaries',()=>{
 for(const file of ['research/run.ts','assistant/proposalWorkflow.ts','assistant/execution/checkArtifact.ts'])expect(readFileSync(resolve(root,file),'utf8')).toContain('correctArtifact(')
 expect(readFileSync(resolve(root,'dialects/dialectChatTools.ts'),'utf8')).toContain('checkArtifact(')
 const core=readFileSync(resolve(root,'assistant/execution/correctArtifact.ts'),'utf8')
 expect(core).not.toMatch(/model\.call|writeFile|MAX_TURNS|extendExecution/)
 for(const file of ['chat/usePackAutoSave.ts','chat/useDialectPublicationAutoSave.ts'])expect(readFileSync(resolve(root,file),'utf8')).toContain('useAutomaticAction(')
 expect(readFileSync(resolve(root,'dialects/savePublication.ts'),'utf8')).toContain('actionApprovalAPI<')
 expect(readFileSync(resolve(root,'chat/ChatProvider.tsx'),'utf8')).toContain('useDialectPublicationAutoSave(')
 const server=readFileSync(resolve(root,'../../internal/desk/assistant_approval.go'),'utf8')
 expect(server).toContain('assistantActionAdapters')
 expect(server).toContain('s.commitWriteLocked(')
})
