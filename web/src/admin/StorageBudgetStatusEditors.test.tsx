import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {ChatDataSettings} from './ChatDataSettings'
import {StorageBudgetStatus} from './StorageBudgetStatus'
import {storageBudgetStatusKey} from './useStorageBudgetStatus'
import {decodeStorageBudgetStatus, retainStorageSnapshot} from './storageBudgetStatus'
import {storageStatusFixture} from './storageBudgetStatus.fixture'
import type {ChatStorageStatus} from './chatStorage'
const mocks = vi.hoisted(() => ({fetch:vi.fn(), flush:vi.fn(async () => true), load:vi.fn()}))
vi.mock('../files/client', async original => ({...await original<typeof import('../files/client')>(), deskFetch:mocks.fetch}))
vi.mock('../chat/ChatProvider', () => ({useChats:() => ({store:{flush:mocks.flush, load:mocks.load}, saving:false, dirty:false, error:''})}))
const location: ChatStorageStatus = {path:'/private/current',recommendedPath:'/private/data',revision:'frozen-original',legacy:false,projectCount:1,bytes:5000,projectBytes:1000,scope:'personal',maxMoveBytes:1024 ** 3,maxBackupBytes:128 * 1024 ** 2}
const clients: QueryClient[] = []
function mount() {
  const client = new QueryClient({defaultOptions:{queries:{retry:false}}}); clients.push(client)
  client.setQueryData(storageBudgetStatusKey, decodeStorageBudgetStatus(storageStatusFixture()))
  render(<QueryClientProvider client={client}><StorageBudgetStatus/><ChatDataSettings/></QueryClientProvider>)
  return client
}
async function pressure(client: QueryClient) {
  const previous = client.getQueryData<ReturnType<typeof decodeStorageBudgetStatus>>(storageBudgetStatusKey)
  const unavailable = decodeStorageBudgetStatus({...storageStatusFixture(), availability:'unavailable', snapshot:null, reason:'storage-busy', readOnly:true})
  await act(async () => {client.setQueryData(storageBudgetStatusKey, retainStorageSnapshot(unavailable, previous))})
}
beforeEach(() => {mocks.fetch.mockReset(); mocks.flush.mockClear(); mocks.load.mockClear(); mocks.fetch.mockImplementation(async () => Response.json(location))})
afterEach(() => {cleanup(); clients.forEach(client => client.clear()); clients.length = 0; vi.useRealTimers()})
it('does not periodically scan legacy location storage', async () => {
  vi.useFakeTimers(); mount()
  await act(async () => {await vi.advanceTimersByTimeAsync(50)})
  expect(mocks.fetch).toHaveBeenCalledTimes(1)
  await act(async () => {await vi.advanceTimersByTimeAsync(30_000)})
  expect(mocks.fetch).toHaveBeenCalledTimes(1)
  expect(mocks.fetch.mock.calls[0][0]).toBe('/api/storage')
})
it('keeps dirty move path and frozen revision across unavailable pressure', async () => {
  const client = mount()
  fireEvent.click(await screen.findByRole('button', {name:'Change location…'}))
  fireEvent.change(screen.getByLabelText('New folder'), {target:{value:'/private/typed'}})
  await pressure(client)
  expect((screen.getByLabelText('New folder') as HTMLInputElement).value).toBe('/private/typed')
  expect(mocks.flush).not.toHaveBeenCalled(); expect(mocks.load).not.toHaveBeenCalled()
  expect(mocks.fetch).toHaveBeenCalledTimes(1)
  fireEvent.click(screen.getByRole('button', {name:'Move data'}))
  await waitFor(() => expect(mocks.fetch).toHaveBeenCalledTimes(2))
  expect(mocks.fetch.mock.calls[1][0]).toBe('/api/storage/move')
  expect(JSON.parse(mocks.fetch.mock.calls[1][1].body)).toEqual({path:'/private/typed', revision:'frozen-original'})
})
it('keeps restore File destination confirmation and revision across unavailable pressure', async () => {
  const client = mount()
  fireEvent.click(await screen.findByRole('button', {name:'Restore backup…'}))
  const file = new File(['synthetic backup'], 'backup.zip', {type:'application/zip'})
  fireEvent.change(screen.getByLabelText('Chat backup'), {target:{files:[file]}})
  fireEvent.change(screen.getByLabelText('Restore into'), {target:{value:'/private/restored'}})
  fireEvent.change(screen.getByLabelText('Type Yes to confirm.'), {target:{value:'Yes'}})
  await pressure(client)
  expect((screen.getByLabelText('Restore into') as HTMLInputElement).value).toBe('/private/restored')
  expect((screen.getByLabelText('Type Yes to confirm.') as HTMLInputElement).value).toBe('Yes')
  expect((screen.getByLabelText('Chat backup') as HTMLInputElement).files?.[0]).toBe(file)
  expect(mocks.flush).not.toHaveBeenCalled(); expect(mocks.load).not.toHaveBeenCalled()
  expect(mocks.fetch).toHaveBeenCalledTimes(1)
  // Keep the explicit user mutation pending so this test cannot cause reload.
  mocks.fetch.mockImplementation(() => new Promise<Response>(() => {}))
  fireEvent.click(screen.getByRole('button', {name:'Restore and reload'}))
  await waitFor(() => expect(mocks.fetch).toHaveBeenCalledTimes(2))
  const [url, init] = mocks.fetch.mock.calls[1]
  expect(url).toBe('/api/storage/restore')
  expect(JSON.parse(init.body.get('settings'))).toEqual({path:'/private/restored', revision:'frozen-original'})
  expect(init.body.get('backup').name).toBe('backup.zip')
})
