import { StrictMode, useLayoutEffect } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DESK_DEFAULTS, type ThemeChoice } from '../config/deskConfig'
import { acknowledgeEntryTheme, chooseEntryTheme, pendingEntryTheme } from '../config/entryTheme'
import { useAppliedTheme } from '../config/theme'
import { forgetSession, resetSessionForTesting, sessionStorageKey } from '../mcp/session'
import { AppearanceProvider } from '../shell/appearanceState'
import { SessionGate } from './SessionGate'

// These boundary tests retain real authentication transitions while isolating
// the independently tested public identity directory.
vi.mock('./flow', async importActual => {
 const actual = await importActual<typeof import('./flow')>()
 return {...actual, authCall: (path: string, body?: unknown, bearer?: string, signal?: AbortSignal) => path === 'desks'
  ? Promise.resolve({startup:'',desks:[{id:'',name:'Unveil',mark:null}]})
  : actual.authCall(path, body, bearer, signal)}
})
import { SessionPage } from './SessionPage'

beforeEach(() => resetSessionForTesting())
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  resetSessionForTesting()
  const pending = pendingEntryTheme()
  if (pending) acknowledgeEntryTheme(pending)
  localStorage.clear()
  sessionStorage.clear()
  document.documentElement.removeAttribute('data-theme')
})

function watchThemes() {
  const applied: Array<string | null> = []
  const root = document.documentElement
  const set = root.setAttribute.bind(root)
  const remove = root.removeAttribute.bind(root)
  vi.spyOn(root, 'setAttribute').mockImplementation((name, value) => {
    if (name === 'data-theme') applied.push(value)
    set(name, value)
  })
  vi.spyOn(root, 'removeAttribute').mockImplementation(name => {
    if (name === 'data-theme') applied.push(null)
    remove(name)
  })
  return applied
}

function mountGate() {
  let finish!: (response: Response) => void
  const session = new Promise<Response>(resolve => { finish = resolve })
  sessionStorage.setItem(sessionStorageKey(), 'a'.repeat(48))
  vi.stubGlobal('fetch', (url: unknown) => String(url) === '/api/auth/status'
    ? Promise.resolve(Response.json({ enabled: true, unavailable: false, sessionActive: true, mode: 'password' }))
    : session)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<StrictMode><QueryClientProvider client={client}><SessionGate>
    <AppearanceProvider projectIdentity="/workspace/a" projectDefault={DESK_DEFAULTS.appearance} projectDefaultKnown>
      <div>Private workspace</div>
    </AppearanceProvider>
  </SessionGate></QueryClientProvider></StrictMode>)
  return finish
}

it('does not apply an old entry Light during actual checking → ready with a System workspace', async () => {
  localStorage.setItem('jpack-desk:entry-theme', 'light')
  const applied = watchThemes()
  const finish = mountGate()
  expect(screen.getByRole('status').textContent).toBe('Opening Desk…')
  expect(applied).toEqual([])
  await act(async () => finish(Response.json({ subject: 'local user', issuer: null })))
  await screen.findByText('Private workspace')
  expect(applied).not.toContain('light')
  expect(document.documentElement.hasAttribute('data-theme')).toBe(false)

  // Confirmed recovery is where the saved public preference belongs.
  act(() => forgetSession())
  await screen.findByRole('heading', { name: 'Sign in to Unveil' })
  expect(document.documentElement.getAttribute('data-theme')).toBe('light')
})

it('reads the public preference only after actual checking → recovery', async () => {
  localStorage.setItem('jpack-desk:entry-theme', 'light')
  const applied = watchThemes()
  const finish = mountGate()
  expect(applied).toEqual([])
  // Read at recovery, rather than a stale snapshot from checking's mount.
  localStorage.setItem('jpack-desk:entry-theme', 'dark')
  await act(async () => finish(new Response(null, { status: 401 })))
  await screen.findByRole('heading', { name: 'Sign in to Unveil' })
  expect(applied).not.toContain('light')
  expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
})

it('honors a fresh explicit entry handoff while checking without consuming it', () => {
  chooseEntryTheme('dark')
  const pending = pendingEntryTheme()
  render(<StrictMode><SessionPage state="checking" /></StrictMode>)
  expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
  expect(pendingEntryTheme()).toBe(pending)
})

it.each(['light', 'dark', 'system'] as const)('applies %s before later layout effects in the same commit', theme => {
  const observed: Array<string | null> = []
  function Owner({ theme }: { theme: ThemeChoice }) {
    useAppliedTheme(theme)
    useLayoutEffect(() => { observed.push(document.documentElement.getAttribute('data-theme')) }, [theme])
    return null
  }
  document.documentElement.setAttribute('data-theme', theme === 'light' ? 'dark' : 'light')
  render(<StrictMode><Owner theme={theme} /></StrictMode>)
  expect(observed.length).toBeGreaterThan(0)
  expect(observed.every(value => value === (theme === 'system' ? null : theme))).toBe(true)
})

it.each(['dark', 'system'] as const)('does not let obsolete cleanup reset a newer %s owner, including equal themes', theme => {
  function Owner() { useAppliedTheme(theme); return null }
  const previous = render(<StrictMode><Owner /></StrictMode>)
  const current = render(<StrictMode><Owner /></StrictMode>)
  const applied = watchThemes()
  previous.unmount()
  expect(applied).toEqual([])
  expect(document.documentElement.getAttribute('data-theme')).toBe(theme === 'system' ? null : theme)
  current.unmount()
  expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
})
