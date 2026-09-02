/**
 * The blocked branch, which had no test before the extraction.
 *
 * `App.tsx` read `blocked && error ?`, and the second half of that is easy to
 * lose in a move: a connection that is blocked but carries **no** error still
 * renders the routes. Losing it would blank the desk on a state that has
 * nothing to say.
 */
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { connected } from '../testing/harness'
import { BlockedNotice, ConnectionNotices, useBlockingError } from './ConnectionNotices'

afterEach(cleanup)

/** The shape `App.tsx` has: notices, then either the reason or the routes. */
function Page() {
  const blocking = useBlockingError()
  return (
    <>
      <ConnectionNotices />
      {blocking ? <BlockedNotice error={blocking} /> : <p>the routes</p>}
    </>
  )
}

function renderPage(overrides: Partial<McpConnection>) {
  return render(
    <McpContext.Provider value={connected(overrides)}>
      <Page />
    </McpContext.Provider>
  )
}

describe('the connection notices', () => {
  it('takes the page when the connection failed with a reason', () => {
    renderPage({ status: 'failed', error: new Error('no session token'), everConnected: false })
    expect(screen.getByRole('alert').textContent).toContain('Not connected to the runtime')
    expect(screen.getByRole('alert').textContent).toContain('no session token')
    expect(screen.queryByText('the routes')).toBeNull()
  })

  it('renders the routes behind a banner once the desk has been connected', () => {
    renderPage({ status: 'reconnecting', error: new Error('closed'), everConnected: true, attempt: 3 })
    expect(screen.getByRole('status').textContent).toContain('Lost the connection to the chassis')
    expect(screen.getByRole('status').textContent).toContain('attempt 3')
    // The reconnect is automatic; throwing the view away would lose the
    // user's place over a local restart.
    expect(screen.getByText('the routes')).toBeTruthy()
  })

  it('renders the routes when the connection is blocked but carries no error', () => {
    renderPage({ status: 'failed', error: null, everConnected: false })
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByText('the routes')).toBeTruthy()
  })

  it('says the tool listing could not be read rather than impersonating an older runtime', () => {
    renderPage({ status: 'ready', known: false, capabilitiesError: new Error('the request timed out') })
    const banner = screen.getByRole('status')
    expect(banner.textContent).toContain('tool listing could not be read')
    expect(banner.textContent).toContain('the request timed out')
    expect(banner.textContent).toContain('unknown rather than known to be little')
  })

  it('offers a retry on the blocked reason while a reconnect is pending', () => {
    renderPage({ status: 'reconnecting', error: new Error('closed'), everConnected: false, attempt: 2 })
    expect(screen.getByText(/Retrying automatically \(attempt 2\)/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Try now' })).toBeTruthy()
  })
})
