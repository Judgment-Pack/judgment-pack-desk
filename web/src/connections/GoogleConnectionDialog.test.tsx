import { useRef, useState } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { GoogleConnectionDialog } from './GoogleConnectionDialog'
import type { ConnectionProvider } from './client'

const mocks = vi.hoisted(() => ({ fetch: vi.fn(), authorize: vi.fn() }))
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: mocks.fetch }))
vi.mock('./client', async original => ({ ...await original<typeof import('./client')>(), authorizeDrive: mocks.authorize }))
let state: string
beforeEach(() => {
  state = 'not-connected'
  mocks.fetch.mockImplementation(async (url: string) => {
    if (url.endsWith('/configure')) { state = 'not-connected'; return Response.json({}) }
    if (url.endsWith('/disconnect')) { state = 'not-connected'; return Response.json({ revoked: false }) }
    return Response.json({ version: 1, state, account: state === 'connected' ? { id: 'account-a', email: 'person@example.test' } : undefined })
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })
function Harness({ available = true, provider = 'google-drive', onSelected }: {
  available?: boolean; provider?: ConnectionProvider; onSelected?: (items: any[]) => void
}) {
  const [open, setOpen] = useState(true), opener = useRef<HTMLButtonElement>(null)
  return <><button ref={opener} onClick={() => setOpen(true)}>Attach</button>
    <GoogleConnectionDialog open={open} onOpenChange={setOpen} provider={provider} available={available} openerRef={opener} onSelected={onSelected} />
  </>
}
function setup(props: Parameters<typeof Harness>[0] = {}) {
  const client = testQueryClient()
  const view = (next = props) => <MemoryRouter><QueryClientProvider client={client}><Harness {...next} /></QueryClientProvider></MemoryRouter>
  return { ...render(view()), view }
}
it('routes missing registration to Admin without importing files or starting OAuth', async () => {
  state = 'setup-required'; setup()
  await screen.findByText('Configure Google registration in Admin → Connections before signing in.')
  expect(screen.getByRole('link', { name: 'Set up in Admin' }).getAttribute('href')).toBe('/admin#connections')
  expect(screen.queryByRole('button', { name: 'Continue with Google' })).toBeNull()
  expect(document.querySelector('input[type=file]')).toBeNull()
  expect(mocks.authorize).not.toHaveBeenCalled()
})
it.each(['google-drive', 'gmail'] as const)('uses locally configured %s sign-in without a credential import', async provider => {
  const selected = vi.fn()
  mocks.authorize.mockImplementation(async () => { state = 'connected'; return [] })
  setup({ provider, onSelected: selected })
  const connect = await screen.findByRole('button', { name: 'Continue with Google' })
  expect(document.querySelector('input[type=file]')).toBeNull()
  expect(screen.queryByText('Configure Google registration in Admin → Connections before signing in.')).toBeNull()
  expect(mocks.authorize).not.toHaveBeenCalled()
  fireEvent.click(connect)
  await waitFor(() => expect(selected).toHaveBeenCalledWith([]))
  expect(mocks.authorize).toHaveBeenCalledWith(provider === 'google-drive' ? 'pick' : 'connect', expect.any(AbortSignal), provider)
  expect(mocks.fetch.mock.calls.some(call => call[0].endsWith('/configure'))).toBe(false)
})
it('connects Gmail before returning to email selection and keeps management consent separate', async () => {
  mocks.authorize.mockImplementation(async () => { state = 'connected'; return [] })
  const selected = vi.fn(); setup({ provider: 'gmail', onSelected: selected })
  fireEvent.click(await screen.findByRole('button', { name: 'Continue with Google' }))
  await waitFor(() => expect(selected).toHaveBeenCalledWith([]))
  expect(mocks.authorize).toHaveBeenCalledWith('connect', expect.any(AbortSignal), 'gmail')
})
it.each(['close', 'unavailable'] as const)('cancels OAuth on %s and ignores a late success', async mode => {
  let finish!: (value: unknown[]) => void
  mocks.authorize.mockReturnValue(new Promise(resolve => { finish = resolve }))
  const selected = vi.fn(), ui = setup({ onSelected: selected })
  fireEvent.click(await screen.findByRole('button', { name: 'Continue with Google' }))
  const signal = mocks.authorize.mock.calls[0]![1] as AbortSignal
  if (mode === 'close') fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  else ui.rerender(ui.view({ onSelected: selected, available: false }))
  expect(signal.aborted).toBe(true)
  await act(async () => { finish([{ fileId: 'late', grant: 'a'.repeat(64) }]) })
  expect(selected).not.toHaveBeenCalled()
})
it('reports failed remote revocation without claiming all Google access was removed', async () => {
  state = 'connected'; setup()
  await screen.findByText('person@example.test')
  fireEvent.click(screen.getByRole('button', { name: 'Disconnect' }))
  await screen.findByText('Disconnected here. Remove access in your Google account to finish revoking access.')
  expect(screen.queryByText('person@example.test')).toBeNull()
})
