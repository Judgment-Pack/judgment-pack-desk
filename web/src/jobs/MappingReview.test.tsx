import {cleanup, fireEvent, render, screen, within} from '@testing-library/react'
import {afterEach, expect, it} from 'vitest'
import {languageReady, setLanguage} from '../i18n'
import {MappingReview, InputLineage} from './MappingReview'
import type {InputProfile, MappingV2, Preparation} from './mappingTypes'

const profile: InputProfile = {id:'fx',publicKey:'key',class:'record',source:'finance',authority:'gateway',shape:'mcp',adapter:{name:'mcp',version:'1',digest:'digest'},endpoint:null,calculator:{name:'fx-convert',version:'2.1.0'}}
const mapping: MappingV2 = {version:2,case:{facts:[],evidence:[]},sources:[{name:'conversion',kind:'operation',profile:'fx',read:{copy:{facts:[],evidence:[]}},calculation:{inputs:{amount:'invoiceAmount',currency:'invoiceCurrency'},tables:{'ecb-rates':86400,'spot-rates':3601}}}]}
const line: Preparation['lineage'][number] = {target:'/converted',kind:'fact',source:'conversion',class:'record',generatedInfluence:false,present:true,status:'resolved',reason:'',calculation:{calculator:{name:'fx-convert',version:'2.1.0'},status:'computed',inputs:[{name:'amount',parameter:'invoiceAmount',source:'case',pointer:'/invoice/amount'},{name:'currency',parameter:'invoiceCurrency',source:'invoice-record',pointer:'/currency'}],asOf:{'ecb-rates':'2026-10-01T14:00:00Z','spot-rates':'2026-10-01T13:00:00Z'}}}
const preparation = (...lineage: Preparation['lineage']): Preparation => ({version:2,verifiedAt:'2026-10-01T14:01:00Z',mappingDigest:'digest',verification:'verified',lineage,cites:[],outcomes:[]})
afterEach(async()=>{cleanup();setLanguage('en');await languageReady()})

it('names the pinned calculator and every binding and exact table limit in the mapping review',()=>{
 render(<MappingReview mapping={mapping} profiles={[profile]}/>)
 const row=within(screen.getByRole('row',{name:/conversion/}))
 expect(row.getByText('Calculator: fx-convert · 2.1.0')).toBeTruthy()
 expect(row.getByText('amount ← invoiceAmount')).toBeTruthy()
 expect(row.getByText('currency ← invoiceCurrency')).toBeTruthy()
 expect(row.getByText('ecb-rates · maximum age: 1 day')).toBeTruthy()
 expect(row.getByText('spot-rates · maximum age: 1 hour, 1 second')).toBeTruthy()
 expect(row.getByText('record')).toBeTruthy()
})
it('leaves ordinary source rows unchanged and never infers a calculator from caller bindings',()=>{
 const {calculator:_,...ordinary}=profile
 render(<MappingReview mapping={{...mapping,sources:[...mapping.sources!,{name:'local',kind:'selected-file',provider:'local-file',read:{copy:{facts:[],evidence:[]}}},{name:'drive',kind:'selected-file',provider:'google-drive',read:{copy:{facts:[],evidence:[]}}}]}} profiles={[ordinary]}/>)
 expect(within(screen.getByRole('row',{name:'conversion fx record'})).getAllByRole('cell').map(c=>c.textContent)).toEqual(['conversion','fx','record'])
 expect(screen.getByRole('row',{name:'local Local JSON file asserted'})).toBeTruthy()
 expect(screen.getByRole('row',{name:'drive Google Drive —'})).toBeTruthy()
 expect(screen.getByRole('row',{name:'Case inputs Manual / API asserted'})).toBeTruthy()
 expect(within(screen.getByRole('table')).queryByText(/Calculator:|maximum age:/)).toBeNull()
})
it('reads mappings without calculation bindings or installed profiles',()=>{
 const {calculation:_,...source}=mapping.sources![0]!
 const ui=render(<MappingReview mapping={{version:2,sources:[source]}} profiles={[profile]}/>)
 expect(screen.getByText('Calculator: fx-convert · 2.1.0')).toBeTruthy()
 ui.rerender(<MappingReview mapping={mapping}/>)
 expect(screen.getByRole('row',{name:'conversion fx —'})).toBeTruthy()
})
it.each([
 ['computed','Calculated',''],
 ['input-missing','Not calculated: an input was missing','calculation-input-missing'],
 ['cannot-compute','Not calculated: the calculator could not compute it','calculation-cannot-compute'],
] as const)('shows %s in words instead of the raw calculation reason', (status,label,reason)=>{
 render(<InputLineage preparation={preparation({...line,present:status==='computed',status:status==='computed'?'resolved':'unknown',reason,calculation:{...line.calculation!,status}})}/>)
 const table=within(screen.getByRole('table'))
 expect(table.getByText(label)).toBeTruthy()
 expect(table.getByText(status==='computed'?'Supplied':'Not supplied')).toBeTruthy()
 if(reason)expect(table.queryByText(reason)).toBeNull()
})
it('shows the calculator and each echoed input with its parameter, case or earlier fact origin',()=>{
 render(<InputLineage preparation={preparation(line)}/>)
 const table=within(screen.getByRole('table'))
 expect(table.getByText('Calculator: fx-convert · 2.1.0')).toBeTruthy()
 expect(table.getByText(/amount ← invoiceAmount · Case inputs ·/).textContent).toContain('/invoice/amount')
 expect(table.getByText(/currency ← invoiceCurrency · Source invoice-record ·/).textContent).toContain('/currency')
})
it('localizes table as-of dates and duration words without translating calculator identifiers',async()=>{
 setLanguage('de');await languageReady()
 render(<><MappingReview mapping={mapping} profiles={[profile]}/><InputLineage preparation={preparation(line)}/></>)
 expect(screen.getByText('ecb-rates · Höchstalter: 1 Tag')).toBeTruthy()
 for(const [table,time] of Object.entries(line.calculation!.asOf)){
  const date=new Intl.DateTimeFormat('de',{dateStyle:'medium',timeStyle:'medium'}).format(new Date(time))
  expect(screen.getByText(`${table} · Stand: ${date}`)).toBeTruthy()
 }
 expect(screen.getAllByText('Rechner: fx-convert · 2.1.0')).toHaveLength(2)
})
it.each(['ordinary','dependency-unavailable'] as const)('leaves %s lineage without calculation details',reason=>{
 const {calculation:_,...ordinary}=line
 render(<InputLineage preparation={preparation({...ordinary,present:false,reason})}/>)
 const row=screen.getByRole('row',{name:new RegExp(reason)})
 expect(within(row).getAllByRole('cell').map(c=>c.textContent)).toEqual(['/converted','conversionrecord',`Not supplied${reason}`])
 expect(within(row).queryByText(/Calculator:|Calculated|as of/)).toBeNull()
})
it('retains the unmodified calculation record in technical details',()=>{
 const value=preparation(line)
 render(<InputLineage preparation={value}/>)
 fireEvent.click(screen.getByText('Technical details'))
 expect(JSON.parse(document.querySelector('pre')!.textContent!)).toEqual(value)
})
