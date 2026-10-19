import { afterEach, expect, it, vi } from 'vitest'
import { waitFor } from '@testing-library/react'
import { readUserQuestion, readSavedQuestions, validQuestionDraft } from '@judgment-pack/harness/questions'
import { interviewQuestion } from '../testing/question'
import { AuthoringRun, type RunPorts } from '../research/run'
import { Ledger } from '../research/ledger'
import { checkpoint, decodeCheckpoint } from './checkpoint'
import { artifactCheckpoint } from '../packs/drafts/model'
import { ChatStore, type ChatPersistence } from './store'
import { memoryDraftPersistence } from '../testing/draftPersistence'
import { submitQuestion, continueQuestion } from './questionActions'

const runs:AuthoringRun[]=[],stores:ChatStore[]=[]
afterEach(()=>{runs.forEach(r=>r.stop());stores.forEach(s=>s.dispose());vi.restoreAllMocks()})
function fixture(turn?:RunPorts['turn']){
 const call=vi.fn(),model=vi.fn(turn??(async(_r,_s,emit)=>{emit({type:'user_question',id:'question-1',question:interviewQuestion});emit({type:'end'})}))
 const run=new AuthoringRun({mode:'draft',turn:model,callTool:call,ledger:new Ledger('fixture'),researchTools:[],seal:async()=>{},registry:async()=>'',gateway:null,newSession:()=>crypto.randomUUID(),authorPrompt:'',maxRevisions:4,seconds:60,log:()=>{}})
 runs.push(run);return {run,model,call}
}
async function pending(){const h=fixture();h.run.start('Interview me, then propose a recipe. Do not start research.',[]);await waitFor(()=>expect(h.run.getSnapshot().status).toBe('needs-input'));return h}
it('refuses malformed recommendations, duplicate choices and out-of-range answers',()=>{
 expect(()=>readUserQuestion({...interviewQuestion,recommendedOptionId:'other'})).toThrow()
 expect(()=>readUserQuestion({...interviewQuestion,recommendation:null})).toThrow()
 expect(()=>readUserQuestion({...interviewQuestion,options:[interviewQuestion.options[0],interviewQuestion.options[0]]})).toThrow()
 expect(validQuestionDraft(interviewQuestion,{selected:['operations','risk'],text:''})).toBe(false)
 expect(validQuestionDraft(interviewQuestion,{selected:['unknown'],text:''})).toBe(false)
 expect(validQuestionDraft(interviewQuestion,{selected:[],text:'Custom buyer'})).toBe(true)
})
it('retains a pending question and its draft on reload, without model calls or runtime work',async()=>{
 const h=await pending();expect(h.call).not.toHaveBeenCalled()
 h.run.updateQuestionDraft('question-1',{selected:['risk'],text:'Focus on insurance.'})
 const saved=decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(h.run.getSnapshot(),[]))))
 const restored=fixture();await restored.run.restore(saved.state)
 expect(restored.model).not.toHaveBeenCalled();expect(restored.run.running).toBe(false)
 expect(restored.run.getSnapshot()).toMatchObject({status:'needs-input',detail:'Waiting for your answer',questions:[{draft:{selected:['risk'],text:'Focus on insurance.'}}]})
 expect(artifactCheckpoint(saved).state.questions).toEqual([])
 expect(()=>decodeCheckpoint({...saved,state:{...saved.state,turns:[]}})).toThrow()
 expect(()=>readSavedQuestions([...saved.state.questions!,...saved.state.questions!])).toThrow()
})
it('records one answer, preserves the original prompt and continues explicitly without replaying work',async()=>{
 const h=await pending();h.run.updateQuestionDraft('question-1',{selected:['operations'],text:'Regional teams'})
 expect(h.run.prepareQuestionAnswer('question-1','answered')).toBe(true)
 expect(h.run.prepareQuestionAnswer('question-1','answered')).toBe(false)
 expect(h.model).toHaveBeenCalledTimes(1)
 const saved=decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(h.run.getSnapshot(),[]))))
 const next=fixture(async(request,_signal,emit)=>{expect(request.prompt).toContain('Do not start research');expect(request.prompt).toContain('Head of operations');expect(request.prompt).toContain('Regional teams');emit({type:'message',text:'Next, let’s define the problem.'});emit({type:'end'})})
 await next.run.restore(saved.state)
 expect(next.model).not.toHaveBeenCalled()
 expect(next.run.continueQuestion('question-1')).toBe(true)
 expect(next.run.continueQuestion('question-1')).toBe(false)
 await waitFor(()=>expect(next.run.getSnapshot().status).toBe('complete'))
 expect(next.run.getSnapshot().turns.filter(t=>t.role==='user')).toHaveLength(2)
 expect(next.call).not.toHaveBeenCalled()
})
it('help does not accept the recommended or selected choice, and typing in chat supersedes the old question',async()=>{
 const h=await pending();h.run.updateQuestionDraft('question-1',{selected:['operations'],text:'Which owns the budget?'})
 h.model.mockImplementation(async(request,_s,emit)=>{expect(request.prompt).toContain('Risk or compliance lead');expect(request.prompt).toContain('Owns systems and integrations.');emit({type:'message',text:'Here are the tradeoffs.'})})
 h.run.prepareQuestionAnswer('question-1','help')
 const reply=h.run.getSnapshot().turns.at(-1)!
 expect(reply.text).toContain('Help me decide');expect(reply.text).not.toContain('Head of operations');expect(reply.input).toContain('"selected":[]')
 h.run.continueQuestion('question-1');await waitFor(()=>expect(h.model).toHaveBeenCalledTimes(2))
 const second=await pending();second.model.mockImplementation(async(_r,_s,emit)=>{emit({type:'message',text:'Direction changed.'})})
 second.run.send('Change direction; focus on claims managers.')
 await waitFor(()=>expect(second.run.getSnapshot().status).toBe('complete'))
 expect(second.run.getSnapshot().questions?.[0]?.resolution?.kind).toBe('superseded')
 expect(second.run.prepareQuestionAnswer('question-1','answered')).toBe(false)
})
it('does not continue when the answer checkpoint fails to save; retry saves it once before continuing',async()=>{
 const h=await pending();h.run.updateQuestionDraft('question-1',{selected:[],text:'Claims manager'})
 h.model.mockImplementation(async(_r,_s,emit)=>{emit({type:'message',text:'Ready for the next question.'})})
 let fail=true
 const write=vi.fn<ChatPersistence['write']>(async document=>{if(fail)throw Error('Revision conflict');return {project:'/test',sha256:'saved',content:document}})
 const store=new ChatStore('/test',{read:async()=>({project:'/test',sha256:'absent',content:{version:1,chats:[]}}),write},memoryDraftPersistence('/test'));stores.push(store)
 await store.load();const chat=store.create()
 const binding={run:h.run,state:h.run.getSnapshot(),sources:[],ledger:null,blocked:'',model:'fixture',researchConfigured:false}
 store.report(chat.id,binding)
 await expect(submitQuestion(store,chat.id,'question-1','answered')).rejects.toThrow('Revision conflict')
 expect(h.model).toHaveBeenCalledTimes(1)
 expect(h.run.getSnapshot().turns.at(-1)?.text).toBe('Claims manager')
 fail=false;store.retrySave();await waitFor(()=>expect(store.getSnapshot().saving).toBe(false))
 await continueQuestion(store,chat.id,'question-1')
 await waitFor(()=>expect(h.model).toHaveBeenCalledTimes(2))
 expect(h.run.getSnapshot().turns.filter(t=>t.role==='user')).toHaveLength(2)
})

it('re-shows choices after prose-only help, retaining explanation and reloading without another request', async () => {
 const h=await pending()
 h.run.updateQuestionDraft('question-1',{selected:['risk'],text:'Who owns the budget?'})
 h.model.mockImplementation(async(_r,_s,emit)=>{emit({type:'message',text:'Operations owns the workflow; risk owns oversight.'});emit({type:'end'})})
 h.run.prepareQuestionAnswer('question-1','help');h.run.continueQuestion('question-1')
 await waitFor(()=>expect(h.run.getSnapshot().questions).toHaveLength(2))
 const state=h.run.getSnapshot(),question=state.questions!.at(-1)!
 expect(state.status).toBe('needs-input')
 expect(state.turns.at(-2)?.text).toBe('Operations owns the workflow; risk owns oversight.')
 expect(question.prompt).toEqual(interviewQuestion)
 expect(question.draft).toEqual({selected:[],text:''})
 expect(h.model).toHaveBeenCalledTimes(2)
 const restored=fixture();await restored.run.restore(decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint(state,[])))).state)
 expect(restored.run.getSnapshot().questions!.at(-1)).toEqual(question)
 expect(restored.model).not.toHaveBeenCalled()
 h.run.updateQuestionDraft(question.id,{selected:['operations'],text:''})
 h.run.prepareQuestionAnswer(question.id,'answered');h.run.continueQuestion(question.id)
 await waitFor(()=>expect(h.run.getSnapshot().status).toBe('complete'))
 expect(h.run.getSnapshot().questions).toHaveLength(2)
 expect(h.model).toHaveBeenCalledTimes(3)
})
it('uses a refined question after help once and retains the streamed public explanation', async () => {
 const h=await pending()
 const next={...interviewQuestion,title:'Responsibility',question:'Which responsibility matters most for the pilot?'}
 h.model.mockImplementation(async(_r,_s,emit)=>{emit({type:'message_progress',text:'First choose which responsibility the pilot serves.'});emit({type:'user_question',id:'question-2',question:next});emit({type:'end'})})
 h.run.prepareQuestionAnswer('question-1','help');h.run.continueQuestion('question-1')
 await waitFor(()=>expect(h.run.getSnapshot().status).toBe('needs-input'))
 expect(h.run.getSnapshot().questions).toHaveLength(2)
 expect(h.run.getSnapshot().questions!.at(-1)?.prompt).toEqual(next)
 expect(h.run.getSnapshot().turns.at(-2)?.text).toBe('First choose which responsibility the pilot serves.')
 expect(h.model).toHaveBeenCalledTimes(2)
})
it('does not hide a failed help request behind another question', async () => {
 const h=await pending()
 h.model.mockImplementation(async(_r,_s,emit)=>{emit({type:'message',text:'A partial explanation.'});emit({type:'error',message:'Connection failed'});emit({type:'end'})})
 h.run.prepareQuestionAnswer('question-1','help');h.run.continueQuestion('question-1')
 await waitFor(()=>expect(h.run.getSnapshot().status).toBe('failed'))
 expect(h.run.getSnapshot().questions).toHaveLength(1)
 expect(h.run.canRetryResponse).toBe(true)
})

it('normalizes only absent optional model fields and keeps saved history strict',async()=>{
 const {readModelQuestion}=await import('@judgment-pack/harness/questions')
 const loose={...interviewQuestion,recommendedOptionId:'',recommendation:'',options:interviewQuestion.options.map(({id,label})=>({id,label}))}
 const normalized=readModelQuestion(loose)
 expect(normalized.options.map(o=>o.label)).toEqual(interviewQuestion.options.map(o=>o.label))
 expect(normalized.options.every(o=>o.description==='')).toBe(true)
 expect(normalized.recommendedOptionId).toBeNull();expect(normalized.recommendation).toBeNull()
 expect(()=>readUserQuestion(loose)).toThrow()
 expect(loose.options[0]).not.toHaveProperty('description')
 expect(()=>readModelQuestion({...loose,recommendedOptionId:'operations'})).toThrow(/recommendation needs/)
 expect(()=>readModelQuestion({...interviewQuestion,title:'x'.repeat(41)})).toThrow(/40 characters/)
 expect(()=>readModelQuestion({...interviewQuestion,options:[interviewQuestion.options[0],interviewQuestion.options[0]]})).toThrow(/distinct choices/)
})

it('Continue retries a failed answer save itself and still blocks on a persistent conflict',async()=>{
 const h=await pending();h.run.updateQuestionDraft('question-1',{selected:['operations'],text:'Regional teams'})
 h.model.mockImplementation(async(_r,_s,emit)=>{emit({type:'message',text:'Answer received.'})})
 let fail=true
 const write=vi.fn<ChatPersistence['write']>(async document=>{if(fail)throw Error('Revision conflict');return {project:'/test',sha256:'saved',content:document}})
 const store=new ChatStore('/test',{read:async()=>({project:'/test',sha256:'absent',content:{version:1,chats:[]}}),write},memoryDraftPersistence('/test'));stores.push(store)
 await store.load();const chat=store.create()
 store.report(chat.id,{run:h.run,state:h.run.getSnapshot(),sources:[],ledger:null,blocked:'',model:'fixture',researchConfigured:false})
 await expect(submitQuestion(store,chat.id,'question-1','answered')).rejects.toThrow('Revision conflict')
 await expect(continueQuestion(store,chat.id,'question-1')).rejects.toThrow('Revision conflict')
 expect(h.model).toHaveBeenCalledTimes(1)
 fail=false
 await continueQuestion(store,chat.id,'question-1')
 await waitFor(()=>expect(h.model).toHaveBeenCalledTimes(2))
 expect(h.run.getSnapshot().turns.filter(t=>t.role==='user')).toHaveLength(2)
 expect(write.mock.calls.every(call=>call[1]==='absent')).toBe(true)
})
