import { createRef } from 'react'
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { GmailPicker } from './GmailPicker'
const calls = vi.hoisted(() => ({ call: vi.fn(), authorize: vi.fn() }))
vi.mock('./client', () => ({ connectionCall: calls.call, authorizeDrive: calls.authorize, CONNECTIONS_KEY: ['gateway-connections'] }))
afterEach(() => { cleanup(); vi.clearAllMocks() })
const messages = Array.from({length: 5}, (_,i) => ({id:`abc${i}`,subject:`Subject ${i}`,from:'person@example.test',date:'2026-09-18'}))
function view(open = true, onSelect = vi.fn(), accountId = 'account-a') {
  return <QueryClientProvider client={testQueryClient()}><GmailPicker open={open} onOpenChange={vi.fn()} state="connected" accountId={accountId} openerRef={createRef()} onSelect={onSelect} /></QueryClientProvider>
}
it('keeps search metadata out of chat and attaches only checked emails', async () => {
  calls.call.mockImplementation(async method => method === 'search' ? {messages,selectionContext:'a'.repeat(64)} : [{messageId:'abc1',grant:'aa'.repeat(32)}])
  const selected = vi.fn(); render(view(true, selected))
  const rows = await screen.findAllByRole('checkbox')
  expect(selected).not.toHaveBeenCalled()
  expect((screen.getByRole('button',{name:'Attach selected emails'}) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(rows[1]!)
  fireEvent.click(screen.getByRole('button',{name:'Attach selected emails'}))
  await waitFor(() => expect(selected).toHaveBeenCalledWith([{messageId:'abc1',grant:'aa'.repeat(32)}]))
  expect(calls.call).toHaveBeenCalledWith('select',{messageIds:['abc1'],selectionContext:'a'.repeat(64)},expect.any(AbortSignal),'gmail')
})
it('limits selection to four and allows deselection', async () => {
  calls.call.mockResolvedValue({messages,selectionContext:'a'.repeat(64)}); render(view())
  const rows = await screen.findAllByRole('checkbox') as HTMLInputElement[]
  rows.slice(0,4).forEach(row => fireEvent.click(row))
  expect(rows[4]!.disabled).toBe(true)
  fireEvent.click(rows[0]!)
  expect(rows[4]!.disabled).toBe(false)
})
it('cancels a pending selection when the picker closes', async () => {
  let complete!: (value: unknown) => void
  calls.call.mockImplementation(method => method === 'search' ? Promise.resolve({messages,selectionContext:'a'.repeat(64)}) : new Promise(resolve => {complete=resolve}))
  const selected=vi.fn(), ui=render(view(true,selected))
  fireEvent.click((await screen.findAllByRole('checkbox'))[0]!)
  fireEvent.click(screen.getByRole('button',{name:'Attach selected emails'}))
  const signal=calls.call.mock.calls.find(call=>call[0]==='select')![2] as AbortSignal
  ui.rerender(view(false,selected))
  expect(signal.aborted).toBe(true)
  complete([{messageId:'abc0',grant:'aa'.repeat(32)}])
  await Promise.resolve()
  expect(selected).not.toHaveBeenCalled()
})
it('uses the submitted query with its pagination token after the input changes', async () => {
  calls.call.mockResolvedValue({messages,selectionContext:'a'.repeat(64),nextPageToken:'page-two'}); render(view())
  await screen.findAllByRole('checkbox')
  fireEvent.change(screen.getByRole('textbox',{name:'Search Gmail…'}),{target:{value:'different query'}})
  fireEvent.click(screen.getByRole('button',{name:'Next page'}))
  await waitFor(()=>expect(calls.call).toHaveBeenLastCalledWith('search',{query:'',pageToken:'page-two'},expect.any(AbortSignal),'gmail'))
})

it('clears old results and cancels pending selection when connected account changes', async () => {
 let complete!: (value: unknown) => void
 calls.call.mockImplementation(method => method === 'search' ? Promise.resolve({messages,selectionContext:'a'.repeat(64)}) : new Promise(resolve=>{complete=resolve}))
 const selected=vi.fn(), ui=render(view(true,selected))
 fireEvent.click((await screen.findAllByRole('checkbox'))[0]!)
 fireEvent.click(screen.getByRole('button',{name:'Attach selected emails'}))
 const signal=calls.call.mock.calls.find(call=>call[0]==='select')![2] as AbortSignal
 calls.call.mockImplementation(()=>new Promise(()=>{}))
 ui.rerender(view(true,selected,'account-b'))
 expect(signal.aborted).toBe(true)
 expect(screen.queryAllByRole('checkbox')).toHaveLength(0)
 complete([{messageId:'abc0',grant:'aa'.repeat(32)}]); await Promise.resolve()
 expect(selected).not.toHaveBeenCalled()
})
