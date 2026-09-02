/**
 * The console: two channels with a real feed, and two that say so.
 *
 * Both of the traps `radixGround.test.tsx` records are live here. A Radix tab
 * switches on **mousedown**, not on click; and an inactive `Tabs.Content`
 * keeps its element and loses its children, so a channel's entries are simply
 * absent until its tab is the active one. A test written the obvious way would
 * assert on a tab that never switched and a panel that never rendered.
 */
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { connected } from '../testing/harness'
import { BottomPane } from './BottomPane'
import { forgetConsole, recordFileChange } from './consoleLog'

afterEach(() => {
  cleanup()
  forgetConsole()
})

function renderConsole(overrides = {}, tab: 'connection' | 'calls' | 'files' | 'notices' = 'connection') {
  const value = connected(overrides)
  return render(
    <McpContext.Provider value={value}>
      <BottomPane open tab={tab} onTabChange={() => {}} />
    </McpContext.Provider>
  )
}

describe('the console', () => {
  it('offers the four channels in the artboard’s order', () => {
    renderConsole()
    expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual([
      'Connection',
      'Calls',
      'Files',
      'Notices'
    ])
  })

  it('records one line per connection transition, not two under StrictMode', () => {
    const { rerender } = renderConsole()
    const value = connected()
    // A second render with the same connection: the effect runs again and the
    // store drops the identical line rather than double-reporting a state the
    // connection entered once.
    rerender(
      <McpContext.Provider value={value}>
        <BottomPane open tab="connection" onTabChange={() => {}} />
      </McpContext.Provider>
    )
    expect(screen.getAllByText(/ready · connection 1/)).toHaveLength(1)
  })

  it('shows a reported file change on the Files channel, by path', () => {
    renderConsole({}, 'files')
    // Through `act`, because the store publishes outside React's own dispatch
    // — a notification from the socket arrives the same way in the page.
    act(() => recordFileChange('packs/intake-triage.json'))
    expect(screen.getByText('packs/intake-triage.json')).toBeTruthy()
  })

  it('says the two unbuilt channels arrive later, and fabricates no rows', () => {
    renderConsole({}, 'calls')
    expect(screen.getByText('This channel arrives later.')).toBeTruthy()
    // No table, no columns, no plausible traffic.
    expect(screen.queryByRole('table')).toBeNull()
    expect(screen.queryByText(/ms/)).toBeNull()
  })

  it('leaves the log list out of the live region', () => {
    renderConsole()
    const list = screen.getByRole('list')
    expect(list.getAttribute('aria-live')).toBe('off')
  })

  it('is absent from the accessibility tree when collapsed', () => {
    render(
      <McpContext.Provider value={connected()}>
        <BottomPane open={false} tab="connection" onTabChange={() => {}} />
      </McpContext.Provider>
    )
    expect(screen.queryByRole('region', { name: 'Console' })).toBeNull()
  })
})
