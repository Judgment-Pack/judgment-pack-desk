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
import { afterEach, describe, expect, it } from 'vitest'
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
import { IdentityProvider } from './IdentityProvider'
import { NONE_MENU_SENTENCE, PROVIDER_PHASE_NOTE, TOKEN_SENTENCE, monogram } from './UserControl'

afterEach(cleanup)

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

function renderHeaderIn(path: string, overrides: Partial<DeskConfig> = {}) {
  const base = effectiveConfig(undefined)
  const value = { ...base, config: { ...base.config, ...overrides } }
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: QUIET.client })}>
            <DeskConfigFixture value={value}>
              <IdentityProvider>
                <HeaderBar
                  inspectorOpen={false}
                  consoleOpen={false}
                  onToggleInspector={() => {}}
                  onToggleConsole={() => {}}
                  railIsDrawer={false}
                  railDrawerOpen={false}
                  onOpenRail={() => {}}
                />
              </IdentityProvider>
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
    expect(menu.textContent).toContain(TOKEN_SENTENCE)
  })

  it('offers no Sign out — not even a disabled one — and no Sign in', async () => {
    renderHeader()
    fireEvent.keyDown(screen.getByRole('button', { name: 'Account and desk settings' }), {
      key: 'Enter'
    })
    const menu = await screen.findByRole('menu')
    expect(menu.textContent).not.toContain('Sign out')
    expect(menu.textContent).not.toContain('Sign in')
    expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual([
      'Appearance',
      'Panes',
      'Keyboard shortcuts',
      'Admin',
      'About'
    ])
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
