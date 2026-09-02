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
 * **The exposed session type has exactly two members**, and that is the
 * enforcement rather than the convention: `identity.provider === null` is
 * local, anything else is a provider, and there is no third shape for a
 * supplied issuer to occupy. An issuer someone else operates and an issuer you
 * run are the same object with a different URL.
 */
import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'

export type IdentitySession =
  | { mode: 'local'; displayName: string }
  | { mode: 'provider'; issuerHost: string; label: string | null }

const LOCAL: IdentitySession = { mode: 'local', displayName: 'local user' }

const IdentityContext = createContext<IdentitySession>(LOCAL)

export function useIdentity(): IdentitySession {
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
  const session = useMemo<IdentitySession>(
    () =>
      provider === null
        ? { mode: 'local', displayName: config.user.displayName }
        : { mode: 'provider', issuerHost: issuerHost(provider.issuer), label: provider.label },
    [provider, config.user.displayName]
  )
  return <IdentityContext.Provider value={session}>{children}</IdentityContext.Provider>
}

/** For a test that wants one session without a config behind it. */
export function IdentityFixture({
  value,
  children
}: {
  value: IdentitySession
  children: ReactNode
}) {
  return <IdentityContext.Provider value={value}>{children}</IdentityContext.Provider>
}
