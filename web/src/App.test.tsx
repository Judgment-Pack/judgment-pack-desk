import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { App } from './App'
import { connected, renderConnected, stubClient } from './testing/harness'

afterEach(cleanup)

/** An empty project still starts at the chat landing page. */
const EMPTY_PROJECT = stubClient({
  list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [] }) }),
  experimental_test_graphs: () => ({
    text: JSON.stringify({ status: 'skipped', summary: { total: 0, passed: 0, mismatched: 0 } })
  })
})

describe('the desk, connected', () => {
  it('opens the chat landing page without a warning when the tool listing was read', async () => {
    renderConnected(<App />, connected({ client: EMPTY_PROJECT.client, known: true }))
    await screen.findByText('Loading chat history…')
    expect(screen.queryByText(/tool listing could not be read/)).toBeNull()
  })

  it('opens the Library collection at /library', async () => {
    renderConnected(<App />, connected({ client: EMPTY_PROJECT.client, known: true }), { path: '/library' })
    await screen.findByRole('heading', { name: /^Library/ })
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
    fireEvent.click(await screen.findByRole('button', {name:'Connection status: Limited connection'}))
    expect(await screen.findByText('Some features are unavailable because Desk could not load runtime capabilities.')).toBeTruthy()
    expect(screen.getByText('the request timed out')).toBeTruthy()
  })
})

it('keeps Help and connection recovery reachable after runtime failure', async () => {
  renderConnected(<App />, connected({status: 'failed', error: new Error('Runtime offline'), everConnected: false}), {path: '/help'})
  expect(await screen.findByRole('heading', {name: 'Help & About', level: 1})).toBeTruthy()
  fireEvent.click(screen.getByRole('button', {name: 'Connection status: not connected'}))
  expect(await screen.findByRole('button', {name: 'Retry connection'})).toBeTruthy()
})
