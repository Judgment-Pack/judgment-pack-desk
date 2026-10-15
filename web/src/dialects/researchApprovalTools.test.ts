import {beforeEach,expect,it,vi} from 'vitest'
import {dialectActionChatTools} from './chatActionTools'
import {dialectAPI} from './client'
import {researchApprovalAPI} from './researchApproval'
vi.mock('./client',()=>({dialectAPI:vi.fn()}));vi.mock('./researchApproval',()=>({researchApprovalAPI:vi.fn()}))
const id='prep_'+'a'.repeat(32),revision='b'.repeat(64),runId='research_'+'c'.repeat(32)
const saved={sha256:revision,document:{id,name:'Companies',recipeVersion:2,recipe:{research:{}},records:[]}}
const add=vi.fn(),before=vi.fn(),notify=vi.fn()
const tool=dialectActionChatTools({chatId:'chat-1',afterTurnId:()=> 'turn-1',addAction:add,beforeResearch:before,onResearchRequested:notify}).find(t=>t.name==='request_dialect_research')!
beforeEach(()=>{vi.resetAllMocks();vi.mocked(dialectAPI).mockResolvedValue(saved);vi.mocked(researchApprovalAPI).mockResolvedValue({status:'review-required'})})
it('turns a Manual request into an explicit review card without confirming work',async()=>{
 const reply=await tool.execute({id,revision},new AbortController().signal)
 expect(researchApprovalAPI).toHaveBeenCalledExactlyOnceWith('chat-1','/request',{preparationId:id,revision,turnId:'turn-1'})
 expect(add.mock.calls[0][0]).toMatchObject({kind:'research',inputId:id,revision});expect(add.mock.calls[0][0].applied).toBeUndefined()
 expect(JSON.stringify(reply)).toContain('review-required');expect(vi.mocked(dialectAPI).mock.calls).toEqual([['/'+id]])
})
it('retains the exact admitted run even if the chat was stopped during admission',async()=>{
 const controller=new AbortController();vi.mocked(researchApprovalAPI).mockImplementation(async()=>{controller.abort();return {status:'started',runId,state:'queued'} as any})
 const reply=await tool.execute({id,revision},controller.signal)
 expect(add.mock.calls[0][0].applied.runId).toBe(runId);expect(JSON.stringify(reply)).toContain('started');expect(notify).toHaveBeenCalledWith(id)
})
it('never grants authority from model arguments and blocks unsaved edits before requesting',async()=>{
 await expect(tool.execute({id,revision,auto:true},new AbortController().signal)).rejects.toThrow()
 before.mockImplementation(()=>{throw Error('Unsaved edits')})
 await expect(tool.execute({id,revision},new AbortController().signal)).rejects.toThrow('Unsaved edits')
 expect(researchApprovalAPI).not.toHaveBeenCalled();expect(add).not.toHaveBeenCalled()
})
it('does not silently propose another run after an uncertain admission response',async()=>{
 vi.mocked(researchApprovalAPI).mockRejectedValue(Error('Connection interrupted'))
 await expect(tool.execute({id,revision},new AbortController().signal)).rejects.toThrow('Read input activity')
 expect(add).not.toHaveBeenCalled();expect(researchApprovalAPI).toHaveBeenCalledOnce()
})
