import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useCallback, useRef, useState, type ReactNode } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { effectiveConfig } from '../config/deskConfig'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { testQueryClient } from '../testing/harness'
import { ConnectionSettings } from './ConnectionSettings'
import { InspectorPresentationContext, type InspectorPresentation } from '../shell/InspectorPresentation'
import { InspectorSlotContext } from '../shell/InspectorSlot'
import { RightPane } from '../shell/RightPane'

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
function GuideShell({ children, drawer }: { children: ReactNode; drawer: boolean }) {
  const [presentation, setPresentation] = useState<InspectorPresentation | null>(null)
  const [target, setTarget] = useState<HTMLDivElement | null>(null)
  const register = useCallback((value: InspectorPresentation) => { setPresentation(value); return () => setPresentation(current => current === value ? null : current) }, [])
  const claim = useCallback(() => () => {}, [])
  const opener = useRef<HTMLButtonElement>(null)
  return <InspectorPresentationContext.Provider value={register}>
    <InspectorSlotContext.Provider value={{ target, claim, open: !!presentation?.open, size: 420, tab: null, setTab: () => {}, reveal: () => {} }}>
      {children}
      <RightPane title={presentation?.title} open={!!presentation?.open} onClose={() => presentation?.onOpenChange(false)} asDrawer={drawer}
        declaredWidth={420} publishTarget={setTarget} publishPane={() => {}} openerRef={opener} restoreFocusRef={presentation?.restoreFocusRef} showEmpty={false} />
    </InspectorSlotContext.Provider>
  </InspectorPresentationContext.Provider>
}
function setup(available = true, returnTo = '/', guide?: { drawer: boolean }) {
  const config = effectiveConfig(undefined)
  config.desk = { present: false, path: '/synthetic/desk.json', problems: [], localGateway: { status: available ? 'ready' : 'unavailable' } }
  const view = (ready = available) => <MemoryRouter initialEntries={[{ pathname: '/admin', hash: '#connections', state: { returnTo } }]}>
    <QueryClientProvider client={client}><DeskConfigFixture value={{ ...config, desk: { ...config.desk!, localGateway: { status: ready ? 'ready' : 'unavailable' } } }}>
      {guide ? <GuideShell drawer={guide.drawer}><ConnectionSettings /></GuideShell> : <ConnectionSettings />}
    </DeskConfigFixture></QueryClientProvider>
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

it.each([
  ['Google Drive', false], ['Gmail', false], ['Google Drive', true], ['Gmail', true]
] as const)('hands %s setup to the guide and back (drawer: %s)', async (title, drawer) => {
  const ui = setup(true, '/', { drawer })
  const opener = await screen.findByRole('button', { name: `${title} registration` })
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  fireEvent.click(screen.getByRole('button', { name: 'Guide' }))
  expect(screen.queryByRole('dialog', { name: `${title} registration` })).toBeNull()
  const pane = await screen.findByRole(drawer ? 'dialog' : 'complementary', { name: `${title} setup guide` })
  await waitFor(() => expect(pane.contains(document.activeElement)).toBe(true))
  expect(within(pane).getAllByRole('listitem')).toHaveLength(6)
  const scope = title === 'Gmail' ? 'gmail.readonly' : 'drive.file'
  expect(within(pane).getByText(`https://www.googleapis.com/auth/${scope}`)).toBeTruthy()
  const official = within(pane).getByRole('link', { name: 'Official Google documentation' })
  expect(official.getAttribute('href')).toBe('https://developers.google.com/workspace/guides/create-credentials#desktop-app')
  expect(official.getAttribute('target')).toBe('_blank')
  expect(fetch.mock.calls.every(([url]) => url.endsWith('/status'))).toBe(true)
  fireEvent.click(within(pane).getByRole('button', { name: 'Continue setup' }))
  const dialog = await screen.findByRole('dialog', { name: `${title} registration` })
  await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true))
  expect(screen.queryByRole(drawer ? 'dialog' : 'complementary', { name: `${title} setup guide` })).toBeNull()
  fireEvent.change(dialog.querySelector('input[type=file]')!, { target: { files: [registration()] } })
  await screen.findByText('Registration saved. Connect your account from the chat attachment menu.')
  expect(fetch.mock.calls.filter(([url]) => url.endsWith('/configure'))).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: 'Close' }))
  fireEvent.click(opener)
  fireEvent.click(screen.getByRole('button', { name: 'Guide' }))
  await screen.findByRole(drawer ? 'dialog' : 'complementary', { name: `${title} setup guide` })
  ui.rerender(ui.view(false))
  expect(screen.queryByText('Official Google documentation')).toBeNull()
})

it('returns from the Gmail guide drawer to the Gmail setup control', async () => {
  setup(true, '/', { drawer: true })
  const opener = await screen.findByRole('button', { name: 'Gmail registration' })
  // RightPane only restores focus to a visible initiating control.
  opener.getClientRects = () => [new DOMRect(0, 0, 80, 32)] as unknown as DOMRectList
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  fireEvent.click(screen.getByRole('button', { name: 'Guide' }))
  const guide = await screen.findByRole('dialog', { name: 'Gmail setup guide' })
  fireEvent.click(within(guide).getByRole('button', { name: 'Close gmail setup guide' }))
  await waitFor(() => expect(document.activeElement).toBe(opener))
  expect(screen.queryByRole('dialog')).toBeNull()
})
