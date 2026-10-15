import {useState} from 'react'
import {cleanup,fireEvent,render,screen,within} from '@testing-library/react'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {ResearchExecutionSettings,ResearchExecutionDetails} from './ResearchExecution'
import {ResearchTaskSteps} from './ResearchActivity'
import type {ResearchExecutionPolicy,ResearchTask} from './research'

afterEach(cleanup)
it('keeps existing recipes Standard until an explicit edit',()=>{
 const change=vi.fn();render(<Tooltip.Provider><ResearchExecutionSettings connectionLimit={2} disabled={false} onChange={change}/></Tooltip.Provider>)
 expect(screen.getByRole('combobox',{name:'Research approach'}).textContent).toBe('Standard')
 expect(screen.queryByRole('combobox',{name:'Concurrent AI tasks'})).toBeNull();expect(change).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('combobox',{name:'Research approach'}));fireEvent.click(screen.getByRole('option',{name:'Delegated'}))
 expect(change).toHaveBeenCalledExactlyOnceWith({approach:'delegated',concurrency:0})
})
it('supports Auto and a numeric maximum without enabling unsupported Codex concurrency',()=>{
 function Fixture(){const[value,set]=useState<ResearchExecutionPolicy>({approach:'delegated',concurrency:0});return <ResearchExecutionSettings value={value} connectionLimit={1} disabled={false} onChange={set}/>}
 render(<Tooltip.Provider><Fixture/></Tooltip.Provider>)
 expect(screen.getByText('Currently: 1 AI task at a time · ChatGPT connection limit')).toBeTruthy()
 const trigger=screen.getByRole('combobox',{name:'Concurrent AI tasks'});fireEvent.click(trigger)
 const unsupported=screen.getByRole('option',{name:'2 · Unavailable with this connection'})
 expect(unsupported.getAttribute('aria-disabled')).toBe('true');fireEvent.click(unsupported)
 expect(trigger.textContent).toBe('Auto')
 fireEvent.click(screen.getByRole('option',{name:'1'}));expect(screen.getByRole('combobox',{name:'Concurrent AI tasks'}).textContent).toBe('1')
})
it('shows actual reviewed concurrency while preserving the requested maximum',()=>{
 render(<dl><ResearchExecutionDetails execution={{approach:'delegated',requestedConcurrency:2,concurrency:1,connectionLimit:1}}/></dl>)
 expect(screen.getByText(/Up to 1/)).toBeTruthy();expect(screen.getByText(/ChatGPT connection limit/)).toBeTruthy()
})
it('groups operations under their own tasks without losing out-of-order completions',()=>{
 const tasks:ResearchTask[]=[{id:'a',title:'Company A',objective:'Research A',profile:'candidate',state:'running',createdAt:'2026-01-01T00:00:00Z',startedAt:'2026-01-01T00:00:01Z',modelRequests:1,sourceRequests:1},{id:'b',title:'Company B',objective:'Research B',profile:'candidate',state:'complete',createdAt:'2026-01-01T00:00:00Z',modelRequests:1,sourceRequests:0}]
 render(<ol aria-label="Research tasks"><ResearchTaskSteps live tasks={tasks} steps={[{id:'a1',taskId:'a',kind:'page',state:'completed',startedAt:'2026-01-01T00:00:01Z',finishedAt:'2026-01-01T00:00:02Z'},{id:'b1',taskId:'b',kind:'model',state:'completed',startedAt:'2026-01-01T00:00:01Z',finishedAt:'2026-01-01T00:00:03Z'},{id:'a2',taskId:'a',kind:'model',state:'started',startedAt:'2026-01-01T00:00:02Z'}]}/></ol>)
 const groups=screen.getByRole('list',{name:'Research tasks'}).children;expect(groups).toHaveLength(2)
 expect(within(groups[0] as HTMLElement).getAllByText('AI request')).toHaveLength(1)
 expect(within(groups[0] as HTMLElement).getByText('Read a website')).toBeTruthy()
 expect(within(groups[1] as HTMLElement).queryByText('Read a website')).toBeNull()
 expect(groups[0].querySelector('summary > span [class*="spinner"]')).toBeTruthy()
})
