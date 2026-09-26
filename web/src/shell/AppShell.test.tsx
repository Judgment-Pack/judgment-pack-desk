import { useMemo, useRef, useState } from 'react'
import { useConnectionsPane } from '../connections/ConnectionPaneContext'
import { useInspectorPortal } from './InspectorSlot'
import { useInspectorPresentation } from './InspectorPresentation'
/**
 * The frame, as a page: its landmarks, its defaults, and the one property the
 * whole arrangement rests on — that `<main>` never remounts.
 *
 * A closed pane is **absent from the accessibility tree**, not merely
 * invisible, which is why the landmark case opens both panes before counting.
 * That is the behaviour the shell wants: a viewer who has collapsed the
 * Inspector should not be able to tab into it, and a screen reader should not
 * be offered a region that is not there.
 */
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { AdminView } from '../routes/AdminView'
import { useDetailsSlot, useDetailsPortal } from './DetailsSlot'
import { ReadingDetails, MessageDetails, useReadingDetails } from '../chat/ReadingDetails'
import { AppShell } from './AppShell'
import { forgetConsole } from './consoleLog'
import { forgetAuthorBridge } from './authorBridge'
import { projectKey, shellStateKey } from './paneState'

/** The chassis' project root, which is what keys this desk's pane record. */
const ROOT = '/home/someone/a-project'

/**
 * The file API, answering the two things the shell asks it for: the listing,
 * whose `root` is the project identity, and `jpack-desk.json`, which is absent
 * here. Without the first the key stays provisional and nothing is written
 * under it — which is the behaviour, not a limitation of the stub.
 */
beforeEach(() => {
  vi.stubGlobal('fetch', async (url: string) => {
    const path = String(url)
    if (path.includes('/api/files')) {
      return {
        ok: true,
        status: 200,
        statusText: '',
        text: async () => JSON.stringify({ root: ROOT, files: [] })
      }
    }
    return {
      ok: false,
      status: 404,
      statusText: '',
      text: async () => JSON.stringify({ error: 'no such file' })
    }
  })
})

afterEach(() => {
  cleanup()
  forgetConsole()
  forgetAuthorBridge()
  vi.unstubAllGlobals()
  window.localStorage.clear()
})

const PROJECT = stubClient({
  list_packs: () => ({
    text: JSON.stringify({
      status: 'valid',
      configPath: '/p/jpack.json',
      packs: [{ id: 'intake-triage', matrix: true }]
    })
  })
})

function renderShell(
  ui: React.ReactNode,
  overrides: Partial<McpConnection> = {},
  path = '/'
) {
  const value = connected({ client: PROJECT.client, ...overrides })
  const router = createMemoryRouter(
    [{ path: '*', element: <McpContext.Provider value={value}>{ui}</McpContext.Provider> }],
    { initialEntries: [path] }
  )
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('the shell frame', () => {
  it.each(['/admin#assistant', '/admin/#assistant'])(
    'puts %s sections in the sidebar and restores app navigation on exit', async (path) => {
    renderShell(<AppShell><AdminView /></AppShell>, {}, path)
    const main = screen.getByRole('main')
    const settings = await screen.findByRole('navigation', { name: 'Settings' })
    expect(screen.getByRole('navigation', { name: 'Project' }).contains(settings)).toBe(true)
    expect(main.contains(settings)).toBe(false)
    expect(screen.queryByRole('button', { name: 'New chat' })).toBeNull()
    expect(screen.getByRole('heading', { name: 'Assistant', level: 2 })).toBeTruthy()
    fireEvent.click(screen.getByRole('link', { name: /^Organization/ }))
    expect(await screen.findByRole('heading', { name: 'Organization', level: 2 })).toBeTruthy()
    expect(screen.getByRole('main')).toBe(main)
    fireEvent.click(screen.getByRole('link', { name: 'Back to app' }))
    expect(await screen.findByRole('link', { name: /^Packs/ })).toBeTruthy()
    expect(screen.queryByRole('link', { name: 'Back to app' })).toBeNull()
  })

  it('puts the skip link first and points it at main', () => {
    const { container } = renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    const first = container.querySelector('a,button,input,textarea,select,[tabindex]')!
    expect(first.textContent).toBe('Skip to main content')
    expect(first.getAttribute('href')).toBe('#main')
    expect(screen.getByRole('main').id).toBe('main')
    expect(screen.getByRole('main').getAttribute('tabindex')).toBe('-1')
  })

  it('carries the strip’s two sentences verbatim', async () => {
    const { unmount } = renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>
    )
    expect(screen.getByRole('contentinfo').textContent).toContain('connected to jpack test')
    expect(screen.getByRole('banner').textContent).not.toContain('connected')
    unmount()
    // **The verdict is the status, not the metadata.** `server` is retained
    // across a reconnect, so a strip that read "connected to" off its presence
    // said so while the socket was down — and a fixture that expressed "not
    // connected" by nulling `server` was asserting the same conflation.
    const lost = renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>,
      { status: 'failed', server: null }
    )
    expect(screen.getByRole('contentinfo').textContent).toContain('not connected')
    lost.unmount()
    renderShell(
      <AppShell>
        <h1>a route</h1>
      </AppShell>,
      { status: 'reconnecting', client: null, attempt: 2 }
    )
    const strip = screen.getByRole('contentinfo').textContent ?? ''
    expect(strip).toContain('reconnecting')
    expect(strip).not.toContain('connected to jpack test')
  })

  it.each(['/', '/help', '/matrix', '/graphs', '/author', '/packs/example/evaluate'])(
    'ignores saved legacy pane flags on %s and exposes no global Inspector or Console', async path => {
      localStorage.setItem(shellStateKey(projectKey(ROOT)), JSON.stringify({v:2, inspector:{open:true},console:{open:true,tab:'calls'}}))
      const {container}=renderShell(<AppShell><h1>Page</h1></AppShell>, {}, path)
      await screen.findByRole('link', {name:/Packs/})
      expect(screen.queryByRole('button',{name:'Inspector'})).toBeNull()
      expect(screen.queryByRole('button',{name:'Console'})).toBeNull()
      expect(screen.queryByRole('complementary')).toBeNull()
      expect(container.querySelector('#desk-console')).toBeNull()
      fireEvent.keyDown(document.body,{key:'i',ctrlKey:true,altKey:true})
      expect(screen.queryByRole('complementary')).toBeNull()
    })
  it('keeps navigation toggle in the app header in both states and persists only navigation',async()=>{
    renderShell(<AppShell><input aria-label="Unfinished edit" defaultValue="Keep me" /></AppShell>)
    const input=screen.getByRole('textbox',{name:'Unfinished edit'})
    const button=screen.getByRole('button',{name:'Collapse navigation'})
    expect(screen.getByRole('banner').contains(button)).toBe(true)
    fireEvent.click(button)
    expect(screen.getByRole('button',{name:'Expand navigation'})).toBe(button)
    expect(screen.getByRole('textbox',{name:'Unfinished edit'})).toBe(input)
    await waitFor(()=>expect(JSON.parse(localStorage.getItem(shellStateKey(projectKey(ROOT)))!)).toEqual({v:2,left:{mode:'icons'}}))
    fireEvent.click(button)
    expect(screen.getByRole('button',{name:'Collapse navigation'})).toBe(button)
  })
  it('opens Diagnostics without a bottom pane and retains unsent work when collapsed',()=>{
    const {container}=renderShell(<AppShell><input aria-label="Unfinished edit" defaultValue="Keep me" /></AppShell>)
    const input=screen.getByRole('textbox',{name:'Unfinished edit'})
    fireEvent.keyDown(document.body,{key:'j',ctrlKey:true,altKey:true})
    expect(screen.getByRole('complementary',{name:'Diagnostics'})).toBeTruthy()
    expect(screen.getByRole('tabpanel',{name:'Connection'}).textContent).toContain('ready · connection 1')
    expect(container.querySelector('#desk-console')).toBeNull()
    fireEvent.click(screen.getByRole('button',{name:'Collapse diagnostics'}))
    expect(screen.queryByRole('complementary')).toBeNull()
    expect(screen.getByRole('textbox',{name:'Unfinished edit'})).toBe(input)
  })
})

function ReaderFixture() {
  const read = useReadingDetails('chat-one')
  const details = useDetailsSlot()
  const portal = useDetailsPortal(<p>Selected pack rule</p>)
  return <>{portal}<textarea aria-label="Unsent message" defaultValue="Keep my draft" />
    <button onClick={event => read(<MessageDetails text="Explain the attachment" input="EXACT CONTEXT" />, event.currentTarget)}>View message details</button>
    <button onClick={event => read(<ReadingDetails title="Source reader"><p>Source excerpt</p></ReadingDetails>, event.currentTarget)}>Open source text</button>
    <button onClick={details.reveal}>Inspect rule</button></>
}
it('replaces one detail reader, preserves draft text, restores focus and can return to pack details', async () => {
  renderShell(<AppShell><ReaderFixture /></AppShell>)
  const message = screen.getByRole('button', { name: 'View message details' })
  fireEvent.click(message)
  expect(await screen.findByRole('region', { name: 'Message details' })).toBeTruthy()
  expect(screen.queryByText('Selected pack rule')?.closest('[hidden]')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Open source text' }))
  expect(await screen.findByRole('region', { name: 'Source reader' })).toBeTruthy()
  expect(screen.queryByRole('region', { name: 'Message details' })).toBeNull()
  expect((screen.getByLabelText('Unsent message') as HTMLTextAreaElement).value).toBe('Keep my draft')
  fireEvent.keyDown(screen.getByRole('region', { name: 'Source reader' }), { key: 'Escape' })
  expect(screen.queryByRole('region', { name: 'Source reader' })).toBeNull()
  expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Open source text' }))
  fireEvent.click(screen.getByRole('button', { name: 'Inspect rule' }))
  expect(screen.getByText('Selected pack rule').closest('[hidden]')).toBeNull()
})

function ConnectionOverlayFixture() {
 const connections = useConnectionsPane(), opener = useRef<HTMLButtonElement>(null)
 const [open, setOpen] = useState(true), [width, setWidth] = useState(360)
 const presentation = useMemo(() => ({ title: 'Assistant', available: true, open, onOpenChange: setOpen, width, onResize: setWidth, onReset: () => setWidth(360), minimumMainWidth: 0, maximumWidth: 800 }), [open, width])
 useInspectorPresentation(presentation)
 const assistant = useInspectorPortal(<label>Assistant draft<input defaultValue="Unsent question" /></label>)
 return <><label>Main draft<input defaultValue="Unsent policy" /></label><button ref={opener} onClick={() => connections.open({ opener: opener.current })}>Open connections</button><button onClick={event => { connections.close?.({ restoreFocus: false }); event.currentTarget.focus() }}>Open another setup</button>{assistant}</>
}
it('overlays connections without replacing the Assistant portal or its route presentation', async () => {
 renderShell(<AppShell><ConnectionOverlayFixture /></AppShell>)
 const assistant = await screen.findByRole('textbox', { name: 'Assistant draft' }), main = screen.getByRole('textbox', { name: 'Main draft' })
 fireEvent.change(assistant, { target: { value: 'Keep this question' } }); fireEvent.change(main, { target: { value: 'Keep this policy' } })
 const opener = screen.getByRole('button', { name: 'Open connections' })
 fireEvent.click(opener)
 expect(screen.queryByRole('textbox', { name: 'Assistant draft' })).toBeNull()
 expect(assistant.isConnected).toBe(true)
 expect(screen.getByRole('textbox', { name: 'Main draft' })).toBe(main)
 expect(screen.queryByRole('dialog')).toBeNull()
 fireEvent.click(screen.getByRole('button', { name: 'Collapse connections' }))
 expect(screen.getByRole('textbox', { name: 'Assistant draft' })).toBe(assistant)
 expect((assistant as HTMLInputElement).value).toBe('Keep this question')
 expect((main as HTMLInputElement).value).toBe('Keep this policy')
 expect(screen.getByRole('complementary', { name: 'Assistant' })).toBeTruthy()
 await waitFor(() => expect(document.activeElement).toBe(opener))
})

it('hands focus to another setup without restoring a stale connection opener', async () => {
 renderShell(<AppShell><ConnectionOverlayFixture /></AppShell>)
 const opener = screen.getByRole('button', { name: 'Open connections' })
 fireEvent.click(opener)
 fireEvent.click(screen.getByRole('button', { name: 'Collapse connections' }))
 await waitFor(() => expect(document.activeElement).toBe(opener))
 const next = screen.getByRole('button', { name: 'Open another setup' })
 // A previously closed utility can still retain its old opener.
 fireEvent.click(next)
 await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
 expect(document.activeElement).toBe(next)
 fireEvent.click(opener)
 fireEvent.click(next)
 await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
 expect(document.activeElement).toBe(next)
 expect(screen.getByRole('complementary', { name: 'Assistant' })).toBeTruthy()
})

function WorkspaceToolsFixture() {
  const [open, setOpen] = useState(true), [width, setWidth] = useState(400)
  const presentation = useMemo(() => ({ title: 'Assistant', workspaceTools: true, available: true, open, onOpenChange: setOpen, width, onResize: setWidth, onReset: () => setWidth(400), minimumMainWidth: 480, maximumWidth: 640 }), [open, width])
  useInspectorPresentation(presentation)
  const assistant = useInspectorPortal(<label>Working message<textarea defaultValue="Unsent question" /></label>)
  const details = useDetailsPortal(<label>Detail note<input defaultValue="Keep note" /></label>)
  const detail = useDetailsSlot()
  const connection = useConnectionsPane()
  return <>{assistant}{details}<label>Main buffer<input defaultValue="Keep pack" /></label><button onClick={detail.reveal}>Inspect selection</button><button onClick={event => connection.open({ opener: event.currentTarget })}>Connect a source</button></>
}
it('switches full-height tools without remounting drafts and restores the previous tool after a utility', async () => {
  renderShell(<AppShell><WorkspaceToolsFixture /></AppShell>)
  const rail = await screen.findByRole('navigation', { name: 'Workspace tools' })
  const assistant = screen.getByRole('textbox', { name: 'Working message' }) as HTMLTextAreaElement
  const main = screen.getByRole('textbox', { name: 'Main buffer' })
  fireEvent.change(assistant, { target: { value: 'Preserve this question' } })
  fireEvent.click(screen.getByRole('button', { name: 'Inspect selection' }))
  expect(screen.getByRole('complementary', { name: 'Details' })).toBeTruthy()
  expect(screen.queryByRole('region', { name: 'Console' })).toBeNull()
  expect(screen.queryByRole('textbox', { name: 'Working message' })).toBeNull()
  expect(assistant.isConnected).toBe(true)
  expect(screen.queryByRole('button', { name: 'Pin details with Assistant' })).toBeNull()
  const note = screen.getByRole('textbox', { name: 'Detail note' })
  expect(document.querySelector('[data-split]')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Expand pane' }))
  expect(document.querySelector('[data-tool-overlay]')).toBeTruthy()
  expect(screen.getByRole('textbox', { name: 'Detail note' })).toBe(note)
  fireEvent.click(screen.getByRole('button', { name: 'Return to split view' }))
  fireEvent.click(within(rail).getByRole('button', { name: 'Activity' }))
  expect(screen.getByRole('tab', { name: 'File changes' })).toBeTruthy()
  expect(assistant.isConnected).toBe(true)
  fireEvent.click(within(rail).getByRole('button', { name: 'Details' }))
  fireEvent.click(screen.getByRole('button', { name: 'Connect a source' }))
  expect(screen.queryByRole('textbox', { name: 'Detail note' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Collapse connections' }))
  expect(screen.getByRole('textbox', { name: 'Detail note' })).toBe(note)
  fireEvent.click(within(rail).getByRole('button', { name: 'Assistant' }))
  expect(screen.getByRole('textbox', { name: 'Working message' })).toBe(assistant)
  expect(assistant.value).toBe('Preserve this question')
  expect(screen.getByRole('textbox', { name: 'Main buffer' })).toBe(main)
})


it('returns focus to the contextual action when Enter collapses its splitter', async () => {
  renderShell(<AppShell><WorkspaceToolsFixture /></AppShell>)
  const opener = screen.getByRole('button', { name: 'Inspect selection' })
  vi.spyOn(opener, 'getClientRects').mockReturnValue([{}] as unknown as DOMRectList)
  opener.focus()
  fireEvent.click(opener)
  const divider = screen.getByRole('separator', { name: 'Details' })
  divider.focus()
  fireEvent.keyDown(divider, { key: 'Enter' })
  expect(screen.queryByRole('complementary')).toBeNull()
  expect(document.activeElement).toBe(opener)
})


it('resizes the overlay independently, blocks background edits and returns without remounting', async () => {
  renderShell(<AppShell><WorkspaceToolsFixture /></AppShell>)
  const main = screen.getByRole('textbox', {name: 'Main buffer'})
  const assistant = await screen.findByRole('textbox', {name: 'Working message'})
  const splitWidth = screen.getByRole('separator', {name: 'Assistant'}).getAttribute('aria-valuenow')
  fireEvent.change(assistant, {target: {value: 'Keep my unsent question'}})
  fireEvent.click(screen.getByRole('button', {name: 'Expand pane'}))
  expect(main.closest('main')?.hasAttribute('inert')).toBe(true)
  const divider = screen.getByRole('separator', {name: 'Assistant'})
  const initial = Number(divider.getAttribute('aria-valuenow'))
  fireEvent.keyDown(divider, {key: 'ArrowLeft'})
  expect(divider.getAttribute('aria-valuenow')).toBe(String(initial + 8))
  fireEvent.keyDown(divider, {key: 'Home'})
  expect(divider.getAttribute('aria-valuenow')).toBe('640')
  fireEvent.keyDown(divider, {key: 'End'})
  expect(divider.getAttribute('aria-valuenow')).toBe(divider.getAttribute('aria-valuemax'))
  fireEvent.click(screen.getByRole('button', {name: 'Return to split view'}))
  expect(main.closest('main')?.hasAttribute('inert')).toBe(false)
  expect(screen.getByRole('separator', {name: 'Assistant'}).getAttribute('aria-valuenow')).toBe(splitWidth)
  expect(screen.getByRole('textbox', {name: 'Working message'})).toBe(assistant)
  expect((assistant as HTMLTextAreaElement).value).toBe('Keep my unsent question')
  expect(screen.getByRole('textbox', {name: 'Main buffer'})).toBe(main)
  fireEvent.click(screen.getByRole('button', {name: 'Expand pane'}))
  expect(screen.getByRole('separator', {name: 'Assistant'}).getAttribute('aria-valuenow')).toBe(divider.getAttribute('aria-valuemax'))
  fireEvent.click(document.querySelector('.desk-pane-backdrop')!)
  expect(document.querySelector('[data-tool-overlay]')).toBeNull()
  fireEvent.click(screen.getByRole('button', {name: 'Expand pane'}))
  fireEvent.keyDown(assistant, {key: 'Escape'})
  expect(document.querySelector('[data-tool-overlay]')).toBeNull()
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', {name: 'Expand pane'})))
})
