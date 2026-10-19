import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { JobsContent } from './JobsView'
import { jobsAPI } from './client'
import { formatDate } from '../i18n'
vi.mock('./client',()=>({jobsAPI:vi.fn()}))
vi.mock('./drafts',async original=>({...await original<typeof import('./drafts')>(),useJobDrafts:()=>({data:[{draft:{id:'draft-one',updatedAt:'2026-09-26T12:00:00Z',values:{name:'Policy review',packId:'policy'}}}],isPending:false})}))
beforeEach(()=>vi.mocked(jobsAPI).mockResolvedValue({items:[]}))
afterEach(()=>{cleanup();vi.clearAllMocks()})
function show(path='/jobs'){render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Tooltip.Provider><MemoryRouter initialEntries={[path]}><JobsContent/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)}
it('shows drafts in the job table, with a badge and compact resume action instead of a second list',async()=>{
 show();const table=await screen.findByRole('table')
 expect(within(table).getByText('Draft')).toBeTruthy()
 expect(within(table).getByRole('link',{name:'Policy review'}).getAttribute('href')).toBe('/jobs/new?draft=draft-one')
 expect(within(table).getByRole('link',{name:'Resume'})).toBeTruthy()
 expect(screen.queryByText('Job drafts')).toBeNull();expect(screen.queryByText('No jobs yet')).toBeNull()
 fireEvent.change(screen.getByRole('searchbox',{name:'Search jobs'}),{target:{value:'absent'}})
 await screen.findByText('No matches');expect(screen.queryByText('Policy review')).toBeNull()
})
it('keeps runnable jobs and saved drafts in the same table',async()=>{
 vi.mocked(jobsAPI).mockResolvedValue({items:[{id:'job-one',name:'Daily review',packTitle:'Policy',packVersion:'1.0.0'}]})
 show();await screen.findByRole('link',{name:'Daily review'})
 expect(screen.getAllByRole('table')).toHaveLength(1)
 expect(screen.getByRole('link',{name:'Run'}).getAttribute('href')).toBe('/jobs/job-one?run=new')
 expect(screen.getByText('Draft')).toBeTruthy()
})
// Collection exports belong to header options; opening the dialog downloads nothing.
it('keeps search first and offers run history through collection options on both tabs',async()=>{
 for(const path of ['/jobs','/jobs/runs']){
  show(path);await screen.findByRole('navigation',{name:'Jobs'})
  expect(screen.getByRole('searchbox',{name:path==='/jobs'?'Search jobs':'Search runs'})).toBeTruthy()
  expect(screen.queryByRole('button',{name:'Download'})).toBeNull()
  const options=screen.getByRole('button',{name:'Collection options'})
  fireEvent.keyDown(options,{key:'Enter'})
  fireEvent.click(await screen.findByRole('menuitem',{name:'Download run history'}))
  const dialog=await screen.findByRole('dialog',{name:'Download run history'})
  expect(within(dialog).getByText('Downloads the runner’s full run-history file, regardless of table filters. Desk saves the original file without verifying it.')).toBeTruthy()
  expect(within(dialog).getByRole('button',{name:'Download'})).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button',{name:'Close'}))
  cleanup()
 }
})
// #223. A job's recent run without a readable submission time: its dot says
// "Not recorded" rather than throwing and taking the job table down with it; a
// run with its time is labelled as before.
it('labels a recent run without a readable submission time Not recorded on its dot',async()=>{
 vi.mocked(jobsAPI).mockResolvedValue({items:[{id:'job-one',name:'Daily review',recentRuns:[{id:'run-one',state:'completed'},{id:'run-two',state:'failed',createdAt:'not a time'},{id:'run-three',state:'completed',createdAt:'2026-09-26T12:00:00Z'}]}]})
 show();await screen.findByRole('link',{name:'Daily review'})
 expect(screen.getByRole('link',{name:'View run · Completed · Submitted: Not recorded'}).getAttribute('href')).toBe('/jobs/job-one/runs/run-one')
 expect(screen.getByRole('link',{name:'View run · Failed · Submitted: Not recorded'}).getAttribute('href')).toBe('/jobs/job-one/runs/run-two')
 expect(screen.getByRole('link',{name:`View run · Completed · Submitted: ${formatDate(new Date('2026-09-26T12:00:00Z'),{dateStyle:'medium',timeStyle:'short'})}`}).getAttribute('href')).toBe('/jobs/job-one/runs/run-three')
})
