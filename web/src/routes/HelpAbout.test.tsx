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
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture, DeskConfigProvider, useDeskConfigRead } from '../config/DeskConfigProvider'
import { effectiveConfig, type EffectiveConfig } from '../config/deskConfig'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { SHORTCUTS } from '../shell/shortcuts'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { HelpAbout } from './HelpAbout'
import { GatesHelp, outsideAgentCommand } from './GatesHelp'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

const PROMPT_TEXT = 'Encode ONE policy decision as a Judgment Pack (declare specVersion …).'

function renderHelp(
  stub: ReturnType<typeof stubClient>,
  overrides: Partial<McpConnection> = {},
  config: EffectiveConfig = effectiveConfig(undefined)
) {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: stub.client, ...overrides })}>
            <DeskConfigFixture value={config}>
              <HelpAbout />
            </DeskConfigFixture>
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

  it('reads the connection off its status, not off the runtime it last met', () => {
    // The provider retains `server` across a reconnect, so naming the runtime
    // off its presence said "connected" while the socket was down.
    renderHelp(stubClient(PACKS), { status: 'reconnecting', client: null, attempt: 2 })
    const connection = screen.getByText(/^Runtime:/).parentElement!
    expect(connection.textContent).toContain('reconnecting')
    expect(connection.textContent).not.toContain('jpack test')
    cleanup()

    renderHelp(stubClient(PACKS), { status: 'failed', client: null, server: null })
    expect(screen.getByText(/^Runtime:/).parentElement!.textContent).toContain('not connected')
  })

  it('carries the runtime facts the Admin card used to hold', () => {
    // The card is gone from Admin — none of its four slots was a setting — and
    // its content has to have a home. The binary comes from the chassis; the
    // summary is the connection's own, `known` carried rather than folded in.
    renderHelp(
      stubClient(PACKS),
      { known: false },
      effectiveConfig(undefined, undefined, undefined, {
        path: '/home/someone/.config/jpack-desk/desk.json',
        present: false,
        sha256: '',
        chassis: {
          projectDir: '/real/a-project',
          projectFile: '/real/a-project/jpack-desk.json',
          runtimeBin: '/usr/local/bin/jpack'
        }
      })
    )
    expect(screen.getByText('/usr/local/bin/jpack')).toBeTruthy()
    const summary = screen.getByText(/"rehearsalSupported"/)
    expect(summary.textContent).toContain('"known": false')
    expect(summary.textContent).toContain('"status": "ready"')
  })

  it.each(['ready', 'external', 'unavailable'] as const)('only labels a running local gateway: %s', status => {
    renderHelp(stubClient(PACKS), {}, effectiveConfig(undefined, undefined, undefined, {
      path: '/private/desk.json', present: false,
      localGateway: { status, build: { version: 'v0.3.1', revision: '1ab277d127ba6ed60a4ede2c970742b04121d247' } }
    }))
    expect(screen.queryByText('v0.3.1') !== null).toBe(status === 'ready')
    expect(screen.queryByText('1ab277d127ba6ed60a4ede2c970742b04121d247') !== null).toBe(status === 'ready')
  })

  it('shows component identities separately and keeps full startup metadata available', () => {
    renderHelp(stubClient(PACKS), { server: { name: 'jpack', version: '0.22.0-dev+6842494' } }, effectiveConfig(undefined, undefined, undefined, {
      path: '/config/desk.json', present: false,
      chassis: { projectDir: '/p', projectFile: '/p/jpack-desk.json', runtimeBin: '/bin/jpack', builds: {
        desk: { moduleVersion: 'v0.0.0-20260926030000-798ef45c6275+dirty', revision: '798ef45c6275', modified: true },
        runtime: { revision: '6842494ff049' }, runner: { revision: '9b739a344e25' }
      } }
    }))
    expect(screen.getByText('798ef45')).toBeTruthy()
    expect(screen.getByText('9b739a3')).toBeTruthy()
    expect(screen.getByText(/Local changes/)).toBeTruthy()
    expect(screen.getByText(/^Runtime:/).textContent).toContain('0.22.0-dev+6842494')
    expect(screen.getByText(/"revision": "798ef45c6275"/)).toBeTruthy()
  })

  it('distinguishes an unconfigured Runner from unavailable build metadata', () => {
    const config = effectiveConfig(undefined, undefined, undefined, {
      path: '/config/desk.json', present: false,
      chassis: { projectDir: '/p', projectFile: '/p/jpack-desk.json', runtimeBin: '/bin/jpack', builds: { desk: { moduleVersion: 'v1.2.3' }, runtime: {} } }
    })
    renderHelp(stubClient(PACKS), {}, config)
    expect(screen.getByText('v1.2.3')).toBeTruthy()
    expect(screen.getByText(/Runner:/).textContent).toBe('Runner: Not configured')
    cleanup()
    renderHelp(stubClient(PACKS))
    expect(screen.getByText(/Runner:/).textContent).toBe('Runner: Unknown')
  })

  it('says the desk has not named a runtime binary rather than composing one', () => {
    renderHelp(stubClient(PACKS))
    expect(screen.getByText(/the desk has not said/)).toBeTruthy()
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
    expect(screen.getByText(/dismisses an open drawer/)).toBeTruthy()
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

  it('carries the true sentence about how this desk is authorized', () => {
    renderHelp(stubClient(PACKS))
    expect(screen.getByText('One owner per local Desk. Source connections are managed separately.')).toBeTruthy()
    expect(screen.getByText(/jpack-desk --reset-sign-in/)).toBeTruthy()
    expect(screen.queryByText(/session token/)).toBeNull()
  })

  it('says what each gate holds and whom it binds, with the command for an outside agent', () => {
    renderHelp(stubClient(PACKS), {}, effectiveConfig(undefined, undefined, undefined, {
      path: '/config/desk.json', present: false,
      chassis: { projectDir: '/desks/a desk', projectFile: '/desks/a desk/jpack-desk.json', runtimeBin: '/opt/jpack/bin/jpack', jobs: { requireTestedReleases: true } }
    }))
    const gates = document.getElementById('gates')!.closest('section')!
    for (const words of ['Reviewed set.', 'Records.', 'Comparable facts.', 'In Desk itself.', 'Who is bound.', 'An outside agent.']) {
      expect(gates.textContent).toContain(words)
    }
    expect(gates.textContent).toContain('it is never refused for a draft, and never recorded')
    expect(gates.textContent).toContain('This holds only as far as the agent’s client really withholds those tools')
    expect(gates.textContent).toContain("JPACK_CONFIG='/desks/a desk/jpack.json' /opt/jpack/bin/jpack mcp")
    expect(gates.textContent).toContain('This installation refuses a job from a release whose saved tests were not run.')
    cleanup()

    renderHelp(stubClient(PACKS), {}, effectiveConfig(undefined, undefined, undefined, {
      path: '/config/desk.json', present: false,
      chassis: { projectDir: '/p', projectFile: '/p/jpack-desk.json', runtimeBin: '/bin/jpack', jobs: { requireTestedReleases: false } }
    }))
    expect(document.getElementById('gates')!.closest('section')!.textContent).toContain('This installation allows a job from a release whose saved tests were not run, because Desk was started with --runner-require-tested-releases=false.')
  })

  it.each([
    ['true', true, true], ['false', false, false], ['missing', undefined, false], ['not a boolean', 'true', false]
  ])('says the startup desk inherits JPACK_SIGNING_KEY only where Desk says so: %s', (_, inherits, shown) => {
    renderHelp(stubClient(PACKS), {}, effectiveConfig(undefined, undefined, undefined, {
      path: '/config/desk.json', present: false,
      chassis: { projectDir: '/p', projectFile: '/p/jpack-desk.json', runtimeBin: '/bin/jpack', ...(inherits === undefined ? {} : { runtimeInheritsSigningKey: inherits as boolean }) }
    }))
    const gates = document.getElementById('gates')!.closest('section')!
    expect(gates.textContent?.includes(SIGNING_KEY_LINE)).toBe(shown)
    expect(gates.textContent).toContain('A record is not signed')
  })

  it('quotes the command for a shell, and stands in for what Desk has not said', () => {
    expect(outsideAgentCommand('/desks/abc/', '/usr/bin/jpack')).toBe('JPACK_CONFIG=/desks/abc/jpack.json /usr/bin/jpack mcp')
    expect(outsideAgentCommand("/it's here", 'jpack')).toBe("JPACK_CONFIG='/it'\\''s here/jpack.json' jpack mcp")
    expect(outsideAgentCommand(undefined, undefined)).toBe('JPACK_CONFIG=/absolute/path/to/the/desk/jpack.json jpack mcp')
  })
})

const SIGNING_KEY_LINE = 'But JPACK_SIGNING_KEY is set where Desk was started, so this project’s runtime signs its audit records with the key it names, if it accepts that key. Desks Desk made do not inherit it.'

/** Shown once the configuration query has answered, so the gates can be read after it. */
function Answered() { return useDeskConfigRead() ? <span>configuration read</span> : null }

describe('Help & About → Gates, from Desk’s own desk-config answer', () => {
  /** The page learns the member from the chassis, through the real provider. */
  async function renderAnswered(runtime: Record<string, unknown>) {
    vi.stubGlobal('fetch', async (url: string) => String(url).includes('/api/desk-config')
      ? { ok: true, status: 200, statusText: '', text: async () => JSON.stringify({ path: '/desk.json', present: false, sha256: '', project: { dir: '/p', file: '/p/jpack-desk.json' }, runtime }) }
      : { ok: false, status: 404, statusText: '', text: async () => JSON.stringify({ error: 'no such file' }) })
    render(<QueryClientProvider client={testQueryClient()}><DeskConfigProvider><GatesHelp /><Answered /></DeskConfigProvider></QueryClientProvider>)
    await screen.findByText('configuration read')
    return document.getElementById('gates')!.closest('section')!.textContent ?? ''
  }

  it('says the startup desk’s runtime inherits JPACK_SIGNING_KEY where Desk says true', async () => {
    expect(await renderAnswered({ bin: 'jpack', inheritsSigningKey: true })).toContain(SIGNING_KEY_LINE)
  })

  it.each([
    ['false', { bin: 'jpack', inheritsSigningKey: false }],
    ['no member', { bin: 'jpack' }],
    ['a string', { bin: 'jpack', inheritsSigningKey: 'true' }],
    ['a number', { bin: 'jpack', inheritsSigningKey: 1 }]
  ])('says nothing of a signing key where Desk answered %s', async (_, runtime) => {
    const gates = await renderAnswered(runtime)
    expect(gates).toContain('Records.')
    expect(gates).not.toContain('JPACK_SIGNING_KEY')
  })
})
