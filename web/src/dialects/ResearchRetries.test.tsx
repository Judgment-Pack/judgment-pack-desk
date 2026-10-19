import {useState} from 'react'
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {ResearchRetrySettings,ResearchRetryReview} from './ResearchRetries'
import {ResearchProgress} from './ResearchRun'
import {defaultResearchRetries,type ResearchRetryPolicy,type ResearchSummary} from './research'
afterEach(()=>{cleanup();vi.useRealTimers()})
function Settings({initial}:{initial?:ResearchRetryPolicy}){
 const [value,setValue]=useState(initial)
 return <ResearchRetrySettings value={value} onChange={setValue} disabled={false}/>
}
async function choose(label:string,option:string){fireEvent.keyDown(screen.getByRole('combobox',{name:label}),{key:'Enter'});fireEvent.click(await screen.findByRole('option',{name:option}))}
it('keeps legacy retries Off until explicitly enabled and does not opt in to timeout usage',async()=>{
 render(<Settings/>);
 expect(screen.getByRole('combobox',{name:'Automatic retries'}).textContent).toContain('Off')
 expect(screen.queryByRole('spinbutton',{name:'Maximum retries'})).toBeNull()
 await choose('Automatic retries','Temporary failures')
 expect((screen.getByRole('spinbutton',{name:'Maximum retries'}) as HTMLInputElement).value).toBe('2')
 expect(screen.getByRole('combobox',{name:'Retry timed-out searches'}).textContent).toContain('Off')
 await choose('Retry timed-out searches','On');await choose('Automatic retries','Off');await choose('Automatic retries','Temporary failures')
 expect(screen.getByRole('combobox',{name:'Retry timed-out searches'}).textContent).toContain('Off')
})
it('shows the exact saved policy and request costs in the research review',()=>{
 const view=render(<ResearchRetryReview/>)
 expect(screen.getByText('Off')).toBeTruthy()
 view.rerender(<ResearchRetryReview policy={{...defaultResearchRetries(),maxRetries:1,searchTimeouts:true}}/>)
 expect(screen.getByText('Temporary failures · Retry limit: 1')).toBeTruthy()
 expect(screen.getByText('On')).toBeTruthy()
 expect(screen.getByText(/Each retry uses the saved request limits/)).toBeTruthy()
})
it('counts down from the journal, keeps Stop available, and removes pending retry on Stop',()=>{
 vi.useFakeTimers();vi.setSystemTime(new Date('2026-10-12T12:00:00Z'))
 const run:ResearchSummary={id:'run',state:'running',updatedAt:'2026-10-12T12:00:00Z',recordCount:2,stopRequested:false,pendingRetry:{afterAttempt:'attempt-1',at:'2026-10-12T12:00:05Z',number:2,maxAttempts:3,problemCode:'search-timeout'}}
 const stop=vi.fn();const view=render(<ResearchProgress run={run} onStop={stop} busy={false} onSteps={vi.fn()}/>)
 expect(screen.getByRole('status').textContent).toBe('Search timed out · 2 saved this run')
 expect(screen.getByText('Retrying in 5s · Attempt 2 of 3').getAttribute('aria-live')).toBe('off')
 act(()=>vi.advanceTimersByTime(2000))
 expect(screen.getByText('Retrying in 3s · Attempt 2 of 3')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Stop'}));expect(stop).toHaveBeenCalledOnce()
 view.rerender(<ResearchProgress run={{...run,stopRequested:true}} onStop={stop} busy={false}/>)
 expect(screen.queryByText(/Retrying in/)).toBeNull()
 expect(screen.getByRole('status').textContent).toContain('Stopping research…')
})
it('disables policy controls while a recipe is locked',()=>{
 const change=vi.fn();render(<ResearchRetrySettings value={defaultResearchRetries()} onChange={change} disabled/>)
 for(const control of screen.getAllByRole('combobox'))expect((control as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('spinbutton') as HTMLInputElement).disabled).toBe(true)
 expect(change).not.toHaveBeenCalled()
})

it('names temporary server retries separately from rate limits',()=>{
 const run:ResearchSummary={id:'run',state:'running',updatedAt:'2026-10-12T12:00:00Z',recordCount:1,stopRequested:false,pendingRetry:{afterAttempt:'attempt-1',at:new Date(Date.now()+5000).toISOString(),number:2,maxAttempts:3,problemCode:'search-temporary'}}
 render(<ResearchProgress run={run} onStop={vi.fn()} busy={false}/>)
 expect(screen.getByRole('status').textContent).toBe('Search temporarily unavailable · 1 saved this run')
 expect(screen.queryByText('Search rate limited')).toBeNull()
})
