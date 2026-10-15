import { useState } from 'react'
import { cleanup, createEvent, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { QuestionCard } from './QuestionCard'
import { interviewQuestion } from '../testing/question'
import type { SavedQuestion } from '../assistant/questions'
afterEach(cleanup)
function setup(multiple=false,submit=vi.fn(async()=>{})){
 function View(){const [q,set]=useState<SavedQuestion>({id:'q',prompt:{...interviewQuestion,selection:multiple?'multiple':'single'},draft:{selected:[],text:''}});return <QuestionCard question={q} disabled={false} canContinue={false} onDraft={draft=>set({...q,draft})} onSubmit={submit} onContinue={async()=>{}}/>}
 render(<View/>);return submit
}
it('requires explicit submission and allows choice plus optional text',async()=>{
 const submit=setup()
 expect((screen.getByRole('button',{name:'Submit answer'}) as HTMLButtonElement).disabled).toBe(true)
 expect(screen.getAllByRole('radio').every(r=>!(r as HTMLInputElement).checked)).toBe(true)
 fireEvent.click(screen.getByRole('radio',{name:/Head of operations/}));expect(submit).not.toHaveBeenCalled()
 fireEvent.change(screen.getByRole('textbox'),{target:{value:'Regional teams'}})
 fireEvent.click(screen.getByRole('button',{name:'Submit answer'}));await waitFor(()=>expect(submit).toHaveBeenCalledWith('answered'))
})
it('supports text alone, clearing choices, and multiple selections',()=>{
 setup(true)
 fireEvent.click(screen.getByRole('checkbox',{name:/Head of operations/}));fireEvent.click(screen.getByRole('checkbox',{name:/Risk or compliance/}))
 expect(screen.getAllByRole('checkbox').filter(r=>(r as HTMLInputElement).checked)).toHaveLength(2)
 fireEvent.click(screen.getByRole('button',{name:'Clear choice'}));expect((screen.getByRole('button',{name:'Submit answer'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.change(screen.getByRole('textbox'),{target:{value:'Claims manager'}});expect((screen.getByRole('button',{name:'Submit answer'}) as HTMLButtonElement).disabled).toBe(false)
})
it('keeps the draft and explains a failed submission beside its controls',async()=>{
 setup(false,vi.fn(async()=>{throw Error('Could not save')}))
 fireEvent.change(screen.getByRole('textbox'),{target:{value:'My answer'}})
 fireEvent.click(screen.getByRole('button',{name:'Submit answer'}));await screen.findByRole('alert')
 expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('My answer');expect(screen.getByRole('alert').textContent).toContain('Could not save')
})
it('requests help without requiring a choice',async()=>{
 const submit=setup();fireEvent.click(screen.getByRole('button',{name:'Help me decide'}));await waitFor(()=>expect(submit).toHaveBeenCalledWith('help'))
})

it('submits text with Enter and exposes its keyboard instructions',async()=>{
 const submit=setup(), input=screen.getByRole('textbox')
 fireEvent.keyDown(input,{key:'Enter'});expect(submit).not.toHaveBeenCalled()
 fireEvent.change(input,{target:{value:'Regional teams\nClaims operations'}})
 const hint=document.getElementById(input.getAttribute('aria-describedby')!)
 expect(hint?.textContent).toBe('Enter to submit · Shift+Enter for a new line')
 expect(fireEvent.keyDown(input,{key:'Enter'})).toBe(false)
 await waitFor(()=>expect(submit).toHaveBeenCalledExactlyOnceWith('answered'))
 expect((input as HTMLTextAreaElement).value).toBe('Regional teams\nClaims operations')
})
it.each([false,true])('Enter on a focused choice submits the selected answer (multiple=%s)',async multiple=>{
 const submit=setup(multiple), choice=screen.getByRole(multiple?'checkbox':'radio',{name:/Head of operations/})
 fireEvent.click(choice);expect(submit).not.toHaveBeenCalled()
 fireEvent.keyDown(choice,{key:'Enter'})
 await waitFor(()=>expect(submit).toHaveBeenCalledExactlyOnceWith('answered'))
})
it.each([
 {shiftKey:true},{ctrlKey:true},{metaKey:true},{altKey:true},{isComposing:true},{keyCode:229},
])('leaves newline, modifiers and composition to the input: %j',keys=>{
 const submit=setup(), input=screen.getByRole('textbox')
 fireEvent.change(input,{target:{value:'My answer'}})
 expect(fireEvent.keyDown(input,{key:'Enter',...keys})).toBe(true)
 expect(submit).not.toHaveBeenCalled()
})
it('ignores an already handled or held Enter and does not send again while saving',async()=>{
 const submit=setup(false,vi.fn(()=>new Promise<void>(()=>{}))), input=screen.getByRole('textbox')
 fireEvent.change(input,{target:{value:'My answer'}})
 const handled=createEvent.keyDown(input,{key:'Enter'});handled.preventDefault();fireEvent(input,handled)
 fireEvent.keyDown(input,{key:'Enter',repeat:true});expect(submit).not.toHaveBeenCalled()
 fireEvent.keyDown(input,{key:'Enter'})
 await waitFor(()=>expect(submit).toHaveBeenCalledTimes(1))
 expect((input as HTMLTextAreaElement).disabled).toBe(true)
 fireEvent.keyDown(input,{key:'Enter'});fireEvent.submit(screen.getByRole('form'))
 expect(submit).toHaveBeenCalledTimes(1)
})
