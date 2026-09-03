/**
 * Who the desk says is looking, and the boundary that keeps that harmless.
 *
 * **Identity is display, never a gate.** It gates no route, no pane and no
 * chassis endpoint. Authorization is, and stays, the loopback bind, the
 * session token this tab holds, and the origin check — a signed-in viewer and
 * a not-signed-in viewer holding the token have identical reach. The header
 * menu and the Admin page say this outright, because an organization that
 * configures SSO and believes it has gated the desk has been misled by the
 * shell.
 *
 * **The exposed state is a nullable provider, and there is no discriminator.**
 * This used to be a `mode`-tagged union — `{ mode: 'local' }` or
 * `{ mode: 'provider' }` — which is the very shape the configuration schema
 * refuses by name one layer down. A rule that holds in the file and not in the
 * type it decodes to is a rule the next branch is written against: `mode` is a
 * place to put a third member, and nothing but review stood between two
 * members and three. So the state carries the provider or `null`, every reader
 * branches on nullness, and there is no tag for a third case to occupy.
 */
import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'

/** What the desk shows about a configured issuer. Display only, both members. */
export interface ProviderIdentity {
  issuerHost: string
  label: string | null
}

export interface IdentityState {
  /** Null exactly where `identity.provider` is null. There is no third value. */
  provider: ProviderIdentity | null
  /** The local display name, which is what the header shows where there is none. */
  displayName: string
}

const LOCAL: IdentityState = { provider: null, displayName: 'local user' }

const IdentityContext = createContext<IdentityState>(LOCAL)

export function useIdentity(): IdentityState {
  return useContext(IdentityContext)
}

/**
 * The issuer's host, for display.
 *
 * Rendering, not branching: nothing anywhere compares this string to anything.
 * An issuer that will not parse renders as its own text rather than as an
 * invented hostname.
 */
export function issuerHost(issuer: string): string {
  try {
    return new URL(issuer).host
  } catch {
    return issuer
  }
}

export function IdentityProvider({ children }: { children: ReactNode }) {
  const { config } = useEffectiveConfig()
  const provider = config.identity.provider
  const state = useMemo<IdentityState>(
    () => ({
      provider:
        provider === null
          ? null
          : { issuerHost: issuerHost(provider.issuer), label: provider.label },
      displayName: config.user.displayName
    }),
    [provider, config.user.displayName]
  )
  return <IdentityContext.Provider value={state}>{children}</IdentityContext.Provider>
}

/** For a test that wants one identity without a config behind it. */
export function IdentityFixture({
  value,
  children
}: {
  value: IdentityState
  children: ReactNode
}) {
  return <IdentityContext.Provider value={value}>{children}</IdentityContext.Provider>
}
