import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { App } from './App'
import { connected, renderConnected, stubClient } from './testing/harness'

afterEach(cleanup)

/** A project with nothing in it: the home page renders, and says so. */
const EMPTY_PROJECT = stubClient({
  list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [] }) }),
  experimental_test_graphs: () => ({
    text: JSON.stringify({ status: 'skipped', summary: { total: 0, passed: 0, mismatched: 0 } })
  })
})

describe('the desk, connected', () => {
  it('says so and shows no banner when the tool listing was read', async () => {
    renderConnected(<App />, connected({ client: EMPTY_PROJECT.client, known: true }))
    await screen.findByRole('heading', { name: 'This project' })
    expect(screen.queryByText(/tool listing could not be read/)).toBeNull()
  })

  it('says the tool listing could not be read rather than impersonating an older runtime', async () => {
    // Every feature-detected capability is off in this state. Off alone would
    // have the page claiming the runtime lacks the tools, which is a claim about
    // the runtime that a failed listing never made.
    renderConnected(
      <App />,
      connected({
        client: EMPTY_PROJECT.client,
        known: false,
        capabilitiesError: new Error('the request timed out')
      })
    )
    const banner = await screen.findByText(/tool listing could not be read/)
    expect(banner.textContent).toContain('the request timed out')
    expect(banner.textContent).toContain('unknown rather than known to be little')
  })
})
