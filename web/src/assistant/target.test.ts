import { expect, it } from 'vitest'
import { selectedAssistant } from './target'

it('selects the subscription target without treating a retained API endpoint as that target', () => {
 const endpoint={url:'https://example.invalid',kind:'gemini' as const,model:'api',models:['api'],tools:[]}
 expect(selectedAssistant({engine:'codex',endpoint})).toBeNull()
 const agent={provider:'openai' as const,authMethod:'subscription' as const,model:'chosen',models:['chosen','other'],tools:[],effort:'high' as const}
 expect(selectedAssistant({engine:'codex',endpoint,agent})).toEqual({models:['chosen','other'],model:'chosen',tools:[]})
 expect(agent.effort).toBe('high')
})

it('retains API selection and its explicitly stated capabilities for the UI', () => {
 const endpoint={url:'https://example.invalid/v1',kind:'openai-compatible' as const,model:'m',models:['m'],tools:[],capabilities:{override:{contextWindow:32768,nativeToolCalls:true}}}
 expect(selectedAssistant({engine:'vercel',endpoint})).toBe(endpoint)
 expect(selectedAssistant({endpoint:null})).toBeNull()
})
