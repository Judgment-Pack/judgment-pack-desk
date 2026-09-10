/**
 * The header's identity, in the two states the file admits.
 *
 * The NONE menu's absences are the assertions that matter: **no Sign out and
 * no disabled Sign out** (there is no session to end), and no Sign in (the
 * route to a provider is Admin). A greyed control that will never enable reads
 * as "locked" to anyone who does not know better, which is the opposite of
 * what this desk is.
 */
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import {
  DESK_FALLBACK_NAME,
  effectiveConfig,
  type DeskConfig,
  type IdentityProviderConfig
} from '../config/deskConfig'
import { HeaderBar, markToDataUri } from '../shell/HeaderBar'
import { McpContext } from '../mcp/McpProvider'
import { QueryClientProvider } from '@tanstack/react-query'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { AppearanceProvider, appearanceKey } from '../shell/appearanceState'
import { ShellStateProvider, projectKey, shellStateKey, useShellState } from '../shell/paneState'
import { NARRATION_BOUND, narrationIn } from '../admin/narration'
import { IdentityProvider } from './IdentityProvider'
import {
  DENSITY_SAYS,
  NONE_MENU_SENTENCE,
  PROVIDER_PHASE_NOTE,
  RESET_SAYS,
  RESTORED_SAYS,
  THEME_SAYS,
  SESSION_SENTENCE,
  monogram
} from './UserControl'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.localStorage.clear()
  document.documentElement.removeAttribute('data-theme')
})

/** The chassis' project root, and the key this browser's layout lives under. */
const ROOT = '/home/someone/a-project'
const KEY = shellStateKey(projectKey(ROOT))
/** And the key this browser's appearance lives under — a separate record. */
const APPEARANCE_KEY = appearanceKey(projectKey(ROOT))

/**
 * The live layout, beside the header.
 *
 * The header's own pane toggles are handed their state by the frame, so they
 * report the props a test passed rather than what the provider holds. This
 * reads the provider.
 */
function ShellProbe() {
  const shell = useShellState()
  return <p data-testid="console-open">{String(shell.console.open)}</p>
}

const PROVIDER: IdentityProviderConfig = {
  label: 'Company sign-in',
  issuer: 'https://issuer.example/',
  clientId: 'jpack-desk',
  scopes: ['openid', 'profile'],
  audience: null,
  claims: { name: 'name', picture: 'picture', subject: 'sub' },
  showRemoteAvatar: false,
  signOut: 'local'
}

const QUIET = stubClient({ list_packs: () => ({ text: JSON.stringify({ packs: [] }) }) })

function renderHeader(overrides: Partial<DeskConfig> = {}) {
  return renderHeaderIn('/', overrides)
}

/**
 * `null` is "the chassis has not answered yet". Not `undefined`: a default
 * parameter takes over for an explicit `undefined`, so the provisional case
 * would silently get the resolved root and assert nothing.
 */
function renderHeaderIn(
  path: string,
  overrides: Partial<DeskConfig> = {},
  projectIdentity: string | null = ROOT
) {
  const base = effectiveConfig(undefined)
  const value = { ...base, config: { ...base.config, ...overrides } }
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: QUIET.client })}>
            <DeskConfigFixture value={value}>
              <ShellStateProvider
                projectIdentity={projectIdentity ?? undefined}
                viewport={{ railIsDrawer: false, inspectorIsDrawer: false }}
              >
              <AppearanceProvider
                projectIdentity={projectIdentity ?? undefined}
                projectDefault={value.config.appearance}
                projectDefaultKnown
              >
              <IdentityProvider>
                <HeaderBar
                  inspectorOpen={false}
                  inspectorIsDrawer={false}
                  consoleOpen={false}
                  onToggleInspector={() => {}}
                  onToggleConsole={() => {}}
                  railIsDrawer={false}
                  railDrawerOpen={false}
                  onOpenRail={() => {}}
                />
              </IdentityProvider>
              <ShellProbe />
              </AppearanceProvider>
              </ShellStateProvider>
            </DeskConfigFixture>
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: [path] }
  )
  return {
    router,
    ...render(
      <QueryClientProvider client={testQueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
  }
}

describe('the header’s organization identity', () => {
  it('falls back to the desk’s own name, with the non-breaking hyphen intact', () => {
    renderHeader()
    const brand = screen.getByRole('link', { name: DESK_FALLBACK_NAME })
    // U+2011. A plain hyphen here would be a silent regression from the
    // original `.brand` string.
    expect(brand.textContent).toBe('judgment‑pack desk')
    expect(brand.textContent).not.toContain('judgment-pack desk')
  })

  it('renders the configured organization name', () => {
    renderHeader({ organization: { name: 'Acme Co.', mark: null } })
    expect(screen.getByRole('link', { name: 'Acme Co.' })).toBeTruthy()
  })

  it('encodes an inline SVG mark rather than injecting it', () => {
    // Never `dangerouslySetInnerHTML`: this page holds the session token, and
    // the mark is a value out of a project file.
    const uri = markToDataUri('<svg viewBox="0 0 1 1"><path d="M0 0"/></svg>')
    expect(uri!.startsWith('data:image/svg+xml,')).toBe(true)
    expect(uri).not.toContain('<svg')
    expect(markToDataUri('data:image/png;base64,AAA')).toBe('data:image/png;base64,AAA')
    expect(markToDataUri(null)).toBeUndefined()
    // A path is not a mark; nothing here fetches one.
    expect(markToDataUri('assets/logo.svg')).toBeUndefined()
  })

  it('keeps the brand inside the router rather than reloading the document', async () => {
    // An `<a href="/">` here is a full document load: the SPA restarts, every
    // query refetches, `/ws` drops — and the chassis kills the runtime
    // subprocess when the socket that started it closes, so clicking the desk's
    // own name respawned `jpack mcp`.
    const brand = screen.queryByRole('link', { name: DESK_FALLBACK_NAME })
    expect(brand).toBeNull()
    const { router } = renderHeaderIn('/admin')
    fireEvent.click(screen.getByRole('link', { name: DESK_FALLBACK_NAME }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/'))
  })

  it('labels the project chip as a label rather than a switcher', async () => {
    renderHeader()
    fireEvent.keyDown(screen.getByRole('button', { name: /this project/ }), { key: 'Enter' })
    const menu = await screen.findByRole('menu')
    expect(menu.textContent).toContain('a label, not a switcher')
    expect(menu.textContent).not.toContain('workspace')
    expect(menu.textContent).not.toContain('tenant')
  })
})

describe('the user control, identity NONE', () => {
  it('shows the local display name and a local tag', async () => {
    renderHeader({
      user: { displayName: 'local user' },
      identity: { provider: null }
    })
    expect(screen.getByText('local user')).toBeTruthy()
    expect(screen.getByText('local')).toBeTruthy()
    expect(await screen.findByText(monogram('local user'))).toBeTruthy()
  })

  it('opens a menu whose first line says what actually authorizes the desk', async () => {
    renderHeader()
    fireEvent.keyDown(screen.getByRole('button', { name: 'Account and desk settings' }), {
      key: 'Enter'
    })
    const menu = await screen.findByRole('menu')
    expect(menu.textContent).toContain(NONE_MENU_SENTENCE)
    expect(menu.textContent).toContain(SESSION_SENTENCE)
  })

  it('offers no Sign out — not even a disabled one — and no Sign in', async () => {
    renderHeader()
    fireEvent.keyDown(screen.getByRole('button', { name: 'Account and desk settings' }), {
      key: 'Enter'
    })
    const menu = await screen.findByRole('menu')
    expect(menu.textContent).not.toContain('Sign out')
    expect(menu.textContent).not.toContain('Sign in')
    // The appearance choices are `menuitemradio`, and are asserted as such in
    // their own suite: what is left as a plain item is the one action that
    // clears a preference, and the four that navigate or reset.
    expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual([
      'Use the project’s default',
      'Reset panes',
      'Keyboard shortcuts',
      'Admin',
      'About'
    ])
  })
})

/**
 * The panes' reset, in the menu it moved to.
 *
 * It was a button on Admin › Panes — a settings page reaching into a browser's
 * own storage — and the card is gone. The three outcomes are three different
 * facts and the menu says which one happened rather than assuming the first.
 */
/**
 * The menu's own two sentences, in order, and only those that pass the prose
 * bound — which is what `narrationIn` reports.
 */
function knownLongSentences(): string[] {
  return [NONE_MENU_SENTENCE, SESSION_SENTENCE]
    .filter((sentence) => sentence.length > NARRATION_BOUND)
    .map((sentence) => sentence.slice(0, 90))
}

describe('the user menu’s reset', () => {
  async function openMenu() {
    fireEvent.keyDown(screen.getByRole('button', { name: 'Account and desk settings' }), {
      key: 'Enter'
    })
    return screen.findByRole('menu')
  }

  it('clears exactly one localStorage key, and says so without closing the menu', async () => {
    // One key: `localStorage.clear()` would take this record's neighbours and
    // every other project's layout with it, and a reset that logged the viewer
    // out of something would be one that lied about scope.
    window.localStorage.setItem(KEY, '{"v":1}')
    window.localStorage.setItem('jpack-desk:shell:v1:another', '{"v":1}')
    window.localStorage.setItem('jpack-desk-unrelated', 'a value')
    renderHeader()
    const menu = await openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Reset panes' }))
    expect(window.localStorage.getItem(KEY)).toBeNull()
    expect(window.localStorage.getItem('jpack-desk:shell:v1:another')).toBe('{"v":1}')
    expect(window.localStorage.getItem('jpack-desk-unrelated')).toBe('a value')
    expect(menu.textContent).toContain(RESET_SAYS.cleared)
  })

  it('reports a reset it could not make, rather than reporting one it did', async () => {
    const backing = new Map<string, string>([[KEY, '{"v":1}']])
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => backing.get(key) ?? null,
      setItem: (key: string, value: string) => void backing.set(key, value),
      removeItem: () => {},
      clear: () => {}
    })
    renderHeader()
    const menu = await openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Reset panes' }))
    expect(menu.textContent).toContain(RESET_SAYS.refused)
    expect(menu.textContent).not.toContain(RESET_SAYS.cleared)
  })

  it('refuses to clear a provisional key, and says nothing was cleared', async () => {
    window.localStorage.setItem(shellStateKey('default'), '{"v":1}')
    renderHeaderIn('/', {}, null)
    const menu = await openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Reset panes' }))
    expect(menu.textContent).toContain(RESET_SAYS.unresolved)
    expect(window.localStorage.getItem(shellStateKey('default'))).toBe('{"v":1}')
  })

  it('leaves a value this shell did not write alone, and says nothing was cleared', async () => {
    // The key is derived from a path the viewer never chose, on an origin this
    // desk shares with whatever else has been served from it.
    window.localStorage.setItem(KEY, 'something else entirely')
    renderHeader()
    const menu = await openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Reset panes' }))
    expect(menu.textContent).toContain(RESET_SAYS.foreign)
    expect(menu.textContent).not.toContain(RESET_SAYS.cleared)
    expect(window.localStorage.getItem(KEY)).toBe('something else entirely')
  })

  /**
   * **The narration sweep, over the feedback Admin's own sweep cannot see.**
   *
   * The reset's answer is rendered in a portal — the menu's content — which is
   * outside the Admin container that sweep walks, and it exists only while the
   * menu is open. All four outcomes are one sentence each and are held to the
   * same bound as the page's.
   *
   * **The menu's own two sentences are over the bound on purpose**, and the
   * sweep is asserted against exactly them rather than against nothing: they
   * are the security explanation this menu exists to carry, they predate the
   * reset, and Admin's no-narration rule is Admin's. Everything else in the
   * menu — the reset's answer included — is held to the page's bound, and a
   * third long sentence appearing anywhere in here fails.
   */
  it.each([
    ['cleared', () => window.localStorage.setItem(KEY, '{"v":1}')],
    [
      'refused',
      () => {
        const backing = new Map<string, string>([[KEY, '{"v":1}']])
        vi.stubGlobal('localStorage', {
          getItem: (key: string) => backing.get(key) ?? null,
          setItem: (key: string, value: string) => void backing.set(key, value),
          removeItem: () => {},
          clear: () => {}
        })
      }
    ],
    ['foreign', () => window.localStorage.setItem(KEY, 'something else entirely')],
    ['unresolved', () => {}]
  ] as const)('carries no paragraph when the reset answers %s', async (outcome, arrange) => {
    arrange()
    if (outcome === 'unresolved') renderHeaderIn('/', {}, null)
    else renderHeader()
    const menu = await openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Reset panes' }))
    expect(menu.textContent).toContain(RESET_SAYS[outcome])
    const long = narrationIn(menu)
    // **The two sentences this menu carries, and no third.** Which of them is
    // long enough to count is derived rather than written down: the menu's
    // wording changes when what authorizes this desk changes, and a hard-coded
    // list would fail on an improvement rather than on a new paragraph.
    expect(long.map((each) => each.says), long.map((each) => each.says).join(' | ')).toEqual(
      knownLongSentences()
    )
    expect(menu.textContent).toContain(NONE_MENU_SENTENCE)
    expect(menu.textContent).toContain(SESSION_SENTENCE)
    // And the answer itself is a line, whichever of the four it is.
    expect(RESET_SAYS[outcome].length).toBeLessThanOrEqual(NARRATION_BOUND)
  })

  it('drops the verdict when the menu closes, rather than greeting the next reader with it', async () => {
    // A verdict from the last time the menu was open is not a verdict about
    // this one. It goes with the menu's own content, which is what makes the
    // outcome the action's state rather than the control's.
    renderHeader()
    const first = await openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Reset panes' }))
    expect(first.textContent).toContain(RESET_SAYS.cleared)
    fireEvent.keyDown(first, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
    const again = await openMenu()
    expect(again.textContent).not.toContain(RESET_SAYS.cleared)
  })

  it('puts the panes back where the layout came from, not merely the record', async () => {
    // What the card's reset did, from where the control now is: the record is
    // removed *and* the live layout is re-seeded, so the panes move now rather
    // than at the next reload.
    window.localStorage.setItem(
      KEY,
      JSON.stringify({ v: 1, console: { open: true, tab: 'connection' } })
    )
    renderHeader()
    expect(screen.getByTestId('console-open').textContent).toBe('true')
    await openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Reset panes' }))
    expect(screen.getByTestId('console-open').textContent).toBe('false')
    expect(window.localStorage.getItem(KEY)).toBeNull()
  })
})

/**
 * Appearance, in the menu it moved to.
 *
 * It was a card on Admin writing `appearance` into `jpack-desk.json` — a file
 * in the project's repository, so one person choosing dark chose it for
 * everybody who cloned it. What a viewer picks here is theirs and this
 * browser's; the file's value is what they get if they pick nothing.
 */
describe('the user menu’s appearance', () => {
  async function openMenu() {
    fireEvent.keyDown(screen.getByRole('button', { name: 'Account and desk settings' }), {
      key: 'Enter'
    })
    return screen.findByRole('menu')
  }

  /** The five choices, and which of them the menu says is in force. */
  function choices() {
    return screen
      .getAllByRole('menuitemradio')
      .map((item) => `${item.textContent}${item.getAttribute('aria-checked') === 'true' ? '*' : ''}`)
  }

  const LIGHT_COMPACT = { theme: 'light', density: 'compact' } as const

  it('offers the decoder’s own unions, and checks the project’s value', async () => {
    renderHeader({ appearance: LIGHT_COMPACT })
    await openMenu()
    expect(choices()).toEqual(['system', 'light*', 'dark', 'comfortable', 'compact*'])
    // And it says what that value is, so "use the default" is not a leap.
    expect(screen.getByRole('menu').textContent).toContain('Project default: light, compact')
  })

  it('applies a chosen theme at once, stores it, and stays open for the next choice', async () => {
    renderHeader({ appearance: LIGHT_COMPACT })
    const menu = await openMenu()
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'dark' }))
    // No Save: a preference is not a file.
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    await waitFor(() =>
      expect(JSON.parse(window.localStorage.getItem(APPEARANCE_KEY)!)).toEqual({
        v: 1,
        theme: 'dark'
      })
    )
    // The menu is still open, because theme and density are two choices.
    expect(screen.queryByRole('menu')).toBe(menu)
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'comfortable' }))
    await waitFor(() =>
      expect(JSON.parse(window.localStorage.getItem(APPEARANCE_KEY)!)).toEqual({
        v: 1,
        theme: 'dark',
        density: 'comfortable'
      })
    )
    expect(choices()).toEqual(['system', 'light', 'dark*', 'comfortable*', 'compact'])
  })

  it('writes only the member that was chosen, never its sibling', async () => {
    // A record is preferred over the file on the next read, so a `density`
    // stored because the *theme* was picked would be a built-in value silently
    // outranking `jpack-desk.json` for ever.
    renderHeader({ appearance: LIGHT_COMPACT })
    await openMenu()
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'dark' }))
    await waitFor(() => expect(window.localStorage.getItem(APPEARANCE_KEY)).not.toBeNull())
    expect(JSON.parse(window.localStorage.getItem(APPEARANCE_KEY)!).density).toBeUndefined()
  })

  it('stores nothing at all for a viewer who chooses nothing', async () => {
    renderHeader({ appearance: LIGHT_COMPACT })
    await openMenu()
    expect(window.localStorage.getItem(APPEARANCE_KEY)).toBeNull()
    expect(document.documentElement.getAttribute('data-theme')).toBe('light')
  })

  it('shows a stored preference as the one in force, over the file’s', async () => {
    window.localStorage.setItem(APPEARANCE_KEY, JSON.stringify({ v: 1, theme: 'dark' }))
    renderHeader({ appearance: LIGHT_COMPACT })
    await openMenu()
    // The theme is this browser's; the density is still the file's.
    expect(choices()).toEqual(['system', 'light', 'dark*', 'comfortable', 'compact*'])
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
  })

  it('treats a stored value outside the union as no preference at all', async () => {
    window.localStorage.setItem(APPEARANCE_KEY, JSON.stringify({ v: 1, theme: 'midnight' }))
    renderHeader({ appearance: LIGHT_COMPACT })
    await openMenu()
    // Never applied, never shown as chosen: the project's default answers.
    expect(choices()).toEqual(['system', 'light*', 'dark', 'comfortable', 'compact*'])
    expect(document.documentElement.getAttribute('data-theme')).toBe('light')
  })

  it('clears the preference, and only its own key', async () => {
    window.localStorage.setItem(APPEARANCE_KEY, JSON.stringify({ v: 1, theme: 'dark' }))
    window.localStorage.setItem(KEY, '{"v":1}')
    window.localStorage.setItem('jpack-desk-unrelated', 'a value')
    renderHeader({ appearance: LIGHT_COMPACT })
    const menu = await openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Use the project’s default' }))
    expect(window.localStorage.getItem(APPEARANCE_KEY)).toBeNull()
    // The panes' record is a different record, and the other key is nobody's.
    expect(window.localStorage.getItem(KEY)).toBe('{"v":1}')
    expect(window.localStorage.getItem('jpack-desk-unrelated')).toBe('a value')
    // The file's value is in force again, now rather than at the next reload.
    expect(document.documentElement.getAttribute('data-theme')).toBe('light')
    expect(choices()).toEqual(['system', 'light*', 'dark', 'comfortable', 'compact*'])
    expect(menu.textContent).toContain(RESTORED_SAYS.cleared)
  })

  it('reports a clearance it could not make, rather than reporting one it did', async () => {
    const backing = new Map<string, string>([[APPEARANCE_KEY, '{"v":1,"theme":"dark"}']])
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => backing.get(key) ?? null,
      setItem: (key: string, value: string) => void backing.set(key, value),
      removeItem: () => {},
      clear: () => {}
    })
    renderHeader({ appearance: LIGHT_COMPACT })
    const menu = await openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Use the project’s default' }))
    expect(menu.textContent).toContain(RESTORED_SAYS.refused)
    expect(menu.textContent).not.toContain(RESTORED_SAYS.cleared)
    // Nothing moved: the record is still there to come back on the next load,
    // so a menu showing the project's default would be showing a fiction.
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
  })

  it('leaves a value this desk did not write alone, and says nothing was cleared', async () => {
    window.localStorage.setItem(APPEARANCE_KEY, 'something else entirely')
    renderHeader({ appearance: LIGHT_COMPACT })
    const menu = await openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Use the project’s default' }))
    expect(menu.textContent).toContain(RESTORED_SAYS.foreign)
    expect(window.localStorage.getItem(APPEARANCE_KEY)).toBe('something else entirely')
  })

  it('writes nothing under the provisional key, and says nothing was cleared', async () => {
    // Until the chassis says which project this is, the key is the literal
    // `default` — a record written there is one project's preference stored
    // under a name that belongs to whichever project answers slowly next.
    renderHeaderIn('/', { appearance: LIGHT_COMPACT }, null)
    const menu = await openMenu()
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'dark' }))
    // The choice is honoured on screen — it is this viewer's, and it is not
    // dropped — and nothing at all is stored.
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
    await waitFor(() => expect(choices()).toContain('dark*'))
    expect(
      Object.keys(window.localStorage).filter((key) => key.startsWith('jpack-desk:appearance:'))
    ).toEqual([])
    fireEvent.click(screen.getByRole('menuitem', { name: 'Use the project’s default' }))
    expect(menu.textContent).toContain(RESTORED_SAYS.unresolved)
  })

  it('says what a theme actually does, where the choice is offered', async () => {
    // Admin's card carried both sentences and Admin's card is gone. A control
    // that moved somewhere its own caveat did not follow is one that has
    // quietly started overstating itself.
    renderHeader()
    const menu = await openMenu()
    expect(menu.textContent).toContain(THEME_SAYS)
    expect(menu.textContent).toContain(DENSITY_SAYS)
  })

  /**
   * **The narration sweep, with the appearance groups on screen.**
   *
   * The menu's own two sentences are over the bound on purpose — they are the
   * security explanation it exists to carry — and everything this chunk adds is
   * held to the page's line. A third long sentence anywhere in here fails.
   */
  it.each([
    ['cleared', () => window.localStorage.setItem(APPEARANCE_KEY, '{"v":1,"theme":"dark"}')],
    ['foreign', () => window.localStorage.setItem(APPEARANCE_KEY, 'something else entirely')]
  ] as const)('carries no paragraph when the appearance reset answers %s', async (outcome, arrange) => {
    arrange()
    renderHeader()
    const menu = await openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Use the project’s default' }))
    expect(menu.textContent).toContain(RESTORED_SAYS[outcome])
    const long = narrationIn(menu)
    // **The two sentences this menu carries, and no third.** Which of them is
    // long enough to count is derived rather than written down: the menu's
    // wording changes when what authorizes this desk changes, and a hard-coded
    // list would fail on an improvement rather than on a new paragraph.
    expect(long.map((each) => each.says), long.map((each) => each.says).join(' | ')).toEqual(
      knownLongSentences()
    )
    expect(menu.textContent).toContain(NONE_MENU_SENTENCE)
    expect(menu.textContent).toContain(SESSION_SENTENCE)
    for (const line of [THEME_SAYS, DENSITY_SAYS, RESTORED_SAYS[outcome]]) {
      expect(line.length, line).toBeLessThanOrEqual(NARRATION_BOUND)
    }
  })
})

describe('the user control, a provider configured', () => {
  it('names the issuer host and says sign-in arrives later, and gates nothing', async () => {
    renderHeader({ identity: { provider: PROVIDER } })
    expect(screen.getByText('issuer.example')).toBeTruthy()
    fireEvent.keyDown(screen.getByRole('button', { name: 'Account and desk settings' }), {
      key: 'Enter'
    })
    const menu = await screen.findByRole('menu')
    expect(menu.textContent).toContain(PROVIDER_PHASE_NOTE)
    // Still no roles, groups, scopes or entitlements, and still no gate.
    expect(menu.textContent).not.toContain('role')
    expect(menu.textContent).not.toContain('scope')
  })

  it('states no session verdict, because phase A checks no session', () => {
    // With no label the control used to read "signed out" — a verdict about a
    // session that is only reachable by checking discovery and expiry, neither
    // of which happens anywhere in this phase. It names what the desk actually
    // read out of the file instead.
    const { container } = renderHeader({
      identity: { provider: { ...PROVIDER, label: null } }
    })
    expect(container.textContent).not.toContain('signed out')
    expect(container.textContent).not.toContain('Signed out')
    expect(container.textContent).not.toContain('Sign in')
    expect(screen.getAllByText('issuer.example').length).toBeGreaterThan(0)
  })

  it('never takes the organization name from the provider’s label', () => {
    renderHeader({ identity: { provider: { ...PROVIDER, label: 'Globex Incorporated' } } })
    // The header still reads the desk's own fallback: an issuer's label for a
    // customer is not the customer's brand.
    expect(screen.getByRole('link', { name: DESK_FALLBACK_NAME })).toBeTruthy()
    expect(screen.queryByRole('link', { name: 'Globex Incorporated' })).toBeNull()
  })
})
