import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ModelControl } from './ModelControl'
import { chatReasoningAgent } from './reasoning'
const catalog=vi.hoisted(()=>({data:{models:[{id:'chosen',efforts:['low','high'],defaultEffort:'low'},{id:'another',efforts:['medium','xhigh'],defaultEffort:'medium'}]},isPending:false,isError:false,error:new Error('Models could not be read'),refetch:vi.fn()}))
vi.mock('../assistant/providers',()=>({useProviderModels:()=>catalog}))
afterEach(()=>{cleanup();catalog.isError=false;catalog.isPending=false;vi.clearAllMocks()})
const agent={provider:'openai',authMethod:'subscription',model:'chosen',tools:[],effort:'high'} as const
const props={id:'model',model:'chosen',models:['chosen'],agent:{...agent,tools:[]},codex:true,enabled:true,disabled:false,onModelChange:vi.fn(),onReasoningChange:vi.fn()}
it('combines model and advertised reasoning in one trigger with a keyboard slider and reset',async()=>{
 const onReasoningChange=vi.fn();render(<ModelControl {...props} onReasoningChange={onReasoningChange}/>)
 expect(screen.queryByText('ChatGPT · Codex')).toBeNull()
 const trigger=screen.getByRole('button',{name:'Model: chosen, Reasoning: High'});fireEvent.click(trigger)
 const slider=await screen.findByRole('slider',{name:'Reasoning'});expect(slider.getAttribute('aria-valuemax')).toBe('1');expect(slider.getAttribute('aria-valuetext')).toBe('High')
 fireEvent.keyDown(slider,{key:'ArrowLeft'});expect(onReasoningChange).toHaveBeenLastCalledWith({model:'chosen',effort:'low'})
 fireEvent.click(screen.getByRole('button',{name:'Reset to default'}));expect(onReasoningChange).toHaveBeenLastCalledWith({model:'chosen',effort:null})
 expect(screen.getByText('ChatGPT · Codex')).toBeTruthy()
})
it('uses the actual model default and exposes no effort outside its catalog',async()=>{
 const onReasoningChange=vi.fn();render(<ModelControl {...props} value={{model:'chosen',effort:null}} onReasoningChange={onReasoningChange}/>);fireEvent.click(screen.getByRole('button',{name:'Model: chosen, Reasoning: Low'}))
 const slider=await screen.findByRole('slider',{name:'Reasoning'});expect(slider.getAttribute('aria-valuenow')).toBe('0');expect(slider.getAttribute('aria-valuetext')).toBe('Low')
 expect((screen.getByRole('button',{name:'Reset to default'}) as HTMLButtonElement).disabled).toBe(true)
 expect(screen.queryByText('Model default')).toBeNull();expect(onReasoningChange).not.toHaveBeenCalled()
})
it('keeps model selection usable without native reasoning and disables the whole control during a run',async()=>{
 const onModelChange=vi.fn();const view=render(<ModelControl {...props} codex={false} models={['chosen','other']} onModelChange={onModelChange}/>);fireEvent.click(screen.getByRole('button',{name:'Model: chosen'}))
 expect(screen.queryByRole('slider')).toBeNull();fireEvent.click(await screen.findByRole('radio',{name:'other'}));expect(onModelChange).toHaveBeenCalledWith('other')
 view.rerender(<ModelControl {...props} disabled/>);expect((screen.getByRole('button',{name:'Model: chosen, Reasoning: High'}) as HTMLButtonElement).disabled).toBe(true)
})
it('keeps overrides per model, distinguishes default from inherited settings, and does not change Admin settings',()=>{
 expect(chatReasoningAgent(props.agent,'chosen')).toBe(props.agent)
 expect(chatReasoningAgent(props.agent,'chosen',{model:'other',effort:'low'})).toBe(props.agent)
 expect(chatReasoningAgent(props.agent,'chosen',{model:'chosen',effort:null})?.effort).toBeUndefined()
 expect(chatReasoningAgent(props.agent,'chosen',{model:'chosen',effort:'low'})?.effort).toBe('low')
 expect(props.agent.effort).toBe('high')
})

it('offers only enabled models and switches their effort scales',async()=>{
 const onModelChange=vi.fn();const view=render(<ModelControl {...props} models={['chosen','another']} onModelChange={onModelChange}/>);fireEvent.click(screen.getByRole('button',{name:'Model: chosen, Reasoning: High'}))
 fireEvent.click(screen.getByRole('radio',{name:'another'}));expect(onModelChange).toHaveBeenCalledWith('another')
 view.rerender(<ModelControl {...props} models={['chosen','another']} model="another" value={{model:'chosen',effort:'high'}} onModelChange={onModelChange}/>)
 expect(screen.getByRole('slider',{name:'Reasoning'}).getAttribute('aria-valuetext')).toBe('Medium')
 expect(screen.getByRole('slider',{name:'Reasoning'}).getAttribute('aria-valuemax')).toBe('1')
 expect(screen.getByText('Extra high')).toBeTruthy()
 expect(props.agent.model).toBe('chosen');expect(props.agent.effort).toBe('high')
})
it('keeps the picker visible but unavailable when its catalog cannot be checked, with retry',async()=>{
 catalog.isError=true;render(<ModelControl {...props}/>);fireEvent.click(screen.getByRole('button',{name:'Model: chosen, Reasoning: High'}))
 expect((screen.getByRole('radio',{name:'chosen'}) as HTMLButtonElement).disabled).toBe(true)
 expect(screen.getByRole('alert').textContent).toContain('Models could not be read')
 fireEvent.click(screen.getByRole('button',{name:'Retry'}));expect(catalog.refetch).toHaveBeenCalledOnce()
})

it('hides unapproved and unavailable models, and filters the allowed list',()=>{
 const view=render(<ModelControl {...props} models={['chosen','missing']}/>);fireEvent.click(screen.getByRole('button',{name:'Model: chosen, Reasoning: High'}))
 expect(screen.getAllByRole('radio')).toHaveLength(1)
 expect(screen.queryByRole('radio',{name:'another'})).toBeNull();expect(screen.queryByRole('radio',{name:'missing'})).toBeNull()
 view.rerender(<ModelControl {...props} models={['chosen','another','missing']}/> )
 fireEvent.change(screen.getByRole('searchbox',{name:'Search models'}),{target:{value:'ANOTH'}})
 expect(screen.getAllByRole('radio')).toHaveLength(1);expect(screen.getByRole('radio',{name:'another'})).toBeTruthy()
 fireEvent.change(screen.getByRole('searchbox',{name:'Search models'}),{target:{value:'unavailable'}})
 expect(screen.queryByRole('radio')).toBeNull();expect(screen.getByText('No models match your search.')).toBeTruthy()
})
