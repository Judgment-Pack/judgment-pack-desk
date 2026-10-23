import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { AssistantOptions } from './AssistantOptions'
vi.mock('../assistant/providers',()=>({useProviderModels:()=>({data:{models:[{id:'review-model',efforts:['low','high']}]}})}))
vi.mock('../files/queries',()=>({useFileListing:()=>({data:{root:'fixture'}})}))
vi.mock('../mcp/McpProvider',()=>({useMcp:()=>({status:'disconnected'})}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({config:{assistant:{engine:'vercel'},research:{}}})}))
vi.mock('../mcp/prompts',()=>({TEST_PACK_PROMPT:'test_pack',usePromptNames:()=>({data:[]})}))
vi.mock('../search/connections',()=>({useSearchConnections:()=>({}),useSearchPreference:()=>({})}))
vi.mock('./ConfigureAssistant',()=>({ConfigureAssistant:()=>null}))
afterEach(cleanup)
it.each([false,true])('edits the run mode and capability critic reasoning (subscription=%s)',codex=>{
 const onReview=vi.fn(),onCriticReasoning=vi.fn()
 render(<QueryClientProvider client={testQueryClient()}><AssistantOptions thinking="off" tools={[]} review="auto" onReview={onReview} onCriticReasoning={onCriticReasoning} codex={codex} criticModel="review-model" /></QueryClientProvider>)
 fireEvent.click(screen.getByRole('button',{name:'Assistant settings'}))
 const control=screen.getByRole('combobox',{name:'Adversarial review'}) as HTMLSelectElement
 expect([...control.options].map(row=>row.value)).toEqual(['','off','auto','on'])
 for(const value of ['off','auto','on','']){
  fireEvent.change(control,{target:{value}})
  expect(onReview).toHaveBeenLastCalledWith(value||undefined)
 }
 expect(screen.getByText('A review runs when the run proposes a pack with saved tests, when the proposal changes an existing pack, or when the assistant asks.')).toBeTruthy()
 const reasoning=screen.getByRole('combobox',{name:'Critic reasoning'}) as HTMLSelectElement
 expect([...reasoning.options].map(row=>row.value)).toEqual(codex?['','low','high']:['','off','low','on','medium','ultra','high'])
 fireEvent.change(reasoning,{target:{value:'high'}})
 expect(onCriticReasoning).toHaveBeenLastCalledWith('high')
 fireEvent.change(reasoning,{target:{value:''}})
 expect(onCriticReasoning).toHaveBeenLastCalledWith(undefined)
})
