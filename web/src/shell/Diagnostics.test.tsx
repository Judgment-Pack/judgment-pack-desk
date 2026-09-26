import { languageReady, setLanguage } from '../i18n'
/** Diagnostic channels preserve recorded events without inventing traffic.
 * Radix tab changes use mousedown, and inactive channels stay mounted.
 */
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { connected } from '../testing/harness'
import { Diagnostics, useConnectionLog } from './Diagnostics'
import { StrictMode } from 'react'
import { forgetConsole, recordFileChange, recordActivity } from './consoleLog'

afterEach(async () => {
  cleanup()
  forgetConsole()
  setLanguage('en'); await languageReady()
})

function LogFixture() { useConnectionLog(); return <Diagnostics /> }

function renderDiagnostics(overrides = {}, tab: 'connection' | 'calls' | 'files' = 'connection') {
  const value = connected(overrides)
  const view = render(
    <McpContext.Provider value={value}>
      <StrictMode><LogFixture /></StrictMode>
    </McpContext.Provider>
  )
  if (tab !== 'connection') fireEvent.mouseDown(screen.getByRole('tab', { name: tab === 'files' ? 'File changes' : 'Activity' }), { button: 0, ctrlKey: false })
  return view
}

describe('diagnostics', () => {
  it('offers the three populated diagnostic channels', () => {
    renderDiagnostics()
    expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual([
      'Connection',
      'Activity',
      'File changes',
    ])
  })

  it('keeps tab bodies in bounded scroll regions', () => {
    const { container } = renderDiagnostics()
    expect(container.querySelector('[data-pane-tabs]')).toBeTruthy()
    expect(container.querySelectorAll('[data-pane-scroll]')).toHaveLength(3)
  })

  it('records one line per connection transition, not two under StrictMode', () => {
    const { rerender } = renderDiagnostics()
    const value = connected()
    // A second render with the same connection: the effect runs again and the
    // store drops the identical line rather than double-reporting a state the
    // connection entered once.
    rerender(
      <McpContext.Provider value={value}>
        <StrictMode><LogFixture /></StrictMode>
      </McpContext.Provider>
    )
    expect(screen.getAllByText(/ready · connection 1/)).toHaveLength(1)
  })

  it('shows a reported file change on the Files channel, by path', () => {
    renderDiagnostics({}, 'files')
    // Through `act`, because the store publishes outside React's own dispatch
    // — a notification from the socket arrives the same way in the page.
    act(() => recordFileChange('packs/intake-triage.json'))
    expect(screen.getByText('packs/intake-triage.json')).toBeTruthy()
  })

  it('starts Activity empty, then shows only recorded milestones', () => {
    renderDiagnostics({}, 'calls')
    expect(screen.getByText('No operations recorded yet.')).toBeTruthy()
    act(() => recordActivity('Pack created and registered.'))
    expect(screen.getByText('Pack created and registered.')).toBeTruthy()
    // No table, no columns, no plausible traffic.
    expect(screen.queryByRole('table')).toBeNull()
    expect(screen.queryByText(/ms/)).toBeNull()
  })

  it('leaves the log list out of the live region', () => {
    renderDiagnostics()
    const list = screen.getByRole('list')
    expect(list.getAttribute('aria-live')).toBe('off')
  })


})


describe('console localization', () => {
  it('translates recorded milestones when the language changes', async () => {
    renderDiagnostics({}, 'calls')
    act(() => recordActivity('Pack created and registered.'))
    await act(async () => { setLanguage('fr'); await languageReady() })
    expect(screen.getByText('Pack créé et enregistré.')).toBeTruthy()
    await act(async () => { setLanguage('ja'); await languageReady() })
    expect(screen.getByText('パックを作成して登録しました。')).toBeTruthy()
  })
  it('preserves file paths even if a name matches a translated UI message', async () => {
    renderDiagnostics({}, 'files')
    act(() => recordFileChange('Pack created and registered.'))
    await act(async () => { setLanguage('fr'); await languageReady() })
    expect(screen.getByText('Pack created and registered.')).toBeTruthy()
    expect(screen.queryByText('Pack créé et enregistré.')).toBeNull()
  })
})
