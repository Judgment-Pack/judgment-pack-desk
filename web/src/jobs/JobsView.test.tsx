import { loadJobDraft } from './drafts'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { CreateJobContent } from './JobsView'
import { jobsAPI, JobsRequestError } from './client'
import { readReleaseTests } from './releaseTests'
vi.mock('./drafts',async original=>({...await original<typeof import('./drafts')>(),loadJobDraft:vi.fn()}))
vi.mock('./MappedInputFields',()=>({MappedInputFields:()=>null}))
vi.mock('./client', async original => ({ ...await original<typeof import('./client')>(), jobsAPI: vi.fn() }))
vi.mock('./releaseTests', () => ({ readReleaseTests: vi.fn() }))
const { pack, packs } = vi.hoisted(() => ({
 pack: { data: { raw: '{"version":"1"}', document: { title: 'Example pack', version: '1' } }, refetch: vi.fn() },
 packs: { data: { packs: [{ id: 'pack', matrixPath: 'cases.json' }] }, refetch: vi.fn() },
}))
vi.mock('../mcp/queries', () => ({ usePacks: () => packs, usePack: () => pack }))
const source = { packKey: 'pack', suiteRevision: 1, caseNames: { clean: 'Clean case' }, exploratoryCount: 0 }
const saved = { project: 'project', matrix: '{"matrixVersion":"3","cases":[{"id":"clean"}]}', testSource: source }
const release = { id: 'release', pack: '{"version":"1"}', packVersion: '1', tests: 'passed', preview: { disposition: { kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: { state: 'none' } } } }
beforeEach(() => {
 pack.refetch.mockResolvedValue(pack); packs.refetch.mockResolvedValue(packs)
 vi.mocked(readReleaseTests).mockResolvedValue(saved)
 vi.mocked(jobsAPI).mockResolvedValue(release)
})
afterEach(() => { cleanup(); vi.resetAllMocks() })
function renderCreate() { return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}><Tooltip.Provider><MemoryRouter initialEntries={['/jobs/new?pack=pack']}><CreateJobContent /></MemoryRouter></Tooltip.Provider></QueryClientProvider>) }
function next(){fireEvent.click(screen.getByRole('button',{name:'Continue'}));const input=screen.queryByRole('combobox',{name:'Input configuration'});if(input){fireEvent.keyDown(input,{key:'Enter'});fireEvent.click(screen.getByRole('option',{name:'Manual / API'}))}}
function toReview(){fireEvent.change(screen.getByLabelText('Job name'),{target:{value:'Intake'}});next();next();next()}
async function check() {
 toReview()
 fireEvent.click(screen.getByRole('button', { name: 'Check release' }))
 await screen.findByText('Review this release')
}
const button = () => screen.getByRole('button', { name: 'Create job' }) as HTMLButtonElement
const review = () => screen.getByLabelText('I reviewed this release, its test status and sample result.') as HTMLInputElement
it('runs saved expectations against the current pack and requires explicit review, invalidating it when sample inputs change', async () => {
 renderCreate(); expect(screen.queryByRole('button',{name:'Create job'})).toBeNull()
 await check()
 expect(jobsAPI).toHaveBeenCalledWith('previews', { pack: pack.data.raw, input: { facts: {} }, matrix: saved.matrix, testSource: source })
 expect(readReleaseTests).toHaveBeenCalledWith('pack', 'cases.json')
 expect(button().disabled).toBe(true)
 fireEvent.click(review()); expect(button().disabled).toBe(false)
 fireEvent.change(screen.getByLabelText('Facts (JSON)'), { target: { value: '{"changed":true}' } })
 expect(button().disabled).toBe(true); expect(screen.queryByText('Review this release')).toBeNull()
 expect(jobsAPI).toHaveBeenCalledTimes(1)
})
it('rejects malformed sample input before invoking the runner and preserves omitted evidence', async () => {
 renderCreate();fireEvent.change(screen.getByLabelText('Job name'),{target:{value:'Intake'}});next()
 fireEvent.change(screen.getByLabelText('Facts (JSON)'), { target: { value: '{' } })
 expect((screen.getByRole('button',{name:'Continue'}) as HTMLButtonElement).disabled).toBe(true)
 await screen.findByRole('alert'); expect(jobsAPI).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('Facts (JSON)'), { target: { value: '{"active":false}' } })
 next();next();fireEvent.click(screen.getByRole('button', { name: 'Check release' }))
 await waitFor(() => expect(jobsAPI).toHaveBeenCalledWith('previews', expect.objectContaining({ input: { facts: { active: false } } })))
})
it.each(['failed','error'])('blocks creating a job after a %s check', async tests => {
 vi.mocked(jobsAPI).mockResolvedValue({ ...release, tests })
 renderCreate(); await check()
 expect(review().disabled).toBe(true); expect(button().disabled).toBe(true)
 expect(screen.getByText('Correct the pack or saved cases, then check a new release before creating a job.')).toBeTruthy()
})
it('allows an explicitly reviewed untested release without calling it passed', async () => {
 vi.mocked(readReleaseTests).mockResolvedValue({ ...saved, matrix: undefined })
 vi.mocked(jobsAPI).mockResolvedValue({ ...release, tests: 'not-run' })
 renderCreate(); await check()
 expect(screen.getByText('Not run')).toBeTruthy(); expect(screen.queryByText('Passed')).toBeNull()
 expect(jobsAPI).toHaveBeenCalledWith('previews', { pack: pack.data.raw, input: { facts: {} } })
 expect(button().disabled).toBe(true); fireEvent.click(review()); expect(button().disabled).toBe(false)
})
it.each(['tests','pack','project'])('rechecks %s before creating and refuses a stale review', async changed => {
 renderCreate(); await check(); fireEvent.click(review())
 if (changed === 'pack') pack.refetch.mockResolvedValue({ data: { ...pack.data, raw: '{"version":"2"}' } })
 else vi.mocked(readReleaseTests).mockResolvedValue({ ...saved, ...(changed === 'tests' ? { matrix: 'changed' } : { project: 'other' }) })
 fireEvent.click(button())
 await screen.findByText('The pack or saved tests changed. Check the release again before creating the job.')
 expect(jobsAPI).toHaveBeenCalledTimes(1); expect(button().disabled).toBe(true)
})
it('does not silently release untested when reading saved tests fails', async () => {
 vi.mocked(readReleaseTests).mockRejectedValue(new Error('Cannot read saved tests'))
 renderCreate(); toReview();fireEvent.click(screen.getByRole('button', { name: 'Check release' }))
 await screen.findByText('Cannot read saved tests'); expect(jobsAPI).not.toHaveBeenCalled(); expect(button().disabled).toBe(true)
})
const untested = 'This installation creates jobs only from releases whose saved tests ran and passed. Save tests for this pack, then check a new release.'
function eventTrigger(){fireEvent.change(screen.getByLabelText('Job name'),{target:{value:'Intake'}});next();next()
 const choice=screen.getByRole('combobox',{name:'Trigger'});fireEvent.keyDown(choice,{key:'Enter'});fireEvent.click(screen.getByRole('option',{name:'Authenticated event'}));next()}
it.each([['plain creation', 'Create job', false], ['creation with a first trigger', 'Create paused job', true]] as const)('shows the runner refusing an untested release in place of a job, on %s', async (_, label, withTrigger) => {
 vi.mocked(readReleaseTests).mockResolvedValue({ ...saved, matrix: undefined })
 vi.mocked(jobsAPI).mockResolvedValue({ ...release, tests: 'not-run' })
 renderCreate()
 if (withTrigger) eventTrigger(); else toReview()
 fireEvent.click(screen.getByRole('button', { name: 'Check release' })); await screen.findByText('Review this release')
 fireEvent.click(review())
 vi.mocked(jobsAPI).mockRejectedValue(new JobsRequestError(untested, 409, 'release_untested'))
 const create = screen.getByRole('button', { name: label }) as HTMLButtonElement
 fireEvent.click(create)
 expect((await screen.findByRole('alert')).textContent).toBe(untested)
 expect(jobsAPI).toHaveBeenLastCalledWith('jobs', { name: 'Intake', releaseId: 'release', reviewed: true, ...(withTrigger ? { trigger: expect.objectContaining({ kind: 'event' }) } : {}) })
 expect(screen.getByText('No saved tests were run for this release. Review it as untested before creating a job.')).toBeTruthy()
 expect(review().checked).toBe(false); expect(review().disabled).toBe(true); expect(create.disabled).toBe(true)
})
it('keeps other creation refusals as an ordinary problem', async () => {
 vi.mocked(jobsAPI).mockResolvedValue({ ...release, tests: 'not-run' })
 renderCreate(); await check(); fireEvent.click(review())
 vi.mocked(jobsAPI).mockRejectedValue(new JobsRequestError('The release is not ready.', 409, 'release_not_ready'))
 fireEvent.click(button())
 await screen.findByText('The release is not ready.')
 expect(review().disabled).toBe(false)
})
it('creates the immutable reviewed release after checking freshness again', async () => {
 renderCreate(); await check(); fireEvent.click(review())
 vi.mocked(jobsAPI).mockResolvedValue({ id: 'job-id' })
 fireEvent.click(button())
 await waitFor(() => expect(jobsAPI).toHaveBeenCalledWith('jobs', { name: 'Intake', releaseId: 'release', reviewed: true }))
 expect(readReleaseTests).toHaveBeenCalledTimes(2)
})

it('starts an operational run with fresh facts and omitted evidence',async()=>{
 const {JobsContent}=await import('./JobsView')
 const {Routes,Route}=await import('react-router-dom')
 vi.mocked(jobsAPI).mockImplementation(async(path:string)=>{
  if(path==='jobs/job-one')return {job:{id:'job-one',name:'Intake'},release:{...release,title:'Policy',sample:{facts:{secret:'release sample'},evidence:{proof:'present'}}}} as never
  if(path.startsWith('jobs/job-one/runs?'))return {items:[]} as never
  if(path==='jobs/job-one/runs')return {id:'run-new',jobId:'job-one'} as never
  return {} as never
 })
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Tooltip.Provider><MemoryRouter initialEntries={['/jobs/job-one?run=new']}><Routes><Route path="/jobs/:jobId" element={<JobsContent/>}/><Route path="/jobs/:jobId/runs/:runId" element={<span/>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 await screen.findByRole('button',{name:'Submit run'})
 expect((screen.getByLabelText('Facts (JSON)') as HTMLTextAreaElement).value).toBe('{}')
 expect((screen.getByLabelText('Supply evidence availability') as HTMLInputElement).checked).toBe(false)
 fireEvent.click(screen.getByRole('button',{name:'Submit run'}))
 await waitFor(()=>expect(jobsAPI).toHaveBeenCalledWith('jobs/job-one/runs',{facts:{}},expect.any(String)))
})
it('sends global run search and attention filters to the runner',async()=>{
 const {JobsContent}=await import('./JobsView')
 vi.mocked(jobsAPI).mockImplementation(async path=>({items:path==='runs?after=0'?[{id:'run-one',jobId:'job-one',jobName:'Intake',state:'completed',createdAt:'2026-09-26T12:00:00Z'}]:[]}) as never)
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Tooltip.Provider><MemoryRouter initialEntries={['/jobs/runs']}><JobsContent/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 await waitFor(()=>expect(jobsAPI).toHaveBeenCalledWith('runs?after=0'))
 fireEvent.change(await screen.findByLabelText('Search runs'),{target:{value:'vendor'}})
 fireEvent.click(screen.getByLabelText('Needs attention'))
 await waitFor(()=>expect(jobsAPI).toHaveBeenCalledWith('runs?q=vendor&review=true&after=0'))
 await screen.findByText('No matches')
 expect(screen.getByLabelText('Search runs')).toBeTruthy()
})

it('keeps a resumed job editor and its unsaved fields through failed background reads',async()=>{
 const values={name:'Saved job',packId:'pack',inputMode:'manual' as const,facts:'{}',supplied:false,evidence:'{}'}
 vi.mocked(loadJobDraft).mockResolvedValueOnce({file:{path:'.desk/job-drafts/example.json',content:'{}',bytes:2,sha256:'base'},draft:{version:1,id:'example',updatedAt:'2026-09-26T12:00:00Z',status:'draft',values}}).mockRejectedValue(Error('File unavailable'))
 const query=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<QueryClientProvider client={query}><MemoryRouter initialEntries={['/jobs/new?draft=example']}><CreateJobContent/></MemoryRouter></QueryClientProvider>)
 const name=await screen.findByLabelText('Job name')
 expect((name as HTMLInputElement).value).toBe('Saved job')
 fireEvent.change(name,{target:{value:'My unsaved change'}})
 await query.invalidateQueries({queryKey:['job-draft','example']})
 await waitFor(()=>expect(loadJobDraft).toHaveBeenCalledTimes(2))
 expect(screen.getByLabelText('Job name')).toBe(name)
 expect((name as HTMLInputElement).value).toBe('My unsaved change')
})
