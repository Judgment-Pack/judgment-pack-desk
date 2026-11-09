import {QueryClient, QueryClientProvider} from '@tanstack/react-query'
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {afterEach, expect, it, vi} from 'vitest'
import {StorageBudgetPolicyEditor} from './StorageBudgetPolicyEditor'
import {storagePolicyFixture} from './storageBudgetPolicy.fixture'
import {storageBudgetStatusKey} from './useStorageBudgetStatus'
const fetch = vi.hoisted(() => vi.fn())
vi.mock('../files/client', () => ({deskFetch: fetch}))
const clients: QueryClient[] = []
function mount(readOnly = false) {
  const client = new QueryClient({defaultOptions: {queries: {retry: false}}}); clients.push(client)
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  render(<QueryClientProvider client={client}><StorageBudgetPolicyEditor readOnly={readOnly}/></QueryClientProvider>)
  return invalidate
}
async function open() {
  fetch.mockResolvedValueOnce(new Response(null, {status: 204})).mockResolvedValueOnce(Response.json(storagePolicyFixture()))
  fireEvent.click(screen.getByRole('button', {name: 'Edit storage limits'}))
  await screen.findByLabelText('Total bytes limit')
}
afterEach(() => {cleanup(); clients.forEach(c => c.clear()); clients.length = 0; fetch.mockReset()})
it('adds no reads or mutation on mount and refuses read-only preparation', () => {
  mount(true)
  const button = screen.getByRole('button', {name: 'Edit storage limits'}) as HTMLButtonElement
  expect(button.disabled).toBe(true); fireEvent.click(button)
  expect(fetch).not.toHaveBeenCalled()
})
it('prepares explicitly before reading and edits exact large decimal values', async () => {
  const invalidate = mount(); await open()
  expect(fetch.mock.calls.map(([url]) => url)).toEqual(['/api/storage/policy/prepare', '/api/storage/policy'])
  fireEvent.change(screen.getByLabelText('Limits to edit'), {target: {value: 'installation'}})
  expect((screen.getByLabelText('Total bytes limit') as HTMLInputElement).value).toBe('9007199254740993')
  expect(invalidate).toHaveBeenCalledWith({queryKey: storageBudgetStatusKey})
  expect(fetch).toHaveBeenCalledTimes(2)
})
it('copies default once into a draft and invalidates the existing status cache only after save', async () => {
  const invalidate = mount(); await open(); invalidate.mockClear()
  fireEvent.click(screen.getByRole('button', {name: 'Copy default once'}))
  expect((screen.getByLabelText('Total bytes limit') as HTMLInputElement).value).toBe('2000')
  expect(fetch).toHaveBeenCalledTimes(2); expect(invalidate).not.toHaveBeenCalled()
  const expected = storagePolicyFixture(), next = structuredClone(expected); next.workspace.limits = structuredClone(next.defaultLimits)
  fetch.mockResolvedValueOnce(Response.json(next))
  fireEvent.click(screen.getByRole('button', {name: 'Save storage limits'}))
  await screen.findByText('Storage limits saved.')
  expect(JSON.parse(fetch.mock.calls[2][1].body)).toEqual({expected, scope: 'workspace', limits: expected.defaultLimits})
  expect(invalidate).toHaveBeenCalledTimes(1)
})
it('preserves the frozen draft on conflict and never retries or rebases it', async () => {
  mount(); await open()
  fireEvent.change(screen.getByLabelText('Total bytes limit'), {target: {value: '999'}})
  fetch.mockResolvedValueOnce(new Response(null, {status: 409}))
  fireEvent.click(screen.getByRole('button', {name: 'Save storage limits'}))
  await screen.findByText(/Storage policy changed or is busy/)
  expect((screen.getByLabelText('Total bytes limit') as HTMLInputElement).value).toBe('999')
  expect((screen.getByRole('button', {name: 'Save storage limits'}) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(screen.getByRole('button', {name: 'Save storage limits'}))
  expect(fetch).toHaveBeenCalledTimes(3)
  expect(JSON.parse(fetch.mock.calls[2][1].body).expected).toEqual(storagePolicyFixture())
})
it('keeps unknown transport outcomes private and preserves the draft', async () => {
  mount(); await open()
  fireEvent.change(screen.getByLabelText('Total bytes limit'), {target: {value: '998'}})
  fetch.mockRejectedValueOnce(new Error('private native path'))
  fireEvent.click(screen.getByRole('button', {name: 'Save storage limits'}))
  await screen.findByText(/Storage policy outcome is not confirmed/)
  expect(screen.queryByText('private native path')).toBeNull()
  expect((screen.getByLabelText('Total bytes limit') as HTMLInputElement).value).toBe('998')
  expect(fetch).toHaveBeenCalledTimes(3)
})
it('rejects noncanonical local decimal text before mutation and permits correction', async () => {
  mount(); await open()
  fireEvent.change(screen.getByLabelText('Total bytes limit'), {target: {value: '01'}})
  fireEvent.click(screen.getByRole('button', {name: 'Save storage limits'}))
  await screen.findByText(/Enter positive whole decimal limits/)
  expect(fetch).toHaveBeenCalledTimes(2)
  fireEvent.change(screen.getByLabelText('Total bytes limit'), {target: {value: '997'}})
  expect((screen.getByRole('button', {name: 'Save storage limits'}) as HTMLButtonElement).disabled).toBe(false)
})
it('explicitly discards and reloads a fresh snapshot without repeating preparation or mutation', async () => {
  mount(); await open()
  fireEvent.change(screen.getByLabelText('Total bytes limit'), {target: {value: '999'}})
  const next = storagePolicyFixture(); next.workspace.limits.total.bytes = '800'
  fetch.mockResolvedValueOnce(Response.json(next))
  fireEvent.click(screen.getByRole('button', {name: 'Discard draft and reload policy'}))
  await waitFor(() => expect((screen.getByLabelText('Total bytes limit') as HTMLInputElement).value).toBe('800'))
  expect(fetch.mock.calls.map(([url]) => url)).toEqual(['/api/storage/policy/prepare', '/api/storage/policy', '/api/storage/policy'])
  expect(fetch.mock.calls[2][1]).toBeUndefined()
})

it('preserves an existing draft when storage becomes read only', async () => {
  const client = new QueryClient({defaultOptions: {queries: {retry: false}}}); clients.push(client)
  const view = render(<QueryClientProvider client={client}><StorageBudgetPolicyEditor readOnly={false}/></QueryClientProvider>)
  await open()
  fireEvent.change(screen.getByLabelText('Total bytes limit'), {target: {value: '996'}})
  view.rerender(<QueryClientProvider client={client}><StorageBudgetPolicyEditor readOnly/></QueryClientProvider>)
  expect((screen.getByLabelText('Total bytes limit') as HTMLInputElement).value).toBe('996')
  expect((screen.getByRole('button', {name: 'Save storage limits'}) as HTMLButtonElement).disabled).toBe(true)
  expect(fetch).toHaveBeenCalledTimes(2)
})
