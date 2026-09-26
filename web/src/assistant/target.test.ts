import { expect, it, vi } from 'vitest'
import { bindExecution, selectedAssistant } from './target'
import { bindAgentRun } from './agentTransport'
import { bindModelCall } from './session'
vi.mock('./agentTransport',()=>({bindAgentRun:vi.fn(()=>vi.fn())}))
vi.mock('./session',()=>({bindModelCall:vi.fn(()=>vi.fn())}))
it('binds subscription runs without constructing an API endpoint capability',()=>{
 const config={engine:'codex' as const,endpoint:null,agent:{provider:'openai' as const,authMethod:'subscription' as const,model:'chosen',tools:[],effort:'high' as const}}
 const bound=bindExecution(config,'chosen','ultra')
 expect(bound).toMatchObject({agent:{model:'chosen'},effort:'high'})
 expect(bound).not.toHaveProperty('model');expect(bound).not.toHaveProperty('thinking')
 expect(bindAgentRun).toHaveBeenCalledWith('chosen');expect(bindModelCall).not.toHaveBeenCalled()
 config.agent.model='changed'
 expect(bound).toMatchObject({agent:{model:'chosen'}})
 expect(()=>bindExecution(config,'chosen','off')).toThrow('Choose a model')
})
it('never uses a retained API configuration when the subscription target is absent',()=>{
 const config={engine:'codex' as const,endpoint:{url:'https://example.invalid',kind:'gemini' as const,model:'api',models:['api'],tools:[]}}
 expect(selectedAssistant(config)).toBeNull()
 expect(()=>bindExecution(config,'api','off')).toThrow('ChatGPT subscription')
})
