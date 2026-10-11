import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { Pagination } from './Pagination'
import { usePagination } from './usePagination'

afterEach(cleanup)
function List({ total, scope = 'all', complete = true }: { total: number; scope?: string; complete?: boolean }) {
  const pagination = usePagination(total, scope)
  return <><output aria-label="First item">{pagination.start + 1}</output><Pagination pagination={pagination} complete={complete}/></>
}

it('changes page size through the shared menu and keeps the preference across queries', async () => {
  const { rerender } = render(<List total={127}/>)
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
  expect(screen.getByLabelText('First item').textContent).toBe('51')
  fireEvent.keyDown(screen.getByRole('button', { name: 'Items per page' }), { key: 'Enter' })
  fireEvent.click(await screen.findByRole('menuitemradio', { name: '25 per page' }))
  expect(screen.getByText('1–25 of 127 items')).toBeTruthy()
  expect(screen.getByText('Page 1 of 6')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
  rerender(<List total={127} scope="another folder"/>)
  expect(screen.getByText('Page 1 of 6')).toBeTruthy()
  expect(screen.getByLabelText('First item').textContent).toBe('1')
})

it('clamps after items disappear and does not resurrect an obsolete page on refresh', () => {
  const { rerender } = render(<List total={101}/>)
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
  expect(screen.getByText('Page 3 of 3')).toBeTruthy()
  rerender(<List total={99}/>)
  expect(screen.getByText('Page 2 of 2')).toBeTruthy()
  expect(screen.getByLabelText('First item').textContent).toBe('51')
  rerender(<List total={101}/>)
  expect(screen.getByText('Page 2 of 3')).toBeTruthy()
  rerender(<List total={0}/>)
  expect(screen.getByText('0 items')).toBeTruthy()
  expect(screen.queryByRole('navigation')).toBeNull()
  rerender(<List total={101}/>)
  expect(screen.getByText('Page 1 of 3')).toBeTruthy()
})

it('identifies a partial inventory instead of claiming a complete total', () => {
  render(<List total={3} complete={false}/>)
  expect(screen.getByText('3 loaded items')).toBeTruthy()
  expect(screen.queryByText('3 items')).toBeNull()
})
