import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it } from 'vitest'
import { DialectActivity } from './DialectActivity'
import type { DialectEvaluation, Preparation } from './client'

afterEach(cleanup)
const document: Preparation = {
  version: 1, id: 'suppliers', name: 'Suppliers', recipe: { intent: '' }, recipeVersion: 1,
  createdAt: '2026-01-01', updatedAt: '2026-01-01', records: [], imports: [],
  activity: Array.from({ length: 51 }, (_, index) => ({ kind: 'imported', at: new Date(Date.UTC(2026, 0, index + 1)).toISOString(), count: index + 1, recipeVersion: 1 }))
}
const batch = { id: 'evaluation', confirmedAt: '2026-04-01T00:00:00Z', recipeVersion: 2, jobId: 'job', rows: [{recordId:'one'}, {recordId:'two'}] } as DialectEvaluation
it('pages preparation and evaluation activity together, newest first, and preserves job links', () => {
  const { rerender } = render(<MemoryRouter><DialectActivity document={document} batches={[batch]}/></MemoryRouter>)
  const table = screen.getByRole('table', { name: 'Activity' })
  expect(within(table).getAllByRole('row')).toHaveLength(51)
  expect(within(table).getAllByRole('row')[1]!.textContent).toContain('Evaluation · 2 records')
  expect(screen.getByRole('link', { name: 'View job' }).getAttribute('href')).toBe('/jobs/job')
  expect(screen.getByText('1–50 of 52 items')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
  expect(screen.getByText('51–52 of 52 items')).toBeTruthy()
  expect(within(table).getAllByRole('row')).toHaveLength(3)
  expect(within(table).getAllByRole('row')[2]!.textContent).toContain('Records imported: 1')
  rerender(<MemoryRouter><DialectActivity document={{...document, activity: document.activity.slice(0,1)}} batches={[]}/></MemoryRouter>)
  expect(screen.getByText('1 item')).toBeTruthy()
  expect(screen.queryByRole('navigation', { name: 'Pagination' })).toBeNull()
  expect(table.getAttribute('aria-describedby')).toBe(screen.getByText('Local preparation activity. This is not a signed audit trail.').id)
  expect(screen.queryByRole('alert')).toBeNull()
})
it('lists research runs alongside preparation history and opens their steps',()=>{
 let opened=''
 render(<MemoryRouter><DialectActivity document={{...document,activity:[]}} batches={[]} research={[{id:'r1',state:'interrupted',createdAt:'2026-04-02',updatedAt:'2026-04-02',recipeVersion:2,recordCount:0,stopRequested:false},{id:'r2',state:'review',updatedAt:'2026-04-03',recordCount:0,stopRequested:false}]} onResearch={id=>{opened=id}}/></MemoryRouter>)
 expect(screen.getByText('1 item')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Research interrupted'}));expect(opened).toBe('r1')
})
