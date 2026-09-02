/**
 * Help & About: the runtime's own words, and the desk's own limits.
 *
 * The prompt case is the one with a boundary behind it. The page renders
 * `author_pack`'s text verbatim for a person to carry elsewhere; it does not
 * run it, and there is no model key anywhere in this desk to run it with.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { SHORTCUTS } from '../shell/shortcuts'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { HelpAbout } from './HelpAbout'

afterEach(cleanup)

const PROMPT_TEXT = 'Encode ONE policy decision as a Judgment Pack (declare specVersion …).'

function renderHelp(stub: ReturnType<typeof stubClient>, overrides: Partial<McpConnection> = {}) {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: stub.client, ...overrides })}>
            <HelpAbout />
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: ['/help'] }
  )
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

const PACKS = { list_packs: () => ({ text: JSON.stringify({ packs: [], configPath: '/p/jpack.json' }) }) }

describe('Help & About', () => {
  it('names the connected runtime and whether its listing was read', () => {
    renderHelp(stubClient(PACKS), { known: false })
    expect(screen.getAllByText('jpack').length).toBeGreaterThan(0)
    expect(screen.getByText(/not read — every capability below is unknown, not absent/)).toBeTruthy()
  })

  it('renders the shortcut list from the one typed array', () => {
    renderHelp(stubClient(PACKS))
    for (const shortcut of SHORTCUTS) {
      expect(screen.getByText(shortcut.keys)).toBeTruthy()
    }
  })

  it('documents the macOS collision rather than shipping a silent no-op', () => {
    renderHelp(stubClient(PACKS))
    expect(screen.getByText(/developer tools before the page sees them/)).toBeTruthy()
  })

  it('states the one place Escape does close a pane', () => {
    renderHelp(stubClient(PACKS))
    expect(screen.getByText(/below 1100px the Inspector is rendered as a drawer/)).toBeTruthy()
  })

  it('renders the runtime’s author_pack text verbatim where it is advertised', async () => {
    renderHelp(stubClient(PACKS, { prompts: { author_pack: { text: PROMPT_TEXT } } }))
    expect(await screen.findByText(PROMPT_TEXT)).toBeTruthy()
    expect(screen.getByText(/holds no model key, calls no model, and executes no prompt/)).toBeTruthy()
  })

  it('says so plainly where the runtime advertises no such prompt', async () => {
    renderHelp(stubClient(PACKS))
    expect(await screen.findByText(/advertises no/)).toBeTruthy()
    expect(screen.queryByText(PROMPT_TEXT)).toBeNull()
  })

  it('carries the true sentence about where the session token lives', () => {
    renderHelp(stubClient(PACKS))
    // Not "it leaves the URL immediately": nothing calls history.replaceState,
    // so it leaves at the first in-app navigation and the page says that.
    expect(screen.getByText(/leaves the address bar at the first in-app navigation/)).toBeTruthy()
  })
})
