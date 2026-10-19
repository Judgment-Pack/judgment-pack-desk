import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import type {DialectTemplate,TemplateOrigin} from './templates'
import {ExchangeResearchTemplate} from './ExchangeResearchTemplate'
const mocks=vi.hoisted(()=>({list:vi.fn(),read:vi.fn()}))
vi.mock('../exchange/client',()=>({exchangeImportsQuery:()=>({queryKey:['imports'],queryFn:mocks.list}),exchangeImportQuery:(id:string)=>({queryKey:['imports',id],queryFn:()=>mocks.read(id)})}))
vi.mock('../shell/QueryStatusNotice',()=>({QueryStatusNotice:()=>null}))
vi.mock('../ui/Select',()=>({Select:({options,onValueChange,...props}:any)=><select {...props} onChange={e=>onValueChange(e.target.value)}><option value=""/>{options.map((o:any)=><option key={o.value} value={o.value}>{o.label}</option>)}</select>}))
let cache:QueryClient
const id='a'.repeat(64),second='b'.repeat(64),digest='sha256:'+'c'.repeat(64)
const item=(id:string)=>({id,entryId:'example/'+id,tag:'v1.0.0'})
const saved=(id:string)=>({import:item(id),candidate:{row:{id:item(id).entryId,entryDigest:digest,dialectDigest:digest},entry:{kind:'preset',title:'Research',licence:'Apache-2.0',description:'Research definition'},digest,document:JSON.stringify({version:3,case:{facts:[],evidence:[],parameters:{request:{pointer:'/request',type:'string'}}},sources:[{name:'records',kind:'research',question:{$param:'request'},constraints:{},evidence:[],fields:[{name:'name',type:'text'}],read:{copy:{facts:[],evidence:[]}}}]})}})
beforeEach(()=>{vi.resetAllMocks();cache=new QueryClient({defaultOptions:{queries:{retry:false}}});mocks.list.mockResolvedValue({items:[item(id),item(second)]});mocks.read.mockImplementation(async id=>saved(id))})
afterEach(()=>{cleanup();cache.clear()})
type Apply=(template:DialectTemplate,origin:TemplateOrigin)=>Promise<void>
const show=(apply:Apply)=>render(<MemoryRouter><QueryClientProvider client={cache}><ExchangeResearchTemplate purpose="Find records" disabled={false} onApply={apply}/></QueryClientProvider></MemoryRouter>)
it('only applies a template after an explicit action and suppresses duplicate writes',async()=>{
 let done!:()=>void;const apply=vi.fn<Apply>(()=>new Promise<void>(resolve=>done=resolve));show(apply)
 await screen.findByRole('option',{name:'example/'+id+' · v1.0.0'})
 fireEvent.change(screen.getByLabelText('Imported template'),{target:{value:id}})
 const button=await screen.findByRole('button',{name:'Use template'})
 expect(apply).not.toHaveBeenCalled();fireEvent.click(button);fireEvent.click(button)
 expect(apply).toHaveBeenCalledTimes(1)
 expect(apply.mock.calls[0][1].id).toBe('exchange-'+id)
 done();await waitFor(()=>expect((button as HTMLButtonElement).disabled).toBe(false))
})
it('does not offer a stale selected import while a different one loads',async()=>{
 let finish!: (value:any)=>void;mocks.read.mockImplementation(value=>value===second?new Promise(resolve=>finish=resolve):Promise.resolve(saved(value)))
 const apply=vi.fn<Apply>(async()=>{});show(apply)
 await screen.findByRole('option',{name:'example/'+id+' · v1.0.0'})
 fireEvent.change(screen.getByLabelText('Imported template'),{target:{value:id}})
 await screen.findByRole('button',{name:'Use template'})
 fireEvent.change(screen.getByLabelText('Imported template'),{target:{value:second}})
 expect(screen.queryByRole('button',{name:'Use template'})).toBeNull()
 finish(saved(second));const button=await screen.findByRole('button',{name:'Use template'});fireEvent.click(button)
 await waitFor(()=>expect(apply).toHaveBeenCalledTimes(1));expect(apply.mock.calls[0][1].id).toBe('exchange-'+second)
})
it('keeps the draft unchanged when the chosen definition cannot be copied safely',async()=>{
 const value=saved(id);value.candidate.entry.kind='dialect';mocks.read.mockResolvedValue(value)
 const apply=vi.fn<Apply>(async()=>{});show(apply)
 await screen.findByRole('option',{name:'example/'+id+' · v1.0.0'})
 fireEvent.change(screen.getByLabelText('Imported template'),{target:{value:id}})
 fireEvent.click(await screen.findByRole('button',{name:'Use template'}))
 expect(apply).not.toHaveBeenCalled()
})

it('does not feed prior generated instructions back into a replacement template',async()=>{
 const apply=vi.fn<Apply>(async()=>{})
 render(<MemoryRouter><QueryClientProvider client={cache}><ExchangeResearchTemplate current={{id:'exchange-'+second,name:'Prior',digest,license:'Apache-2.0'}} purpose='Old task with old fields and constraints' disabled={false} onApply={apply}/></QueryClientProvider></MemoryRouter>)
 await screen.findByRole('option',{name:'example/'+id+' · v1.0.0'})
 fireEvent.change(screen.getByLabelText('Imported template'),{target:{value:id}})
 const question=await screen.findByRole('textbox',{name:'What records do you need?'}) as HTMLTextAreaElement
 expect(question.value).toBe('')
 const button=screen.getByRole('button',{name:'Use template'}) as HTMLButtonElement
 expect(button.disabled).toBe(true)
 fireEvent.change(question,{target:{value:'Find procurement notices'}})
 fireEvent.click(button)
 await waitFor(()=>expect(apply).toHaveBeenCalledTimes(1))
 expect(apply.mock.calls[0][0].intent).toContain('Find procurement notices')
 expect(apply.mock.calls[0][0].intent).not.toContain('Old task')
})
