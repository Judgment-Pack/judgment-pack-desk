import { loadJobDraft,saveJobDraft } from './drafts'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider, onlineManager } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter, useLocation, useNavigate, Routes, Route } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { CreateJobContent, JobsContent } from './JobsView'
import { jobsAPI, JobsRequestError } from './client'
import { readReleaseTests } from './releaseTests'
import { refreshRunnerKey } from './runnerKey'
import { DeskConfigProvider } from '../config/DeskConfigProvider'
import { createHash, webcrypto } from 'node:crypto'
import { readReview, type Review } from '../packs/review/client'
vi.mock('./drafts',async original=>({...await original<typeof import('./drafts')>(),loadJobDraft:vi.fn(),saveJobDraft:vi.fn()}))
const mappedEditor=vi.hoisted(()=>({props:undefined as import('react').ComponentProps<typeof import('./MappedInputFields').MappedInputFields>|undefined}))
vi.mock('./MappedInputFields',()=>({MappedInputFields:(props:import('react').ComponentProps<typeof import('./MappedInputFields').MappedInputFields>)=>{mappedEditor.props=props;return null}}))
vi.mock('./client', async original => ({ ...await original<typeof import('./client')>(), jobsAPI: vi.fn() }))
vi.mock('./releaseTests', () => ({ readReleaseTests: vi.fn() }))
vi.mock('./runnerKey', async original => ({ ...await original<typeof import('./runnerKey')>(), refreshRunnerKey: vi.fn() }))
vi.mock('../packs/review/client', async original => ({ ...await original<typeof import('../packs/review/client')>(), readReview: vi.fn() }))
const { pack, packs } = vi.hoisted(() => ({
 pack: { data: { raw: '{"version":"1"}', document: { id:'policy',title: 'Example pack', version: '1' } }, refetch: vi.fn() },
 packs: { data: { packs: [{ id: 'pack', packId:'policy', matrixPath: 'cases.json' }] }, refetch: vi.fn() },
}))
vi.mock('../mcp/queries', () => ({ usePacks: () => packs, usePack: () => pack }))
const source = { packKey: 'pack', suiteRevision: 1, caseNames: { clean: 'Clean case' }, exploratoryCount: 0 }
const saved = { project: 'project', matrix: '{"matrixVersion":"3","cases":[{"id":"clean"}]}', testSource: source }
const release = { packId:'policy',sample:{facts:{}},id: 'release', pack: '{"version":"1"}', packVersion: '1', tests: 'passed', preview: { disposition: { kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: { state: 'none' } } } }
beforeEach(() => {
 vi.mocked(saveJobDraft).mockImplementation(async(id,values)=>{const result={file:{path:'draft',sha256:'sha',content:'',bytes:0},draft:{version:1 as const,id,updatedAt:new Date().toISOString(),status:'draft' as const,values}};vi.mocked(loadJobDraft).mockResolvedValue(result);return result})
 pack.refetch.mockResolvedValue(pack); packs.refetch.mockResolvedValue(packs)
 vi.mocked(readReview).mockRejectedValue(new Error('The review could not be loaded. Please try again.'))
 vi.mocked(readReleaseTests).mockResolvedValue(saved)
 vi.mocked(jobsAPI).mockResolvedValue(release)
})
afterEach(() => { cleanup(); vi.resetAllMocks(); vi.unstubAllGlobals() })
function renderCreate(path='/jobs/new?pack=pack') { return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}><Tooltip.Provider><MemoryRouter initialEntries={[path]}><CreateJobContent /></MemoryRouter></Tooltip.Provider></QueryClientProvider>) }
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
/** Desk's desk-config answer, stating this installation's tested-releases policy. */
function statePolicy(requireTestedReleases: boolean) {
 vi.stubGlobal('fetch', async (url: string) => String(url).includes('/api/desk-config')
  ? { ok: true, status: 200, statusText: '', text: async () => JSON.stringify({ path: '/desk.json', present: false, sha256: '', project: { dir: '/p', file: '/p/jpack-desk.json' }, runtime: { bin: 'jpack' }, jobs: { requireTestedReleases } }) }
  : { ok: false, status: 404, statusText: '', text: async () => JSON.stringify({ error: 'no such file' }) })
}
const refusesNote = 'No saved tests were run for this release, and this installation refuses to create a job from an untested release. Save tests for this pack, then check a new release. To turn this policy off, restart Desk with --runner-require-tested-releases=false.'
it('names the installation policy beside the runner refusing an untested release', async () => {
 statePolicy(true)
 vi.mocked(readReleaseTests).mockResolvedValue({ ...saved, matrix: undefined })
 vi.mocked(jobsAPI).mockResolvedValue({ ...release, tests: 'not-run' })
 render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><DeskConfigProvider><Tooltip.Provider><MemoryRouter initialEntries={['/jobs/new?pack=pack']}><CreateJobContent /></MemoryRouter></Tooltip.Provider></DeskConfigProvider></QueryClientProvider>)
 await check()
 expect(await screen.findByText(refusesNote)).toBeTruthy()
 fireEvent.click(review())
 vi.mocked(jobsAPI).mockRejectedValue(new JobsRequestError(untested, 409, 'release_untested'))
 fireEvent.click(button())
 expect((await screen.findByRole('alert')).textContent).toBe(untested)
 expect(screen.getByText(refusesNote)).toBeTruthy(); expect(button().disabled).toBe(true)
})
it('tells a job made from an untested release that the policy does not stop it', async () => {
 statePolicy(true)
 const { JobsContent } = await import('./JobsView')
 const { Routes, Route } = await import('react-router-dom')
 vi.mocked(jobsAPI).mockImplementation(async (path: string) => {
  if (path === 'jobs/job-one') return { job: { id: 'job-one', name: 'Intake' }, release: { ...release, title: 'Policy', tests: 'not-run', sample: { facts: {} } } } as never
  return { items: [] } as never
 })
 render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><DeskConfigProvider><Tooltip.Provider><MemoryRouter initialEntries={['/jobs/job-one?tab=release']}><Routes><Route path="/jobs/:jobId" element={<JobsContent />} /></Routes></MemoryRouter></Tooltip.Provider></DeskConfigProvider></QueryClientProvider>)
 expect(await screen.findByText('No saved tests were run for this release. This job was created before this installation refused untested releases, and it keeps running. A new job needs a release whose saved tests ran and passed.')).toBeTruthy()
 expect(screen.queryByText(refusesNote)).toBeNull()
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
 // A job made may have started Runner: Gates asks again what it signs with.
 await waitFor(() => expect(refreshRunnerKey).toHaveBeenCalledTimes(1))
})

it('starts an operational run with fresh facts and omitted evidence',async()=>{
 vi.stubGlobal('fetch',async()=>new Response('Not found',{status:404}))
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
 // A run submitted may have started Runner: Gates asks again what it signs with.
 await waitFor(()=>expect(refreshRunnerKey).toHaveBeenCalledTimes(1))
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

// #223. Runner v0.5.0 always writes `createdAt`, but a record from an older or
// edited store may not, and one such run threw while rendering and took the
// whole Runs tab down with it. It now says the time is not recorded, as the
// Activity tab does; a run that has its time is shown as before.
it('shows Not recorded in the Runs table for a run without a readable submission time, instead of throwing',async()=>{
 const {JobsContent}=await import('./JobsView')
 const {formatDate}=await import('../i18n')
 const run={jobId:'job-one',jobName:'Intake',state:'completed'}
 vi.mocked(jobsAPI).mockImplementation(async path=>({items:path==='runs?after=0'?[
  {...run,id:'run-aaaaaaaa',createdAt:'2026-09-26T12:00:00Z'},
  {...run,id:'run-bbbbbbbb'},
  {...run,id:'run-cccccccc',createdAt:'not a time'},
 ]:[]}) as never)
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Tooltip.Provider><MemoryRouter initialEntries={['/jobs/runs']}><JobsContent/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 const submitted=async(run:string)=>(await screen.findByRole('link',{name:run})).closest('tr')!.querySelectorAll<HTMLTableCellElement>('td:not([data-selection-cell])')[4]!.textContent
 expect(await submitted('bbbbbbbb')).toBe('Not recorded')
 expect(await submitted('cccccccc')).toBe('Not recorded')
 expect(await submitted('aaaaaaaa')).toBe(formatDate(new Date('2026-09-26T12:00:00Z'),{dateStyle:'medium',timeStyle:'short'}))
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

it('uses the Desk AI origin in the test matrix sent with a job release preview', async () => {
 const { importMatrix, emptySuite } = await import('../packs/test-workspace/model')
 const storage = await import('../packs/test-workspace/store')
 const actual = await vi.importActual<typeof import('./releaseTests')>('./releaseTests')
 const cases = importMatrix({cases: [{id: 'proposed', origin: 'manual', facts: {}, expectedDisposition: {kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: {state: 'none'}}}]}, 'ai')
 const read = vi.spyOn(storage, 'readTests').mockResolvedValue({project: 'project', sha256: 'hash', content: {version: 1, suites: {pack: {...emptySuite(), cases, recovered: ['matrix:cases.json']}}}})
 vi.mocked(readReleaseTests).mockImplementation(actual.readReleaseTests)
 try {
  renderCreate(); await check()
  const preview = vi.mocked(jobsAPI).mock.calls.find(([path]) => path === 'previews')![1] as {matrix: string}
  expect(JSON.parse(preview.matrix).cases[0].origin).toBe('ai')
 } finally { read.mockRestore() }
})

/** The project's review, with its lock pinning `locked` for the decision id `pack`. */
const sha = (text: string) => 'sha256:' + createHash('sha256').update(text, 'utf8').digest('hex')
function projectReview(locked: string | undefined, rest: Partial<Review> = {}): Review {
 return { status: 'valid', locked: true, findings: [], diagnostics: [], contents: {}, token: 'f'.repeat(64), files: [
  { kind: 'config', path: 'jpack.json', lock: 'same', locked: 'sha256:config', now: { state: 'text', digest: 'sha256:config' } },
  { kind: 'pack', id: 'pack', path: 'pack.json', lock: locked ? 'same' : 'none', ...(locked ? { locked } : {}), now: { state: 'text', digest: sha(release.pack) } }
 ], ...rest }
}
const standing = async (state: string) => { await waitFor(() => expect(document.querySelector('[data-standing]')?.getAttribute('data-standing')).toBe(state)); return document.querySelector('[data-label]')?.textContent }
it('shows whether the release’s pack bytes are in the reviewed set, for the decision id the release was checked for', async () => {
 // Runner's packId is the pack document's own id; the project's lock is keyed by the decision id.
 vi.mocked(jobsAPI).mockResolvedValue({ ...release, packId: 'https://example.com/judgment-packs/example' })
 vi.mocked(readReview).mockResolvedValue(projectReview(sha(release.pack)))
 renderCreate(); await check()
 expect(await standing('reviewed')).toBe('In the reviewed set')
 expect(screen.getByText(/The lock pins these exact bytes for pack\./)).toBeTruthy()
})
const states: [string, () => Promise<Review>][] = [
 ['reviewed', async () => projectReview(sha(release.pack))],
 ['draft', async () => projectReview(sha('{"version":"0"}'), { status: 'invalid', findings: [{ name: 'document-drift', kind: 'pack', id: 'pack', path: 'pack.json' }] })],
 ['no-lock', async () => ({ ...projectReview(undefined), status: 'error', locked: false, diagnostics: [{ code: 'JPS-LOCK-ABSENT', message: 'There is no reviewed-set lock.' }] })],
 ['config-drift', async () => projectReview(sha(release.pack), { status: 'invalid', findings: [{ name: 'config-drift', path: 'jpack.json' }] })],
 ['unreadable', async () => { throw new Error('The project could not be reviewed.') }],
]
it.each(states)('refuses nothing: with the standing %s, the job is created exactly as before', async (state, answer) => {
 vi.mocked(readReview).mockImplementation(answer)
 renderCreate(); await check(); await standing(state)
 expect(review().disabled).toBe(false); fireEvent.click(review()); expect(button().disabled).toBe(false)
 vi.mocked(jobsAPI).mockResolvedValue({ id: 'job-id' })
 fireEvent.click(button())
 await waitFor(() => expect(jobsAPI).toHaveBeenLastCalledWith('jobs', { name: 'Intake', releaseId: 'release', reviewed: true }))
 expect(screen.queryByRole('alert')).toBeNull()
})
it('leaves the tested-releases policy as it was: a reviewed release whose tests failed is still not a job', async () => {
 vi.mocked(jobsAPI).mockResolvedValue({ ...release, tests: 'failed' })
 vi.mocked(readReview).mockResolvedValue(projectReview(sha(release.pack)))
 renderCreate(); await check()
 expect(await standing('reviewed')).toBe('In the reviewed set')
 expect(review().disabled).toBe(true); expect(button().disabled).toBe(true)
})

it('upgrades only the reviewed frozen job revision through the existing checked-release flow',async()=>{
 const base={job:{id:'job-base',name:'Existing',releaseId:'old-release',revision:4},release:{...release,packId:'policy',packVersion:'0.1.0'}}
 vi.mocked(jobsAPI).mockImplementation(async(path)=>path==='jobs/job-base'?base:path==='previews'?release:base.job)
 renderCreate('/jobs/new?upgrade=job-base')
 await screen.findByDisplayValue('Existing')
 expect((screen.getByLabelText('Job name') as HTMLInputElement).disabled).toBe(true)
 expect(screen.getByRole('button',{name:'Save draft'})).toBeTruthy()
 next();next();next();fireEvent.click(screen.getByRole('button',{name:'Check release'}))
 await screen.findByText('Review this release')
 expect((screen.getByRole('button',{name:'Upgrade job'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(review());fireEvent.click(screen.getByRole('button',{name:'Upgrade job'}))
 await waitFor(()=>expect(jobsAPI).toHaveBeenCalledWith('jobs/job-base/upgrade',{releaseId:release.id,baseReleaseId:'old-release',baseRevision:4,reviewed:true}))
 expect(vi.mocked(jobsAPI).mock.calls.some(([path])=>path==='jobs')).toBe(false)
})

it('recovers a historical completed upgrade after reload without restoring approval or checking changed project inputs',async()=>{
 const attempt={jobId:'job-base',baseReleaseId:'old-release',baseRevision:4,submittedReleaseId:'accepted-release'}
 const values={name:'Existing',packId:'pack',facts:'{}',supplied:false,evidence:'{}',inputMode:'manual' as const,upgrade:attempt}
 vi.mocked(loadJobDraft).mockResolvedValue({file:{path:'draft',content:'',bytes:0,sha256:'retained'},draft:{version:1,id:'recover',status:'draft',updatedAt:'2026-10-16T12:00:00Z',values}})
 const receipt={id:'job-base',name:'Existing',releaseId:'accepted-release',revision:5,createdAt:''}
 vi.mocked(jobsAPI).mockImplementation(async(path)=>path.includes('/upgrade-receipt?')?receipt:{job:{...receipt,releaseId:'later-release',revision:6},release})
 vi.mocked(readReleaseTests).mockRejectedValue(Error('Project changed'))
 renderCreate('/jobs/new?draft=recover')
 await screen.findByText('This upgrade has already been applied.')
 expect(screen.getByRole('link',{name:'Open job'}).getAttribute('href')).toBe('/jobs/job-base')
 expect(readReleaseTests).not.toHaveBeenCalled()
 expect(vi.mocked(jobsAPI).mock.calls.every(([,body])=>body===undefined)).toBe(true)
 expect(saveJobDraft).not.toHaveBeenCalled()
})

it('reconciles an interrupted attempt before checking changed inputs on an explicit retry',async()=>{
 const base={job:{id:'job-base',name:'Existing',releaseId:'old-release',revision:4},release:{...release,packId:'policy',packVersion:'0.1.0'}}
 const receipt={...base.job,releaseId:release.id,revision:5}
 vi.mocked(jobsAPI).mockImplementation(async(path)=>{
  if(path==='jobs/job-base')return base
  if(path==='previews')return release
  if(path.includes('/upgrade-receipt?'))return receipt
  if(path.endsWith('/upgrade')){vi.mocked(readReleaseTests).mockRejectedValue(Error('Project changed'));throw Error('Response lost')}
  throw Error('Unexpected write')
 })
 renderCreate('/jobs/new?upgrade=job-base');await screen.findByDisplayValue('Existing')
 next();next();next();fireEvent.click(screen.getByRole('button',{name:'Check release'}));await screen.findByText('Review this release')
 fireEvent.click(review());fireEvent.click(screen.getByRole('button',{name:'Upgrade job'}));await screen.findByText('Response lost')
 const reads=vi.mocked(readReleaseTests).mock.calls.length
 fireEvent.click(screen.getByRole('button',{name:'Upgrade job'}))
 await waitFor(()=>expect(jobsAPI).toHaveBeenCalledWith(expect.stringContaining('/upgrade-receipt?')))
 expect(readReleaseTests).toHaveBeenCalledTimes(reads)
 expect(vi.mocked(jobsAPI).mock.calls.filter(([path])=>path.endsWith('/upgrade'))).toHaveLength(1)
})


it.each(['cancel','leave'] as const)('does not navigate after a delayed research checkpoint when its owner will %s',async action=>{
 let finish!:(value:Awaited<ReturnType<typeof saveJobDraft>>)=>void
 vi.mocked(saveJobDraft).mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
 function Location(){const location=useLocation(),navigate=useNavigate();return <><output data-testid="location">{location.pathname+location.search}</output><button onClick={()=>navigate('/elsewhere')}>Leave editor</button></>}
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Tooltip.Provider><MemoryRouter initialEntries={['/jobs/new?pack=pack']}><Location/><Routes><Route path="/jobs/new" element={<CreateJobContent/>}/><Route path="/elsewhere" element={<span>Another page</span>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 await waitFor(()=>expect(mappedEditor.props?.onPersistResearchDraft).toBeDefined())
 const task=new AbortController(),draft={text:'{}',caseText:'{}'}
 let pending!:Promise<void>
 act(()=>{pending=mappedEditor.props!.onPersistResearchDraft!(draft,task.signal)})
 expect((screen.getByLabelText('Job name') as HTMLInputElement).disabled).toBe(true)
 expect(screen.getByRole('combobox',{name:'Input configuration',hidden:true}).getAttribute('data-disabled')).not.toBeNull()
 if(action==='leave')fireEvent.click(screen.getByRole('button',{name:'Leave editor'}))
 else task.abort()
 await act(async()=>{const [id,values]=vi.mocked(saveJobDraft).mock.calls[0]!;finish({file:{path:'draft',content:'',bytes:0,sha256:'saved'},draft:{version:1,id,updatedAt:new Date().toISOString(),status:'draft',values}});await pending})
 expect(screen.getByTestId('location').textContent).toBe(action==='leave'?'/elsewhere':'/jobs/new?pack=pack')
 if(action==='cancel')expect((screen.getByLabelText('Job name') as HTMLInputElement).disabled).toBe(false)
})

function authorityRunPage(client=new QueryClient({defaultOptions:{queries:{retry:false}}})){
 return render(<QueryClientProvider client={client}><Tooltip.Provider><MemoryRouter initialEntries={['/jobs/job-one?run=new']}><Routes><Route path="/jobs/:jobId" element={<JobsContent/>}/><Route path="/jobs/:jobId/runs/:runId" element={<span/>}/></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
}
function authorityRunData(onSubmit?: (key?:string)=>unknown){
 vi.mocked(jobsAPI).mockImplementation(async(path,body,key)=>{
  if(path==='jobs/job-one')return {job:{id:'job-one',name:'Intake'},release:{...release,title:'Policy'}} as never
  if(path.startsWith('jobs/job-one/runs?'))return {items:[]} as never
  if(path==='jobs/job-one/runs'&&body!==undefined){onSubmit?.(key);return {id:'run-new',jobId:'job-one'} as never}
  return {} as never
 })
}
const authoritySubmit=()=>screen.getByRole('button',{name:'Submit run'}) as HTMLButtonElement
it.each([false,true])('holds submission while authority status is delayed or failed (cached disabled: %s)',async cached=>{
 let rejectStatus!:(error:Error)=>void
 vi.stubGlobal('fetch',()=>new Promise<Response>((_,reject)=>{rejectStatus=reject}))
 authorityRunData()
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 if(cached)client.setQueryData(['policy-execution'],{enabled:false})
 authorityRunPage(client)
 await screen.findByRole('button',{name:'Submit run'})
 expect(authoritySubmit().disabled).toBe(true)
 fireEvent.click(authoritySubmit())
 expect(vi.mocked(jobsAPI).mock.calls.some(([path,body])=>path==='jobs/job-one/runs'&&body!==undefined)).toBe(false)
 await act(async()=>rejectStatus(Error('authority unavailable')))
 await waitFor(()=>expect(client.getQueryState(['policy-execution'])?.status).toBe('error'))
 expect(authoritySubmit().disabled).toBe(true)
 fireEvent.click(authoritySubmit())
 expect(vi.mocked(jobsAPI).mock.calls.some(([path,body])=>path==='jobs/job-one/runs'&&body!==undefined)).toBe(false)
})
it.each([200,404])('allows legacy submit only after verified disabled authority status (%s)',async status=>{
 vi.stubGlobal('fetch',async()=>new Response(status===200?JSON.stringify({version:1,enabled:false}):'Not found',{status}))
 authorityRunData()
 authorityRunPage()
 await waitFor(()=>expect(authoritySubmit().disabled).toBe(false))
 fireEvent.click(authoritySubmit())
 await waitFor(()=>expect(jobsAPI).toHaveBeenCalledWith('jobs/job-one/runs',{facts:{}},expect.any(String)))
})
it('retains the enabled authority submission key before send and holds it across refresh after a lost reply',async()=>{
 vi.stubGlobal('crypto',webcrypto)
 for(const key of Object.keys(sessionStorage))if(key.startsWith('jpack.manual-intent.v1:'))sessionStorage.removeItem(key)
 vi.stubGlobal('fetch',async(url:unknown)=>new Response(JSON.stringify(String(url).includes('/policy-registration')?{version:1,state:'registered',eligible:true,reason:'eligible',resource:{jobId:'job-one',jobRevision:'1',releaseId:'release'}}:{version:1,enabled:true,profile:'local-admin-manual-pack/1'}),{status:200}))
 let originalKey:string|undefined,sends=0
 authorityRunData(key=>{
  originalKey=key;sends++
  expect(Object.keys(sessionStorage).filter(k=>k.startsWith('jpack.manual-intent.v1:')).some(k=>JSON.parse(sessionStorage.getItem(k)!).key===key)).toBe(true)
  throw Error('The actual submission reply was lost.')
 })
 authorityRunPage()
 await waitFor(()=>expect(authoritySubmit().disabled).toBe(false))
 fireEvent.click(authoritySubmit())
 await screen.findByText('The actual submission reply was lost.')
 expect(sends).toBe(1);expect(originalKey).toBeTruthy();expect(authoritySubmit().disabled).toBe(true)
 cleanup();authorityRunPage()
 await screen.findByText(originalKey!)
 expect(authoritySubmit().disabled).toBe(true)
 fireEvent.click(authoritySubmit());expect(sends).toBe(1)
 expect(screen.getByRole('button',{name:'Choose a new intent'})).toBeTruthy()
 for(const key of Object.keys(sessionStorage))if(key.startsWith('jpack.manual-intent.v1:'))sessionStorage.removeItem(key)
})

it('holds submission during paused authority refresh with cached disabled status',async()=>{
 vi.stubGlobal('fetch',async()=>new Response(JSON.stringify({version:1,enabled:false}),{status:200}))
 authorityRunData()
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 client.setQueryData(['policy-execution'],{enabled:false})
 client.setQueryData(['job','job-one'],{job:{id:'job-one',name:'Intake'},release:{...release,title:'Policy'}})
 onlineManager.setOnline(false)
 try{
  authorityRunPage(client)
  await screen.findByRole('button',{name:'Submit run'})
  await waitFor(()=>expect(client.getQueryState(['policy-execution'])?.fetchStatus).toBe('paused'))
  expect(authoritySubmit().disabled).toBe(true)
  fireEvent.click(authoritySubmit())
  expect(vi.mocked(jobsAPI).mock.calls.some(([path,body])=>path==='jobs/job-one/runs'&&body!==undefined)).toBe(false)
 }finally{cleanup();onlineManager.setOnline(true)}
})
