import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { DraftSources } from './DraftSources'
import { TestsPanel } from './DraftPanels'
import { INITIAL_STATE } from '../run'

vi.mock('../../documents/SourceReader',()=>({SourceReader:(props:unknown)=><output aria-label="Document reader inputs">{JSON.stringify(props)}</output>}))
afterEach(cleanup)

it('lists declared references and retained inputs without claiming receipt verification', () => {
  const onRead = vi.fn()
  render(<DraftSources document={{ sources: [{ id: 'policy', title: 'Policy guide', locator: { value: 'https://example.org/policy' } }] }} files={[{ name: 'notes.txt', text: 'Notes' }]} onRead={onRead} />)
  expect(screen.getByText('Policy guide')).toBeTruthy()
  expect(screen.getByText('notes.txt')).toBeTruthy()
  expect(screen.queryByText('receipt verified')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: /Policy guide/ }))
  expect(onRead).not.toHaveBeenCalled()
  expect(screen.getByRole('region',{name:'Policy guide'}).textContent).toContain('https://example.org/policy')
  expect(screen.getByText('Declared only')).toBeTruthy()
})
it('shows a saved structure check without inventing behavioral cases', () => {
  const digest = 'a'.repeat(64)
  render(<TestsPanel mode="draft" state={{ ...INITIAL_STATE, restored: true, candidates: [{ revision: 1, text: '{}', document: {}, digest, producedBy: 'conversation', previousCheck: { documentDigest: digest, valid: true, diagnostics: [], cases: [] } }] }} onSelect={vi.fn()} />)
  expect(screen.getByRole('heading', { name: 'Structure check' })).toBeTruthy()
  expect(screen.getByText('Valid')).toBeTruthy()
  expect(screen.getByText('Saved result. Recheck the draft before creating it.')).toBeTruthy()
  expect(screen.queryByRole('table')).toBeNull()
})

it('shows where a reference is used without claiming an attached document was retrieved',()=>{
 render(<DraftSources document={{sources:[{id:'policy',title:'Policy'}],rules:[{id:'eligibility',sourceRefs:['policy']}]}} files={[{name:'Notes',text:'local notes'}]}/>)
 expect(screen.getByText('eligibility')).toBeTruthy();expect(screen.getByText('Attached')).toBeTruthy()
 fireEvent.change(screen.getByRole('searchbox',{name:'Search sources'}),{target:{value:'eligibility'}})
 expect(screen.getByRole('button',{name:'Policy'})).toBeTruthy();expect(screen.queryByRole('button',{name:'Notes'})).toBeNull()
})

it('opens the exact cited document revision and quote below the source list',()=>{
 const reference={id:'12345678-1234-1234-1234-000000000001',digest:'sha256:'+'a'.repeat(64),pages:[1,2],allowPartial:false}
 const citation={sourceId:'policy',location:`attachment:${reference.id}/${reference.digest}/page/2`,excerptId:null,url:'',traced:true,reason:'',quote:'Exact quoted words.'}
 render(<DraftSources document={{sources:[{id:'policy',title:'Policy'}]}} documents={[{id:'12345678-1234-1234-1234-000000000001',name:'Policy file',text:'',document:reference}]} citations={[citation]}/>)
 fireEvent.click(screen.getByRole('button',{name:'Policy'}))
 fireEvent.click(screen.getByRole('button',{name:'Page 2'}))
 const props=JSON.parse(screen.getByLabelText('Document reader inputs').textContent!)
 expect(props.reference).toEqual(reference)
 expect(props.citation).toEqual({page:2,quote:'Exact quoted words.'})
 fireEvent.click(screen.getByRole('button',{name:'Back to source details'}))
 expect(screen.queryByLabelText('Document reader inputs')).toBeNull()
})
