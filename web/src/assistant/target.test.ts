import { expect, it, vi } from 'vitest'
import { chatReasoningAgent } from '../chat/reasoning'
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

it('binds a discovered chat override to that model without changing Admin or using an API endpoint',()=>{
 const admin={provider:'openai' as const,authMethod:'subscription' as const,model:'default',tools:['get_schema' as const],effort:'high' as const}
 const agent=chatReasoningAgent(admin,'selected',{model:'selected',effort:null})
 const bound=bindExecution({engine:'codex',endpoint:null,agent},'selected','off')
 expect(bound).toMatchObject({agent:{model:'selected'}});expect(bound).not.toHaveProperty('effort')
 expect(bindAgentRun).toHaveBeenLastCalledWith('selected');expect(bindModelCall).not.toHaveBeenCalled()
 expect(admin).toMatchObject({model:'default',effort:'high',tools:['get_schema']})
})
