import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import fixtures from '../dialect/testdata/authoring-parity.json'
import {deskFetch,readFile} from '../files/client'
import {canonicalize} from '../dialect/canon'
import {dialectChatTools} from './dialectChatTools'
import {buildDialect,type DialectProposal} from './dialect'
import {packBinding} from './qualification'
import {ConversionPreview} from './ConversionPreview'
import {dialectAPI,type PreparationReply} from './client'
import type {DialectChatAction} from './chatActions'
vi.mock('../files/client',async original=>({...await original<object>(),deskFetch:vi.fn(),readFile:vi.fn()}))
vi.mock('./client',async original=>({...await original<object>(),dialectAPI:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})

for(const fixture of fixtures)it(`manual/chat non-GTM authoring parity: ${fixture.name}`,async()=>{
 const id='prep_'+'a'.repeat(32),revision='b'.repeat(64)
 const saved:PreparationReply={sha256:revision,document:{version:1,id,name:'Work item governance',recipeVersion:1,recipe:{intent:'Triage work items'},createdAt:'2026-10-16',updatedAt:'2026-10-16',imports:[],activity:[],records:[{id:'row_'+'c'.repeat(64),name:'Review retention policy',sourceId:'original',row:1,class:'asserted',status:'provided',values:{Priority:'high',Policy:'present'}}]}}
 const pack=await packBinding('governance',fixture.pack)
 const proposal:DialectProposal={packKey:pack.key,packDigest:pack.digest,facts:fixture.name==='omission'?[]:[{target:'/workItem/priority',column:'Priority',conversion:{kind:'text'}}],evidence:[{requirement:'policy',column:'Policy'}],summary:'Map declared work item priority and policy availability.'}
 const value=buildDialect(saved,proposal,pack)
 expect(value.mapping).toEqual(fixture.mapping)
 vi.mocked(readFile).mockImplementation(async path=>({content:path==='jpack.json'?JSON.stringify({packs:{governance:{path:'governance.json'}}}):fixture.pack}) as any)
 vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>method==='POST'?{rows:[]} as any:structuredClone(saved))
 vi.mocked(deskFetch).mockImplementation(async()=>new Response(JSON.stringify(fixture.report),{status:200,headers:{'Content-Type':'application/json'}}))
 const checked=vi.fn()
 render(<ConversionPreview saved={saved} value={value} disabled={false} expanded onChecked={checked}/> )
 fireEvent.click(screen.getByRole('button',{name:'Preview conversions'}))
 await waitFor(()=>expect((screen.getByRole('button',{name:'Preview conversions'}) as HTMLButtonElement).disabled).toBe(false))
 const manual=JSON.parse(String(vi.mocked(deskFetch).mock.calls[0]![1]!.body))
 expect(manual).toEqual({document:canonicalize(fixture.mapping),pack:fixture.pack})
 const actions:DialectChatAction[]=[]
 const tools=dialectChatTools({afterTurnId:()=> 'turn',retainAction:async action=>{actions.push(structuredClone(action))}})
 const signal=new AbortController().signal
 await tools.find(t=>t.name==='read_dialect')!.execute({id,packKey:pack.key},signal)
 const result=tools.find(t=>t.name==='propose_dialect')!.execute({id,revision,...proposal},signal)
 if(fixture.name==='refused'){
  await expect(result).rejects.toThrow()
  expect(checked).not.toHaveBeenCalled()
  for(const finding of fixture.report.diagnostics)expect(screen.getByText(new RegExp(finding.code))).toBeTruthy()
 }else{
  await result;expect(checked).toHaveBeenCalledOnce()
 }
 expect(JSON.parse(String(vi.mocked(deskFetch).mock.calls[1]![1]!.body))).toEqual(manual)
 expect(actions.at(-1)?.dialectCheck?.report).toEqual(fixture.report)
 expect(actions.at(-1)?.dialectCheck?.document).toBe(manual.document)
 expect(actions.every(action=>!action.applied)).toBe(true)
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method).every(([path])=>path?.endsWith('/conversion-preview'))).toBe(true)
})
