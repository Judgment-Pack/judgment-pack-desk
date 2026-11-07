import { afterEach, expect, it, vi } from 'vitest'
import { readUserQuestion, validQuestionDraft } from '@judgment-pack/harness/questions'
import { interviewQuestion } from '../testing/question'
import { ChatStore, type ChatPersistence } from './store'
import { memoryDraftPersistence } from '../testing/draftPersistence'
import { submitQuestion, continueQuestion } from './questionActions'
import { SavedChatRun } from '../assistant/serverRuns/controller'
import { serverRunClient } from '../assistant/serverRuns/client'
import { giveThisPageASessionForTesting } from '../mcp/session'
import answer from '../assistant/serverRuns/fixtures/continuation-answer.json'
const stores:ChatStore[]=[]
afterEach(()=>{stores.forEach(s=>s.dispose());stores.length=0;vi.restoreAllMocks();vi.unstubAllGlobals();sessionStorage.clear()})
it('refuses malformed recommendations, duplicate choices and out-of-range answers',()=>{
 expect(()=>readUserQuestion({...interviewQuestion,recommendedOptionId:'other'})).toThrow()
 expect(()=>readUserQuestion({...interviewQuestion,recommendation:null})).toThrow()
 expect(()=>readUserQuestion({...interviewQuestion,options:[interviewQuestion.options[0],interviewQuestion.options[0]]})).toThrow()
 expect(validQuestionDraft(interviewQuestion,{selected:['operations','risk'],text:''})).toBe(false)
 expect(validQuestionDraft(interviewQuestion,{selected:['unknown'],text:''})).toBe(false)
 expect(validQuestionDraft(interviewQuestion,{selected:[],text:'Custom buyer'})).toBe(true)
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

it('holds a server-owned answer behind the original save revision and continues exactly once after an explicit save retry',async()=>{
 giveThisPageASessionForTesting('question-save-session')
 let fail=true
 const write=vi.fn<ChatPersistence['write']>(async document=>{if(fail)throw Error('Revision conflict');return {project:'/test',sha256:'saved',content:document}})
 const store=new ChatStore('/test',{read:async()=>({project:'/test',sha256:'absent',content:{version:1,chats:[]}}),write},memoryDraftPersistence('/test'));stores.push(store)
 await store.load();const chat=store.create()
 const saved={...chat,serverRequest:{requestId:answer.requestId,admissionId:answer.admissionId}}
 const observed={...answer,owner:{...answer.owner,id:chat.id}}
 const json=(value:unknown)=>new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}})
 const fetch=vi.fn().mockResolvedValueOnce(json({runs:[{...observed,state:'awaiting-input',continuations:undefined}]})).mockResolvedValueOnce(json(observed));vi.stubGlobal('fetch',fetch)
 const ownerRun=new SavedChatRun(chat.id,saved,{project:'/test',client:serverRunClient,prepare:async()=>{throw Error('No fresh start')},reconcile:async()=>undefined,pin:()=>{},problem:()=>{}})
 await ownerRun.reconnect();const question=ownerRun.getSnapshot().questions![0]
 ownerRun.updateQuestionDraft(question.id,{selected:['one'],text:'Retained answer'})
 store.report(chat.id,{run:ownerRun,state:ownerRun.getSnapshot(),sources:[],ledger:null,blocked:'',model:'fixture',researchConfigured:false})
 await expect(submitQuestion(store,chat.id,question.id,'answered')).rejects.toThrow('Revision conflict')
 await expect(continueQuestion(store,chat.id,question.id)).rejects.toThrow('Revision conflict')
 expect(fetch).toHaveBeenCalledOnce()
 fail=false;await continueQuestion(store,chat.id,question.id)
 await vi.waitFor(()=>expect(fetch).toHaveBeenCalledTimes(2))
 expect(JSON.parse(fetch.mock.calls[1][1].body)).toMatchObject({kind:'answer',questionId:question.id,answer:{selected:['one'],text:'Retained answer'}})
 expect(write.mock.calls.every(call=>call[1]==='absent')).toBe(true)
 expect(fetch.mock.calls.every(([url])=>url!=='/api/assistant-runs')).toBe(true)
 ownerRun.disconnect()
})
