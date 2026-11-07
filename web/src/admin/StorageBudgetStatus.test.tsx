import {QueryClient, QueryClientProvider, focusManager} from '@tanstack/react-query'
import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {afterEach, expect, it, vi} from 'vitest'
import {StorageBudgetStatus} from './StorageBudgetStatus'
import {decodeStorageBudgetStatus, retainStorageSnapshot} from './storageBudgetModel'
import {storageStatusFixture, storageMeasuredFixture} from './storageBudgetModel.fixture'
import {storageBudgetStatusKey, useStorageBudgetStatus} from './useStorageBudgetStatus'
const fetch = vi.hoisted(() => vi.fn())
vi.mock('../files/client', async original => ({...await original<typeof import('../files/client')>(), deskFetch: fetch}))
const clients: QueryClient[] = []
function client() {const q = new QueryClient({defaultOptions:{queries:{retry:false}}}); clients.push(q); return q}
function Owner() {useStorageBudgetStatus(true); return null}
afterEach(() => {cleanup(); clients.forEach(q => q.clear()); clients.length = 0; focusManager.setFocused(undefined); vi.useRealTimers(); fetch.mockReset()})
it('shows exact logical rows, distinct scopes, unknown allocation and stale original as-of', async () => {
  const q = client(), first = decodeStorageBudgetStatus(storageStatusFixture())
  q.setQueryData(storageBudgetStatusKey, first)
  render(<QueryClientProvider client={q}><StorageBudgetStatus/></QueryClientProvider>)
  expect(screen.getAllByText(/9,007,199,254,740,993 bytes/).length).toBeGreaterThan(0)
  expect(screen.getAllByText('Not measured')).toHaveLength(4)
  expect(screen.getByText('Current workspace', {selector:'h3'})).toBeTruthy()
  expect(screen.getByText('Installation', {selector:'h3'})).toBeTruthy()
  expect(screen.getByText(/Context compaction changes/)).toBeTruthy()
  const unavailable = decodeStorageBudgetStatus({...storageStatusFixture(), availability:'unavailable', snapshot:null, reason:'storage-busy'})
  await act(async () => {q.setQueryData(storageBudgetStatusKey, retainStorageSnapshot(unavailable, first))})
  expect(screen.getByText(/Stale storage observation/)).toBeTruthy()
  expect(screen.getByText('2026-10-31T12:00:00Z')).toBeTruthy()
  expect(screen.getAllByText(/9,007,199,254,740,993 bytes/).length).toBeGreaterThan(0)
  expect(fetch).not.toHaveBeenCalled()
})
it('uses one owner timer with two consumers, skips hidden polling and never retries transport failures', async () => {
  vi.useFakeTimers(); focusManager.setFocused(true)
  fetch.mockImplementation(async () => Response.json(storageStatusFixture()))
  const q = client()
  render(<QueryClientProvider client={q}><Owner/><StorageBudgetStatus/><StorageBudgetStatus/></QueryClientProvider>)
  await act(async () => {await vi.advanceTimersByTimeAsync(50)})
  expect(fetch).toHaveBeenCalledTimes(1)
  await act(async () => {await vi.advanceTimersByTimeAsync(10_000)})
  expect(fetch).toHaveBeenCalledTimes(2)
  focusManager.setFocused(false)
  await act(async () => {await vi.advanceTimersByTimeAsync(30_000)})
  expect(fetch).toHaveBeenCalledTimes(2)
  fetch.mockRejectedValue(new Error('private transport diagnostic'))
  await act(async () => {focusManager.setFocused(true); await vi.advanceTimersByTimeAsync(50)})
  expect(fetch).toHaveBeenCalledTimes(3)
  await act(async () => {await vi.advanceTimersByTimeAsync(1000)})
  expect(fetch).toHaveBeenCalledTimes(3)
  expect(screen.getAllByText(/Stale storage observation/)).toHaveLength(2)
  expect(screen.queryByText('private transport diagnostic')).toBeNull()
  expect(fetch.mock.calls.every(([url]) => url === '/api/storage/status')).toBe(true)
})
it('manual refresh uses the same read and retains rows after failure', async () => {
  const q = client(); q.setQueryData(storageBudgetStatusKey, decodeStorageBudgetStatus(storageStatusFixture()))
  fetch.mockRejectedValue(new Error('unavailable'))
  render(<QueryClientProvider client={q}><StorageBudgetStatus/></QueryClientProvider>)
  fireEvent.click(screen.getByRole('button', {name:'Refresh storage status'}))
  await screen.findByText(/Stale storage observation/)
  expect(screen.getByText('2026-10-31T12:00:00Z')).toBeTruthy()
  expect(fetch).toHaveBeenCalledTimes(1)
})

it('keeps prior rows and as-of stale when malformed unknown usage supplies definite remaining and No', async () => {
  const q = client(), first = decodeStorageBudgetStatus(storageStatusFixture())
  q.setQueryData(storageBudgetStatusKey, first)
  const malformed = storageStatusFixture(), row = malformed.snapshot.installation as Record<string, unknown>
  row.known = false; row.chargedLogical = null; row.logicalRemaining = {bytes:'123', files:'0', directories:'0'}; row.effectiveLogicalRemaining = null; row.overBudget = false
  fetch.mockImplementation(async () => Response.json(malformed))
  render(<QueryClientProvider client={q}><StorageBudgetStatus/></QueryClientProvider>)
  fireEvent.click(screen.getByRole('button', {name:'Refresh storage status'}))
  await waitFor(() => expect(q.getQueryState(storageBudgetStatusKey)?.status).toBe('error'))
  expect(q.getQueryData(storageBudgetStatusKey)).toEqual(first)
  expect(screen.getByText(/Stale storage observation/)).toBeTruthy()
  expect(screen.getByText('2026-10-31T12:00:00Z')).toBeTruthy()
  expect(screen.getAllByText(/9,007,199,254,740,993 bytes/).length).toBeGreaterThan(0)
  expect(screen.queryByText('123 bytes · 0 files · 0 directories')).toBeNull()
  expect(screen.queryByText('No')).toBeNull()
  expect(fetch).toHaveBeenCalledTimes(1)
})

it('shows child low-space warnings with partial unknown metadata even when Desk accounting is unavailable', async () => {
  vi.useFakeTimers(); vi.setSystemTime(new Date('2026-11-01T12:00:01Z'))
  const childObservation = {coverage:'selected-child-roots-only', enforcement:'warning-only', state:'partial', observedAt:'2026-11-01T12:00:00Z', lowSpaceThresholdBytes:'1073741824', roots:[
    {component:'runner',role:'workspace',state:'measured',reason:'none',logicalBytes:'9007199254740993',files:'1',directories:'1',availableBytes:'0',lowSpace:true},
    {component:'native',role:'profile',state:'unavailable',reason:'unknown',logicalBytes:null,files:null,directories:null,availableBytes:null,lowSpace:null},
  ]}
  const q=client()
  q.setQueryData(storageBudgetStatusKey,decodeStorageBudgetStatus({...storageStatusFixture(),availability:'unavailable',snapshot:null,reason:'storage-busy',childObservation}))
  render(<QueryClientProvider client={q}><StorageBudgetStatus/></QueryClientProvider>)
  expect(screen.getByText(/Partial child storage observation/)).toBeTruthy()
  expect(screen.getByText(/Low filesystem space observed for child storage/)).toBeTruthy()
  expect(screen.getByText(/Child storage growth is measured where available and is not prevented/)).toBeTruthy()
  expect(screen.getByText(/9,007,199,254,740,993/)).toBeTruthy()
  expect(screen.getByText(/native · profile · Unavailable/).textContent).toContain('Unknown')
  expect(screen.getAllByRole('button').map(button=>button.textContent)).toEqual(['Edit storage limits', 'Refresh storage status'])
  expect(fetch).not.toHaveBeenCalled()
  await act(async()=>{q.setQueryData(storageBudgetStatusKey,decodeStorageBudgetStatus({...storageStatusFixture(),childObservation:{...childObservation,state:'stale'}}));await vi.advanceTimersByTimeAsync(1)})
  expect(screen.getByText(/Stale child storage observation/)).toBeTruthy()
  expect(screen.getByText(/Low filesystem space observed for child storage/)).toBeTruthy()
})
it('does not invent a low-space result from unknown child counters', () => {
  const q=client()
  q.setQueryData(storageBudgetStatusKey,decodeStorageBudgetStatus({...storageStatusFixture(),childObservation:{coverage:'selected-child-roots-only',enforcement:'warning-only',state:'partial',observedAt:'2026-11-01T12:00:00Z',lowSpaceThresholdBytes:'1073741824',roots:[{component:'git',role:'active-scratch',state:'unavailable',reason:'bounded',logicalBytes:null,files:null,directories:null,availableBytes:null,lowSpace:null}]}}))
  render(<QueryClientProvider client={q}><StorageBudgetStatus/></QueryClientProvider>)
  expect(screen.queryByText(/Low filesystem space observed for child storage/)).toBeNull()
  expect(screen.getByText(/git · active-scratch · Unavailable/).textContent).toContain('Filesystem available bytes: Unknown')
})

it('shows measured logical, sparse allocation and workspace overlap separately with original measurement time', async () => {
 const q=client(),wire=storageMeasuredFixture()
 q.setQueryData(storageBudgetStatusKey,decodeStorageBudgetStatus(wire))
 render(<QueryClientProvider client={q}><StorageBudgetStatus/></QueryClientProvider>)
 expect(screen.getByText('Measured logical usage')).toBeTruthy()
 expect(screen.getByText('Reported allocated storage')).toBeTruthy()
 expect(screen.getByText('Workspace attribution overlap')).toBeTruthy()
 expect(screen.getByText('0 bytes · 2 files · 1 directories')).toBeTruthy()
 expect(screen.getByText('23 bytes · 1 files · 0 directories')).toBeTruthy()
 expect(screen.getByText('2026-10-31T11:59:59Z')).toBeTruthy()
 expect(screen.getByText('2026-10-31T12:00:00Z')).toBeTruthy()
 expect(screen.getAllByText('Not measured')).toHaveLength(3)
 expect(screen.getByText(/does not establish exclusive storage or reclaimable bytes/)).toBeTruthy()
 expect(fetch).not.toHaveBeenCalled()
 const later=storageMeasuredFixture();later.snapshot.observedAt='2026-10-31T12:00:02Z'
 await act(async()=>{q.setQueryData(storageBudgetStatusKey,decodeStorageBudgetStatus(later))})
 // React Query schedules subscriber notifications after the cache write.
 expect(await screen.findByText('2026-10-31T12:00:02Z')).toBeTruthy()
 expect(screen.getByText('2026-10-31T11:59:59Z')).toBeTruthy()
 expect(screen.queryByText('2026-10-31T12:00:00Z')).toBeNull()
})
it('keeps unavailable allocation unknown and retains the original measured sample after refresh failure', async () => {
 const wire=storageMeasuredFixture();wire.snapshot.physicalAllocated={value:null,unknown:'unavailable'}
 const first=decodeStorageBudgetStatus(wire),q=client();q.setQueryData(storageBudgetStatusKey,first)
 fetch.mockRejectedValue(new Error('private diagnostic'))
 render(<QueryClientProvider client={q}><StorageBudgetStatus/></QueryClientProvider>)
 expect(screen.queryByText('0 bytes · 2 files · 1 directories')).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Refresh storage status'}))
 await waitFor(()=>expect(q.getQueryState(storageBudgetStatusKey)?.status).toBe('error'))
 expect(screen.getByText('2026-10-31T11:59:59Z')).toBeTruthy()
 expect(screen.getByText(/Stale usage measurement/)).toBeTruthy()
 expect(screen.getAllByText('Not measured')).toHaveLength(3)
 expect(fetch).toHaveBeenCalledTimes(1)
})
