import { useRef, useState } from 'react'
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
  const view = (next = props) => <QueryClientProvider client={client}><Harness {...next} /></QueryClientProvider>
  return { ...render(view()), view }
}
it('keeps registration setup advanced and never pretends an unregistered build can sign in', async () => {
  state = 'setup-required'; setup()
  await screen.findByText('Google sign-in is not configured in this build. You can use your own Google app below.')
  expect(screen.queryByRole('button', { name: 'Continue' })).toBeNull()
  expect(screen.getByText('Use your own Google app').closest('details')?.open).toBe(false)
  expect(mocks.authorize).not.toHaveBeenCalled()
})
it('imports registration without leaving chat, then waits for a user click before OAuth', async () => {
  state = 'setup-required'; const selected = vi.fn(), ui = setup({ onSelected: selected })
  fireEvent.click(await screen.findByText('Use your own Google app'))
  const file = new File(['{}'], 'desktop.json', { type: 'application/json' })
  Object.defineProperty(file, 'text', { value: async () => JSON.stringify({ installed: { client_id: 'synthetic.apps.googleusercontent.com' } }) })
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [file] } })
  await screen.findByRole('button', { name: 'Continue' })
  expect(screen.getByRole('dialog')).toBeTruthy()
  expect(mocks.authorize).not.toHaveBeenCalled()
  const sent = JSON.parse(mocks.fetch.mock.calls.find(call => call[0].endsWith('/configure'))![1].body)
  expect(sent).toEqual({ clientId: 'synthetic.apps.googleusercontent.com', clientSecret: '' })
  mocks.authorize.mockImplementation(async () => { state = 'connected'; return [{ fileId: 'chosen', grant: 'a'.repeat(64) }] })
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
  await waitFor(() => expect(selected).toHaveBeenCalledWith([{ fileId: 'chosen', grant: 'a'.repeat(64) }]))
  expect(mocks.authorize).toHaveBeenCalledWith('pick', expect.any(AbortSignal), 'google-drive')
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Attach' })))
  expect(ui.container.textContent).not.toContain('synthetic.apps')
})
it('connects Gmail before returning to email selection and keeps management consent separate', async () => {
  mocks.authorize.mockImplementation(async () => { state = 'connected'; return [] })
  const selected = vi.fn(); setup({ provider: 'gmail', onSelected: selected })
  fireEvent.click(await screen.findByRole('button', { name: 'Continue' }))
  await waitFor(() => expect(selected).toHaveBeenCalledWith([]))
  expect(mocks.authorize).toHaveBeenCalledWith('connect', expect.any(AbortSignal), 'gmail')
})
it.each(['close', 'unavailable'] as const)('cancels OAuth on %s and ignores a late success', async mode => {
  let finish!: (value: unknown[]) => void
  mocks.authorize.mockReturnValue(new Promise(resolve => { finish = resolve }))
  const selected = vi.fn(), ui = setup({ onSelected: selected })
  fireEvent.click(await screen.findByRole('button', { name: 'Continue' }))
  const signal = mocks.authorize.mock.calls[0]![1] as AbortSignal
  if (mode === 'close') fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  else ui.rerender(ui.view({ onSelected: selected, available: false }))
  expect(signal.aborted).toBe(true)
  await act(async () => { finish([{ fileId: 'late', grant: 'a'.repeat(64) }]) })
  expect(selected).not.toHaveBeenCalled()
})
it('does not import registration after the dialog closes during a file read', async () => {
  state = 'setup-required'; setup()
  fireEvent.click(await screen.findByText('Use your own Google app'))
  let finish!: (value: string) => void
  const file = new File(['{}'], 'desktop.json')
  Object.defineProperty(file, 'text', { value: () => new Promise(resolve => { finish = resolve }) })
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [file] } })
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  await act(async () => { finish('{"installed":{"client_id":"synthetic.apps.googleusercontent.com"}}') })
  expect(mocks.fetch.mock.calls.some(call => call[0].endsWith('/configure'))).toBe(false)
})
it('reports failed remote revocation without claiming all Google access was removed', async () => {
  state = 'connected'; setup()
  await screen.findByText('person@example.test')
  fireEvent.click(screen.getByRole('button', { name: 'Disconnect' }))
  await screen.findByText('Disconnected here. Remove access in your Google account to finish revoking access.')
  expect(screen.queryByText('person@example.test')).toBeNull()
})
