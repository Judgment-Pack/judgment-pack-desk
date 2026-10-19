import { StrictMode, useState } from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { AppearanceConfig, ThemeChoice } from '../config/deskConfig'
import { acknowledgeEntryTheme, pendingEntryTheme } from '../config/entryTheme'
import { AppearanceProvider, appearanceKey, useAppearance } from '../shell/appearanceState'
import { projectKey } from '../shell/paneState'
import { SessionPage } from './SessionPage'

const PROJECT = '/home/someone/project-a'
const KEY = appearanceKey(projectKey(PROJECT))
const OTHER_PROJECT = '/home/someone/project-b'
const OTHER_KEY = appearanceKey(projectKey(OTHER_PROJECT))
const LIGHT_COMPACT: AppearanceConfig = { theme: 'light', density: 'compact' }

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  const pending = pendingEntryTheme()
  if (pending) acknowledgeEntryTheme(pending)
  window.localStorage.clear()
  window.sessionStorage.clear()
  document.documentElement.removeAttribute('data-theme')
  document.documentElement.removeAttribute('data-density')
})

function Workspace() {
  const appearance = useAppearance()
  const [reset, setReset] = useState('')
  return <>
    <p data-testid="theme">{appearance.theme ?? 'unknown'}</p>
    <p data-testid="density">{appearance.density ?? 'unknown'}</p>
    <button onClick={() => setReset(appearance.restoreProjectDefault())}>Use project default</button>
    <p data-testid="reset">{reset}</p>
  </>
}

interface ProjectAnswers {
  projectIdentity?: string
  projectDefault: AppearanceConfig
  projectDefaultKnown: boolean
}

function boundary(authenticated: boolean, options: Partial<ProjectAnswers> = {}) {
  const answers = { projectIdentity: PROJECT, projectDefault: LIGHT_COMPACT, projectDefaultKnown: true, ...options }
  return <StrictMode>{authenticated ? <AppearanceProvider
    {...answers}
  ><Workspace /></AppearanceProvider> : <SessionPage state="unavailable" />}</StrictMode>
}

function choose(theme: ThemeChoice) {
  fireEvent.keyDown(screen.getByRole('combobox', { name: 'Appearance' }), { key: 'ArrowDown' })
  fireEvent.click(screen.getByRole('option', { name: { dark: 'Dark', light: 'Light', system: 'System' }[theme] }))
}

function expectTheme(theme: ThemeChoice) {
  expect(screen.getByTestId('theme').textContent).toBe(theme)
  expect(document.documentElement.getAttribute('data-theme')).toBe(theme === 'system' ? null : theme)
}

it.each([
  ['dark', 'light'],
  ['light', 'dark'],
  ['system', 'dark']
] as const)('keeps an explicit %s login choice over a %s project default', (theme: ThemeChoice, projectTheme: ThemeChoice) => {
  // Start away from the chosen value so Radix reports a real selection.
  window.localStorage.setItem('jpack-desk:entry-theme', theme === 'dark' ? 'light' : 'dark')
  const projectDefault: AppearanceConfig = { theme: projectTheme, density: 'compact' }
  const view = render(boundary(false, { projectDefault }))
  choose(theme)
  expect(document.documentElement.getAttribute('data-theme')).toBe(theme === 'system' ? null : theme)

  view.rerender(boundary(true, { projectDefault }))
  expectTheme(theme)
  expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({ v: 1, theme })
})

it.each(['pointer', 'Enter', ' '] as const)('honors an explicit same-value login selection using %j', activation => {
  window.localStorage.setItem('jpack-desk:entry-theme', 'dark')
  const view = render(boundary(false))
  fireEvent.keyDown(screen.getByRole('combobox', { name: 'Appearance' }), { key: 'Enter' })
  const selected = screen.getByRole('option', { name: 'Dark' })
  if (activation === 'pointer') {
    fireEvent.pointerDown(selected, { pointerType: 'mouse' })
    fireEvent.pointerUp(selected, { pointerType: 'mouse' })
    fireEvent.click(selected)
  } else {
    fireEvent.keyDown(selected, { key: activation })
  }
  expect(screen.queryByRole('listbox')).toBeNull()

  view.rerender(boundary(true))
  expectTheme('dark')
  expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({ v: 1, theme: 'dark' })
})

it('keeps a login choice made by typing on the closed appearance trigger', () => {
  window.localStorage.setItem('jpack-desk:entry-theme', 'dark')
  const projectDefault: AppearanceConfig = { theme: 'dark', density: 'compact' }
  const view = render(boundary(false, { projectDefault }))
  const trigger = screen.getByRole('combobox', { name: 'Appearance' })
  fireEvent.keyDown(trigger, { key: 'l' })
  expect(trigger.textContent).toContain('Light')
  expect(screen.queryByRole('listbox')).toBeNull()

  view.rerender(boundary(true, { projectDefault }))
  expectTheme('light')
  expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({ v: 1, theme: 'light' })
})

it.each(['root first', 'config first'] as const)('holds the login choice until project resolution, with %s', order => {
  window.localStorage.setItem(KEY, JSON.stringify({ v: 1, theme: 'light', density: 'comfortable' }))
  const view = render(boundary(false))
  choose('dark')
  view.rerender(boundary(true, { projectIdentity: undefined, projectDefaultKnown: false }))
  expectTheme('dark')
  expect(screen.getByTestId('density').textContent).toBe('unknown')
  expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({ v: 1, theme: 'light', density: 'comfortable' })
  expect(window.localStorage.getItem(appearanceKey('default'))).toBeNull()

  view.rerender(boundary(true, order === 'root first'
    ? { projectDefaultKnown: false }
    : { projectIdentity: undefined }))
  expectTheme('dark')
  view.rerender(boundary(true))
  expectTheme('dark')
  expect(screen.getByTestId('density').textContent).toBe('comfortable')
  expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({ v: 1, theme: 'dark', density: 'comfortable' })
})

it('keeps the explicit choice for this visit when browser storage is denied', () => {
  const denied = {
    getItem: () => { throw new Error('site data is blocked') },
    setItem: () => { throw new Error('site data is blocked') },
    removeItem: () => { throw new Error('site data is blocked') }
  }
  vi.stubGlobal('localStorage', denied)
  vi.stubGlobal('sessionStorage', denied)
  const view = render(boundary(false))
  choose('dark')
  view.rerender(boundary(true, { projectIdentity: undefined }))
  expectTheme('dark')
  view.rerender(boundary(true))
  expectTheme('dark')
  expect(screen.getByTestId('density').textContent).toBe('compact')
})

it.each([true, false])('does not transfer an old saved entry preference when a project record exists: %s', stored => {
  window.localStorage.setItem('jpack-desk:entry-theme', 'dark')
  if (stored) window.localStorage.setItem(KEY, JSON.stringify({ v: 1, theme: 'system' }))
  const view = render(boundary(false))
  expect(screen.getByRole('combobox', { name: 'Appearance' }).textContent).toContain('Dark')
  view.rerender(boundary(true))
  expectTheme(stored ? 'system' : 'light')
  expect(window.localStorage.getItem(KEY)).toBe(stored ? JSON.stringify({ v: 1, theme: 'system' }) : null)
})

it('transfers a new choice after logout without reusing the previous handoff', () => {
  const view = render(boundary(false))
  choose('dark')
  view.rerender(boundary(true))
  expectTheme('dark')

  view.rerender(boundary(false))
  window.localStorage.setItem(KEY, JSON.stringify({ v: 1, theme: 'light' }))
  view.rerender(boundary(true))
  expectTheme('light')

  view.rerender(boundary(false))
  choose('system')
  view.rerender(boundary(true))
  expectTheme('system')
  expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({ v: 1, theme: 'system' })
})

it('discards the login choice after leaving its project and reads that project afresh on return', () => {
  window.localStorage.setItem(OTHER_KEY, JSON.stringify({ v: 1, theme: 'system' }))
  const view = render(boundary(false))
  choose('dark')
  view.rerender(boundary(true))
  expectTheme('dark')

  view.rerender(boundary(true, { projectIdentity: OTHER_PROJECT }))
  expectTheme('system')
  expect(JSON.parse(window.localStorage.getItem(OTHER_KEY)!)).toEqual({ v: 1, theme: 'system' })
  window.localStorage.setItem(KEY, JSON.stringify({ v: 1, theme: 'light' }))
  view.rerender(boundary(true))
  expectTheme('light')
  expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({ v: 1, theme: 'light' })
})

it('does not restore a reset login choice after project changes or another login', () => {
  const view = render(boundary(false))
  choose('dark')
  view.rerender(boundary(true))
  fireEvent.click(screen.getByRole('button', { name: 'Use project default' }))
  expect(screen.getByTestId('reset').textContent).toBe('cleared')
  expectTheme('light')
  expect(window.localStorage.getItem(KEY)).toBeNull()

  view.rerender(boundary(true, { projectIdentity: OTHER_PROJECT }))
  expectTheme('light')
  expect(window.localStorage.getItem(OTHER_KEY)).toBeNull()
  view.rerender(boundary(true))
  expectTheme('light')
  view.rerender(boundary(false))
  view.rerender(boundary(true))
  expectTheme('light')
  expect(window.localStorage.getItem(KEY)).toBeNull()
})

it('keeps the fresh choice across the document reload performed by sign-in', async () => {
  const entry = render(boundary(false))
  choose('dark')
  entry.unmount()

  // A new document loses module state but retains this tab's sessionStorage.
  vi.resetModules()
  const { AppearanceProvider: ReloadedProvider, useAppearance: useReloadedAppearance } = await import('../shell/appearanceState')
  function ReloadedWorkspace() {
    return <p data-testid="reloaded-theme">{useReloadedAppearance().theme}</p>
  }
  const workspace = render(<StrictMode><ReloadedProvider
    projectIdentity={PROJECT}
    projectDefault={LIGHT_COMPACT}
    projectDefaultKnown
  ><ReloadedWorkspace /></ReloadedProvider></StrictMode>)
  expect(screen.getByTestId('reloaded-theme').textContent).toBe('dark')
  expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
  expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({ v: 1, theme: 'dark' })
  workspace.unmount()

  // A later document receives its own project's preference/default, rather
  // than applying the already accepted handoff to another project.
  vi.resetModules()
  const next = await import('../shell/appearanceState')
  function NextWorkspace() {
    return <p data-testid="next-theme">{next.useAppearance().theme}</p>
  }
  render(<StrictMode><next.AppearanceProvider
    projectIdentity={OTHER_PROJECT}
    projectDefault={LIGHT_COMPACT}
    projectDefaultKnown
  ><NextWorkspace /></next.AppearanceProvider></StrictMode>)
  expect(screen.getByTestId('next-theme').textContent).toBe('light')
  expect(window.localStorage.getItem(OTHER_KEY)).toBeNull()
})
