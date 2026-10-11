import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { fittingTabs, TabOverflowMenu } from './TabOverflow'

afterEach(cleanup)
const items = ['Overview','Logic','Sources','Tests','Review','Brief','History','Usage'].map(value => ({value,label:value}))
it('keeps all sections when they fit and retains the active section on narrow panes', () => {
  expect([...fittingTabs(items,'History',{available:1000,widths:items.map(()=>60),more:70,gap:20})]).toEqual(items.map(item=>item.value))
  const compact = fittingTabs(items,'History',{available:320,widths:items.map(()=>60),more:70,gap:20})
  expect(items.filter(item=>compact.has(item.value)).map(item=>item.value)).toEqual(['Overview','Logic','History'])
  expect([...fittingTabs(items,'Usage',{available:80,widths:items.map(()=>60),more:70,gap:20})]).toEqual(['Usage'])
})
it('offers every overflowed section as a keyboard-accessible menu command', async () => {
  const select=vi.fn()
  render(<TabOverflowMenu items={items.slice(5)} onSelect={select}/> )
  fireEvent.keyDown(screen.getByRole('button',{name:'More'}),{key:'Enter'})
  expect((await screen.findAllByRole('menuitem')).map(item=>item.textContent)).toEqual(['Brief','History','Usage'])
  fireEvent.click(screen.getByRole('menuitem',{name:'History'}))
  expect(select).toHaveBeenCalledWith('History')
})
