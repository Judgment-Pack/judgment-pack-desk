import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useCallback, useState, type ReactNode } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { effectiveConfig } from '../config/deskConfig'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { testQueryClient } from '../testing/harness'
import { ConnectionSettings } from './ConnectionSettings'
import { InspectorPresentationContext, type InspectorPresentation } from '../shell/InspectorPresentation'
import { InspectorSlotContext } from '../shell/InspectorSlot'
import { RightPane } from '../shell/RightPane'
import { ConnectionPaneContext } from '../connections/ConnectionPaneContext'
import { connectionCatalogFixture } from '../testing/connectionCatalog'
import { genericCatalog, genericConnection } from '../testing/genericConnection'
import { CONNECTIONS_KEY } from '../connections/client'

const fetch = vi.hoisted(() => vi.fn())
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: fetch }))
const openConnection = vi.fn(), closeConnection = vi.fn()
let states: Record<string, string>
let catalog: unknown
beforeEach(() => {
  sessionStorage.clear()
  states = { drive: 'setup-required', gmail: 'setup-required', notion: 'not-connected', obsidian: 'not-connected', 'fixture-files': 'setup-required' }
  catalog = structuredClone(connectionCatalogFixture)
  fetch.mockImplementation(async (url: string) => {
    if (url.endsWith('/catalog')) return Response.json(catalog)
    const provider = url.split('/')[3] === 'status' || url.split('/')[3] === 'configure' ? 'drive' : url.split('/')[3]
    if (url.endsWith('/configure')) { states[provider] = 'not-connected'; return Response.json({}) }
    return Response.json({ version: 1, provider, state: states[provider], maxFiles: 4, maxFileBytes: 4 << 20 })
  })
})
afterEach(() => { cleanup(); vi.clearAllMocks() })
function SetupShell({ children, drawer }: { children: ReactNode; drawer: boolean }) {
  const [presentation, setPresentation] = useState<InspectorPresentation | null>(null)
  const [target, setTarget] = useState<HTMLDivElement | null>(null)
  const register = useCallback((value: InspectorPresentation) => { setPresentation(value); return () => setPresentation(current => current === value ? null : current) }, [])
  const claim = useCallback(() => () => {}, [])
  return <InspectorPresentationContext.Provider value={register}>
    <InspectorSlotContext.Provider value={{ target, claim, open: !!presentation?.open, size: 480, tab: null, setTab: () => {}, reveal: () => {} }}>
      {children}
      <RightPane title={presentation?.title} open={!!presentation?.open} onClose={() => presentation?.onOpenChange(false)} asDrawer={drawer}
        declaredWidth={480} publishTarget={setTarget} publishPane={() => {}} restoreFocusRef={presentation?.restoreFocusRef} showEmpty={false} />
    </InspectorSlotContext.Provider>
  </InspectorPresentationContext.Provider>
}
function setup(available = true, returnTo = '/', pane: { drawer: boolean } = { drawer: false }) {
  const config = effectiveConfig(undefined)
  config.desk = { present: false, path: '/synthetic/desk.json', problems: [], localGateway: { status: available ? 'ready' : 'unavailable' } }
  const view = (ready = available) => <MemoryRouter initialEntries={[{ pathname: '/admin', hash: '#connections', state: { returnTo } }]}>
    <QueryClientProvider client={client}><DeskConfigFixture value={{ ...config, desk: { ...config.desk!, localGateway: { status: ready ? 'ready' : 'unavailable' } } }}>
      <ConnectionPaneContext.Provider value={{ open: openConnection, close: closeConnection }}><SetupShell drawer={pane.drawer}><ConnectionSettings /></SetupShell></ConnectionPaneContext.Provider>
    </DeskConfigFixture></QueryClientProvider>
  </MemoryRouter>
  const client = testQueryClient()
  return { ...render(view()), view, client }
}
function registration(text = '{"installed":{"client_id":"synthetic.apps.googleusercontent.com","client_secret":"local-only"}}') {
  const file = new File([text], 'desktop.json', { type: 'application/json' })
  Object.defineProperty(file, 'text', { value: async () => text })
  return file
}

it.each(['Google Drive', 'Gmail'])('configures %s in Admin without starting consent or storing registration in the DOM', async title => {
  const ui = setup()
  const open = await screen.findByRole('button', { name: new RegExp(`${title} registration$`) })
  await waitFor(() => expect(open.hasAttribute('disabled')).toBe(false))
  fireEvent.click(open)
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [registration()] } })
  await screen.findByText('Registration saved. Connect your account from the chat attachment menu.')
  const calls = fetch.mock.calls.filter(([url]) => url.endsWith('/configure'))
  expect(calls).toHaveLength(1)
  expect(calls[0]![0]).toBe(title === 'Gmail' ? '/api/connections/gmail/configure' : '/api/connections/configure')
  expect(JSON.parse(calls[0]![1].body)).toEqual({ clientId: 'synthetic.apps.googleusercontent.com', clientSecret: 'local-only' })
  expect(fetch.mock.calls.every(([url]) => /\/(catalog|status|configure)$/.test(url))).toBe(true)
  expect(ui.container.textContent).not.toMatch(/synthetic\.apps|local-only/)
  expect(document.body.textContent).not.toContain('local-only')
  fireEvent.click(screen.getByRole('button', { name: 'Done' }))
  await waitFor(() => expect(document.activeElement).toBe(open))
  expect(screen.getByRole('link', { name: 'Return to chat' }).getAttribute('href')).toBe('/')
})

it.each(['close', 'navigate', 'unavailable'])('ignores a late registration file read after %s', async action => {
  const ui = setup()
  const opener = await screen.findByRole('button', { name: /Gmail registration$/ })
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
  const opener = await screen.findByRole('button', { name: /Gmail registration$/ })
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  await screen.findByText('Disconnect this account in My connections before changing its registration.')
  expect(document.querySelector('input[type=file]')).toBeNull()
  expect(fetch.mock.calls.some(([url]) => url.endsWith('/configure'))).toBe(false)
})

it('refuses a web client without exposing its contents or contacting configure', async () => {
  setup()
  const opener = await screen.findByRole('button', { name: /Gmail registration$/ })
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [registration('{"web":{"client_secret":"private-value"}}')] } })
  await screen.findByText('Choose the credentials JSON for a Google Desktop app.')
  expect(document.body.textContent).not.toContain('private-value')
  expect(fetch.mock.calls.some(([url]) => url.endsWith('/configure'))).toBe(false)
})

it('does not offer unavailable setup or a foreign return destination', async () => {
  setup(false, '//external.invalid')
  expect(screen.queryByRole('button', { name: /Gmail registration$/ })).toBeNull()
  expect(screen.getByText('Local processing is unavailable. Check the details in Admin → Storage & data.')).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Manage gateway' }).getAttribute('href')).toBe('/admin#storage')
  expect(screen.queryByRole('link', { name: 'Return to chat' })).toBeNull()
  expect(fetch).not.toHaveBeenCalled()
})

it.each([
  ['Google Drive', false], ['Gmail', false], ['Google Drive', true], ['Gmail', true]
] as const)('opens %s instructions and upload together (drawer: %s)', async (title, drawer) => {
  const ui = setup(true, '/', { drawer })
  const opener = await screen.findByRole('button', { name: new RegExp(`${title} registration$`) })
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  const pane = await screen.findByRole(drawer ? 'dialog' : 'complementary', { name: new RegExp(`${title} registration$`) })
  // A desktop setup is nonmodal; a narrow screen has only the shell's side drawer.
  expect(screen.queryAllByRole('dialog')).toHaveLength(drawer ? 1 : 0)
  expect(screen.queryByRole('button', { name: 'Guide' })).toBeNull()
  await waitFor(() => expect(pane.contains(document.activeElement)).toBe(true))
  const instructions = within(pane).getByText('Setup instructions').closest('details')!
  expect(instructions.open).toBe(true)
  expect(within(pane).getAllByRole('listitem')).toHaveLength(6)
  const scope = title === 'Gmail' ? 'gmail.readonly' : 'drive.file'
  expect(within(pane).getByText(`https://www.googleapis.com/auth/${scope}`)).toBeTruthy()
  const official = within(pane).getByRole('link', { name: 'Official Google documentation' })
  expect(official.getAttribute('href')).toBe('https://developers.google.com/workspace/guides/create-credentials#desktop-app')
  expect(official.getAttribute('target')).toBe('_blank')
  expect(fetch.mock.calls.every(([url]) => /\/(catalog|status)$/.test(url))).toBe(true)
  fireEvent.click(instructions.querySelector('summary')!)
  await waitFor(() => expect(instructions.open).toBe(false))
  expect(within(pane).getByRole('button', { name: 'Choose credentials file' })).toBeTruthy()
  fireEvent.change(pane.querySelector('input[type=file]')!, { target: { files: [registration()] } })
  await within(pane).findByText('Registration saved. Connect your account from the chat attachment menu.')
  expect(instructions.open).toBe(false)
  expect(fetch.mock.calls.filter(([url]) => url.endsWith('/configure'))).toHaveLength(1)
  fireEvent.click(within(pane).getByRole('button', { name: 'Done' }))
  fireEvent.click(opener)
  const reopened = await screen.findByRole(drawer ? 'dialog' : 'complementary', { name: new RegExp(`${title} registration$`) })
  expect(within(reopened).getByText('Setup instructions').closest('details')!.open).toBe(false)
  fireEvent.click(within(reopened).getByText('Setup instructions'))
  await waitFor(() => expect(within(reopened).getByText('Setup instructions').closest('details')!.open).toBe(true))
  ui.rerender(ui.view(false))
  expect(screen.queryByText('Official Google documentation')).toBeNull()
})

it('returns from the Gmail setup drawer to the Gmail setup control', async () => {
  setup(true, '/', { drawer: true })
  const opener = await screen.findByRole('button', { name: /Gmail registration$/ })
  // RightPane only restores focus to a visible initiating control.
  opener.getClientRects = () => [new DOMRect(0, 0, 80, 32)] as unknown as DOMRectList
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  const pane = await screen.findByRole('dialog', { name: /Gmail registration$/ })
  fireEvent.click(within(pane).getByRole('button', { name: 'Collapse gmail registration' }))
  await waitFor(() => expect(document.activeElement).toBe(opener))
  expect(screen.queryByRole('dialog')).toBeNull()
})

it('abandons an unfinished file read when switching providers', async () => {
  setup()
  const gmail = await screen.findByRole('button', { name: /Gmail registration$/ })
  await waitFor(() => expect(gmail.hasAttribute('disabled')).toBe(false))
  fireEvent.click(gmail)
  let finish!: (text: string) => void
  const file = new File(['{}'], 'desktop.json')
  Object.defineProperty(file, 'text', { value: () => new Promise(resolve => { finish = resolve }) })
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [file] } })
  fireEvent.click(screen.getByRole('button', { name: /Google Drive registration$/ }))
  await act(async () => { finish('{"installed":{"client_id":"synthetic.apps.googleusercontent.com"}}') })
  expect(fetch.mock.calls.some(([url]) => url.endsWith('/configure'))).toBe(false)
  const pane = await screen.findByRole('complementary', { name: /Google Drive registration$/ })
  expect(within(pane).queryByRole('status')).toBeNull()
  fireEvent.change(pane.querySelector('input[type=file]')!, { target: { files: [registration()] } })
  await within(pane).findByText('Registration saved. Connect your account from the chat attachment menu.')
  expect(fetch.mock.calls.filter(([url]) => url.endsWith('/configure')).map(([url]) => url)).toEqual(['/api/connections/configure'])
})

it('aborts an in-flight upload and ignores its late response after closing', async () => {
  setup()
  const opener = await screen.findByRole('button', { name: /Gmail registration$/ })
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  let finish!: (response: Response) => void
  const normal = fetch.getMockImplementation()!
  fetch.mockImplementation((url: string, options: RequestInit) => url.endsWith('/configure')
    ? new Promise(resolve => { finish = resolve }) : normal(url, options))
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [registration()] } })
  await waitFor(() => expect(fetch.mock.calls.some(([url]) => url.endsWith('/configure'))).toBe(true))
  const signal = fetch.mock.calls.find(([url]) => url.endsWith('/configure'))![1].signal as AbortSignal
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(signal.aborted).toBe(true)
  fireEvent.click(screen.getByRole('button', { name: /Google Drive registration$/ }))
  await act(async () => { finish(Response.json({})) })
  expect(screen.queryByText('Registration saved. Connect your account from the chat attachment menu.')).toBeNull()
  expect(screen.getByRole('complementary', { name: /Google Drive registration$/ })).toBeTruthy()
})


it('lists every advertised legacy provider and opens the shared connection pane', async () => {
  setup()
  for (const title of ['Google Drive', 'Gmail', 'Notion', 'Obsidian']) expect(await screen.findByRole('heading', { name: title })).toBeTruthy()
  const opener = screen.getByRole('button', { name: /Notion$/ })
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  expect(opener.getAttribute('aria-label')).toBe('Connect: Notion')
  expect(openConnection).toHaveBeenCalledWith({ provider: 'notion', descriptor: expect.objectContaining({ id: 'notion' }), opener })
  expect(fetch.mock.calls.every(([url]) => /\/(catalog|status)$/.test(url))).toBe(true)
})

it('discovers a new gateway provider without a Desk-specific row and shows its current state', async () => {
  catalog = genericCatalog()
  states['fixture-files'] = 'connected'
  setup()
  const opener = await screen.findByRole('button', { name: /Fixture files$/ })
  await screen.findByText('Connected')
  expect(opener.textContent).toBe('Manage')
  expect(screen.queryByText('Google Drive')).toBeNull()
  fireEvent.click(opener)
  expect(openConnection).toHaveBeenCalledWith({ provider: 'fixture-files', descriptor: genericConnection, opener })
  expect(fetch.mock.calls.map(([url]) => url)).toEqual(['/api/connections/catalog', '/api/connections/fixture-files/status'])
})

it('shows an update hint for an advertised unsupported protocol without requesting its status', async () => {
  catalog = { ...genericCatalog(), providers: [{ ...genericConnection, protocol: 'future-protocol' }] }
  setup()
  await screen.findByRole('heading', { name: /Fixture files$/ })
  expect(screen.getByText('Update Desk to use this connection.')).toBeTruthy()
  expect(screen.queryByRole('button', { name: /Fixture files$/ })).toBeNull()
  expect(fetch.mock.calls.map(([url]) => url)).toEqual(['/api/connections/catalog'])
})

it('offers retry on catalog failure and no guessed providers', async () => {
  const normal = fetch.getMockImplementation()!
  fetch.mockImplementation(async (url: string, options: RequestInit) => url.endsWith('/catalog') ? Response.json({}, { status: 503 }) : normal(url, options))
  setup()
  await screen.findByRole('alert')
  expect(screen.getByText('Connections could not be loaded.')).toBeTruthy()
  expect(screen.queryByRole('heading', { name: 'Google Drive' })).toBeNull()
  fetch.mockImplementation(normal)
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  await screen.findByRole('heading', { name: /Notion$/ })
})

it('removes stale rows and cancels a pending registration when a catalog refresh fails', async () => {
  const ui = setup()
  const opener = await screen.findByRole('button', { name: /Gmail registration$/ })
  await waitFor(() => expect(opener.hasAttribute('disabled')).toBe(false))
  fireEvent.click(opener)
  let finish!: (text: string) => void
  const file = new File(['{}'], 'desktop.json')
  Object.defineProperty(file, 'text', { value: () => new Promise(resolve => { finish = resolve }) })
  fireEvent.change(document.querySelector('input[type=file]')!, { target: { files: [file] } })
  const normal = fetch.getMockImplementation()!
  fetch.mockImplementation(async (url: string, options: RequestInit) => url.endsWith('/catalog') ? Response.json({}, { status: 503 }) : normal(url, options))
  await act(async () => { await ui.client.invalidateQueries({ queryKey: [...CONNECTIONS_KEY, 'catalog'] }) })
  await screen.findByRole('alert')
  expect(screen.queryByRole('heading', { name: 'Gmail' })).toBeNull()
  expect(screen.queryByRole('complementary')).toBeNull()
  await act(async () => { finish('{"installed":{"client_id":"synthetic.apps.googleusercontent.com"}}') })
  expect(fetch.mock.calls.some(([url]) => url.endsWith('/configure'))).toBe(false)
})

it('keeps a failed provider visible with recovery through the shared pane', async () => {
  catalog = genericCatalog()
  const normal = fetch.getMockImplementation()!
  fetch.mockImplementation(async (url: string, options: RequestInit) => url.endsWith('/status') ? Response.json({}, { status: 503 }) : normal(url, options))
  setup()
  const opener = await screen.findByRole('button', { name: /Fixture files$/ })
  await screen.findByText('Unavailable')
  fireEvent.click(opener)
  expect(openConnection).toHaveBeenCalledWith(expect.objectContaining({ provider: 'fixture-files' }))
})

it('shows an honest empty catalog instead of fixed Google rows', async () => {
  catalog = { version: 3, providers: [], sources: [] }
  setup()
  await screen.findByText('No connections found.')
  expect(screen.queryByRole('heading', { name: 'Google Drive' })).toBeNull()
  expect(fetch.mock.calls.map(([url]) => url)).toEqual(['/api/connections/catalog'])
})

it('closes registration before opening another provider', async () => {
  setup()
  const gmail = await screen.findByRole('button', { name: /Gmail registration$/ })
  await waitFor(() => expect(gmail.hasAttribute('disabled')).toBe(false))
  fireEvent.click(gmail)
  await screen.findByRole('complementary', { name: /Gmail registration$/ })
  fireEvent.click(screen.getByRole('button', { name: /Obsidian$/ }))
  expect(screen.queryByRole('complementary')).toBeNull()
  expect(openConnection).toHaveBeenCalledWith(expect.objectContaining({ provider: 'obsidian' }))
})
