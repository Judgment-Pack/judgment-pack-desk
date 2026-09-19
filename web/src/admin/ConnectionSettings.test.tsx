import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { effectiveConfig } from '../config/deskConfig'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { testQueryClient } from '../testing/harness'
import { ConnectionSettings } from './ConnectionSettings'

const fetch = vi.hoisted(() => vi.fn())
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: fetch }))
let states: Record<string, string>
beforeEach(() => {
  states = { drive: 'setup-required', gmail: 'setup-required' }
  fetch.mockImplementation(async (url: string) => {
    const provider = url.includes('/gmail/') ? 'gmail' : 'drive'
    if (url.endsWith('/configure')) { states[provider] = 'not-connected'; return Response.json({}) }
    return Response.json({ version: 1, state: states[provider] })
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })
function setup(available = true, returnTo = '/') {
  const config = effectiveConfig(undefined)
  config.desk = { present: false, path: '/synthetic/desk.json', problems: [], localGateway: { status: available ? 'ready' : 'unavailable' } }
  const view = (ready = available) => <MemoryRouter initialEntries={[{ pathname: '/admin', hash: '#connections', state: { returnTo } }]}>
    <QueryClientProvider client={client}><DeskConfigFixture value={{ ...config, desk: { ...config.desk!, localGateway: { status: ready ? 'ready' : 'unavailable' } } }}><ConnectionSettings /></DeskConfigFixture></QueryClientProvider>
  </MemoryRouter>
  const client = testQueryClient()
  return { ...render(view()), view }
}
function registration(text = '{"installed":{"client_id":"synthetic.apps.googleusercontent.com","client_secret":"local-only"}}') {
  const file = new File([text], 'desktop.json', { type: 'application/json' })
  Object.defineProperty(file, 'text', { value: async () => text })
  return file
}

it.each(['Google Drive', 'Gmail'])('configures %s in Admin without starting consent or storing registration in the DOM', async title => {
  const ui = setup()
  const open = await screen.findByRole('button', { name: `${title} registration` })
  await waitFor(() => expect(open.hasAttribute('disabled')).toBe(false))
  fireEvent.click(open)
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [registration()] } })
  await screen.findByText('Registration saved. Connect your account from the chat attachment menu.')
  const calls = fetch.mock.calls.filter(([url]) => url.endsWith('/configure'))
  expect(calls).toHaveLength(1)
  expect(calls[0]![0]).toBe(title === 'Gmail' ? '/api/connections/gmail/configure' : '/api/connections/configure')
  expect(JSON.parse(calls[0]![1].body)).toEqual({ clientId: 'synthetic.apps.googleusercontent.com', clientSecret: 'local-only' })
  expect(fetch.mock.calls.every(([url]) => /\/(status|configure)$/.test(url))).toBe(true)
  expect(ui.container.textContent).not.toMatch(/synthetic\.apps|local-only/)
  expect(document.body.textContent).not.toContain('local-only')
  fireEvent.click(screen.getByRole('button', { name: 'Close' }))
  await waitFor(() => expect(document.activeElement).toBe(open))
  expect(screen.getByRole('link', { name: 'Return to chat' }).getAttribute('href')).toBe('/')
})

it.each(['close', 'navigate', 'unavailable'])('ignores a late registration file read after %s', async action => {
  const ui = setup()
  const opener = await screen.findByRole('button', { name: 'Gmail registration' })
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  let finish!: (text: string) => void
  const file = new File(['{}'], 'desktop.json')
  Object.defineProperty(file, 'text', { value: () => new Promise(resolve => { finish = resolve }) })
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [file] } })
  if (action === 'close') fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  else if (action === 'navigate') ui.unmount()
  else ui.rerender(ui.view(false))
  await act(async () => { finish('{"installed":{"client_id":"synthetic.apps.googleusercontent.com"}}') })
  expect(fetch.mock.calls.some(([url]) => url.endsWith('/configure'))).toBe(false)
})

it('requires disconnect before replacing a connected account registration', async () => {
  states.gmail = 'connected'; setup()
  const opener = await screen.findByRole('button', { name: 'Gmail registration' })
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  await screen.findByText('Disconnect this account in My connections before changing its registration.')
  expect(document.querySelector('input[type=file]')).toBeNull()
  expect(fetch.mock.calls.some(([url]) => url.endsWith('/configure'))).toBe(false)
})

it('refuses a web client without exposing its contents or contacting configure', async () => {
  setup()
  const opener = await screen.findByRole('button', { name: 'Gmail registration' })
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [registration('{"web":{"client_secret":"private-value"}}')] } })
  await screen.findByText('Choose the credentials JSON for a Google Desktop app.')
  expect(document.body.textContent).not.toContain('private-value')
  expect(fetch.mock.calls.some(([url]) => url.endsWith('/configure'))).toBe(false)
})

it('does not offer unavailable setup or a foreign return destination', async () => {
  setup(false, '//external.invalid')
  expect(screen.getByRole('button', { name: 'Gmail registration' }).hasAttribute('disabled')).toBe(true)
  expect(screen.getByRole('link', { name: 'Manage gateway' }).getAttribute('href')).toBe('/admin#storage')
  expect(screen.queryByRole('link', { name: 'Return to chat' })).toBeNull()
  expect(fetch).not.toHaveBeenCalled()
})
