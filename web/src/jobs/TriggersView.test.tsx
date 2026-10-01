import {useState} from 'react'
import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {TriggerForm,defaultTrigger} from './TriggerForm'
import {TriggersView} from './TriggersView'
import {jobsAPI,type Release} from './client'
import type {Trigger,TriggerConfig} from './triggerTypes'
import type {PackDocument} from '../mcp/types'
import {listFiles} from '../files/client'
vi.mock('./client',()=>({jobsAPI:vi.fn()}))
vi.mock('../files/client',()=>({listFiles:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
function shell(children:React.ReactNode){return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Tooltip.Provider><MemoryRouter>{children}</MemoryRouter></Tooltip.Provider></QueryClientProvider>)}
const doc={} as PackDocument
function Form({config,onValid=()=>{}}:{config:TriggerConfig;onValid?:(v:boolean)=>void}){const [value,setValue]=useState(config);return <><TriggerForm doc={doc} value={value} onChange={setValue} onValid={onValid} disabled={false}/><output data-testid="config">{JSON.stringify(value)}</output></>}
it('configures a watched input file without reading project data until explicitly requested',async()=>{
 const config=defaultTrigger('file'),valid=vi.fn();shell(<Form config={config} onValid={valid}/>)
 expect(listFiles).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('Input file'),{target:{value:'intake.json'}})
 fireEvent.change(screen.getByLabelText('Watch file'),{target:{value:'intake.json'}})
 await waitFor(()=>expect(valid).toHaveBeenLastCalledWith(true))
 fireEvent.change(screen.getByLabelText('Watch file'),{target:{value:'different.json'}})
 await waitFor(()=>expect(valid).toHaveBeenLastCalledWith(false))
 expect(listFiles).not.toHaveBeenCalled()
})
it('preserves evidence while facts JSON is incomplete and blocks saving invalid input',async()=>{
 const config=defaultTrigger('schedule');config.input={kind:'constant',value:'{"facts":{"a":1},"evidence":{"proof":"present"}}'}
 const valid=vi.fn();shell(<Form config={config} onValid={valid}/>)
 fireEvent.change(screen.getByLabelText('Facts (JSON)'),{target:{value:'{'}})
 await waitFor(()=>expect(valid).toHaveBeenLastCalledWith(false))
 fireEvent.change(screen.getByLabelText('Facts (JSON)'),{target:{value:'{"a":2}'}})
 await waitFor(()=>expect(valid).toHaveBeenLastCalledWith(true))
 const saved=JSON.parse(screen.getByTestId('config').textContent!);expect(JSON.parse(saved.input.value)).toEqual({facts:{a:2},evidence:{proof:'present'}})
})
it('defaults case-only mappings to fresh file inputs and v1 file mappings to fresh file bindings',()=>{
 expect(defaultTrigger('schedule',{version:2,case:{facts:[],evidence:[]},sources:[]}).input?.kind).toBe('mapped-files')
 expect(defaultTrigger('schedule',{version:1,provider:'local-file',facts:[],evidence:[]}).input).toEqual({kind:'mapped-files',files:{file:''}})
})
const release={pack:'{}',id:'rel',packVersion:'1'} as Release
const trigger:Trigger={id:'trg_test',jobId:'job_test',revision:1,authority:'local',paused:true,createdAt:'2026-09-26T10:00:00Z',updatedAt:'2026-09-26T10:00:00Z',hasKey:false,config:defaultTrigger('event')}
it('requires review before enabling and displays a scoped credential only after success',async()=>{
 let enabled=false
 vi.mocked(jobsAPI).mockImplementation(async(path,body)=>{
  if(path==='jobs/job_test/triggers')return {items:[{...trigger,paused:!enabled,revision:enabled?2:1}],localFiles:true} as never
  if(path.startsWith('jobs/job_test/occurrences'))return {items:[],next:0} as never
  if(path==='triggers/trg_test/state'){expect(body).toEqual({revision:1,paused:false,reviewed:true});enabled=true;return {trigger:{...trigger,revision:2},token:'a'.repeat(64)} as never}
  throw Error(path)
 })
 shell(<TriggersView jobId="job_test" release={release}/>)
 fireEvent.click(await screen.findByRole('button',{name:'Enable'}))
 const dialog=await screen.findByRole('dialog',{name:'Enable trigger'}),confirm=within(dialog).getByRole('button',{name:'Enable'}) as HTMLButtonElement
 expect(confirm.disabled).toBe(true)
 expect(jobsAPI).not.toHaveBeenCalledWith('triggers/trg_test/state',expect.anything())
 fireEvent.click(within(dialog).getByLabelText('I reviewed this trigger and its execution policy.'));fireEvent.click(confirm)
 const tokenDialog=await screen.findByRole('dialog',{name:'Event token'})
 expect((within(tokenDialog).getByLabelText('Event token') as HTMLInputElement).value).toBe('a'.repeat(64))
 fireEvent.click(within(tokenDialog).getByRole('button',{name:'Done'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(screen.queryByDisplayValue('a'.repeat(64))).toBeNull()
})
it('names the occurrence read beside the delivery route in the event endpoint',async()=>{
 vi.mocked(jobsAPI).mockImplementation(async path=>{
  if(path==='jobs/job_test/triggers')return {items:[{...trigger,paused:false,hasKey:true}],localFiles:true} as never
  if(path.startsWith('jobs/job_test/occurrences'))return {items:[],next:0} as never
  throw Error(path)
 })
 shell(<TriggersView jobId="job_test" release={release}/>)
 const endpoint=(await screen.findByText('Event endpoint')).closest('details')!
 expect([...endpoint.querySelectorAll('pre')].map(pre=>pre.textContent)).toEqual([`${location.origin}/api/job-events/trg_test`,`GET ${location.origin}/api/job-events/trg_test/occurrences/occ_<id>`])
 expect(endpoint.textContent).toContain("The token that delivered an event can read that occurrence's result here: send a GET with the same Authorization header")
})
it('cannot enable a schedule whose fresh input preview fails',async()=>{
 vi.mocked(jobsAPI).mockImplementation(async path=>{
  if(path==='jobs/job_test/triggers')return {items:[{...trigger,config:defaultTrigger('schedule')}]} as never
  if(path.includes('occurrences'))return {items:[]} as never
  if(path.endsWith('/preview'))throw Error('Input file is unavailable')
  throw Error(path)
 })
 shell(<TriggersView jobId="job_test" release={release}/>)
 fireEvent.click(await screen.findByRole('button',{name:'Enable'}));await screen.findByText('Input file is unavailable')
 const dialog=screen.getByRole('dialog');fireEvent.click(within(dialog).getByLabelText('I reviewed this trigger and its execution policy.'))
 expect((within(dialog).getByRole('button',{name:'Enable'}) as HTMLButtonElement).disabled).toBe(true)
})
it('blocks cloud setup until an installed subscription is selected and a job is identified',async()=>{
 vi.mocked(jobsAPI).mockResolvedValue({cloudConnections:[{id:'google',subscription:'projects/example-project/subscriptions/desk-inbox'}],gatewayProfiles:[]} as never)
 const valid=vi.fn();shell(<Form config={defaultTrigger('cloud')} onValid={valid}/>)
 await screen.findByRole('combobox',{name:'Cloud connection'})
 expect(valid).toHaveBeenLastCalledWith(false)
 fireEvent.click(screen.getByRole('combobox',{name:'Cloud connection'}));fireEvent.click(await screen.findByRole('option',{name:'google'}))
 fireEvent.change(screen.getByLabelText('Scheduler job name'),{target:{value:'projects/example-project/locations/us-central1/jobs/daily'}})
 fireEvent.change(screen.getByLabelText('Input file'),{target:{value:'cases/current.json'}})
 await waitFor(()=>expect(valid).toHaveBeenLastCalledWith(true))
 const c=JSON.parse(screen.getByTestId('config').textContent!)
 expect(c.cloud).toEqual({connection:'google',subscription:'projects/example-project/subscriptions/desk-inbox',job:'projects/example-project/locations/us-central1/jobs/daily'})
 expect(c.schedule).toBeUndefined()
 expect(screen.queryByLabelText('Time zone')).toBeNull()
})
it('shows the installation requirement without offering a false cloud connection',async()=>{
 vi.mocked(jobsAPI).mockResolvedValue({cloudConnections:[],gatewayProfiles:[]} as never)
 const valid=vi.fn();shell(<Form config={defaultTrigger('cloud')} onValid={valid}/>)
 await screen.findByText('No cloud connection is installed. The installation owner must configure a dedicated Pub/Sub subscription and local Google credentials.')
 expect(valid).toHaveBeenLastCalledWith(false)
 expect(screen.queryByRole('combobox',{name:'Cloud connection'})).toBeNull()
})
it('does not reuse interactive grants and only binds local files in automatic operation mappings',()=>{
 const mapping={version:2,case:{facts:[],evidence:[]},sources:[{name:'registry',kind:'operation',profile:'registry',read:{copy:{facts:[],evidence:[]}}},{name:'caseFile',kind:'selected-file',provider:'local-file',read:{copy:{facts:[],evidence:[]}}}]} as const
 const value=defaultTrigger('cloud',mapping as never)
 expect(value.input).toEqual({kind:'mapped-sources',case:{},files:{caseFile:''}})
})
