import { useVerifiedSession } from '../auth/VerifiedSession'
import { msg, useLocale } from '../i18n'
/** Display verified session claims when signed in. Legacy identity configuration
 * remains display-only; the backend's protected sign-in policy authorizes access.
 * An issuer/subject pair, never a display name or email, identifies the owner. */
import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'

/** What the desk shows about a configured issuer. Display only, both members. */
export interface ProviderIdentity {
  issuerHost: string
  label: string | null
}

export interface IdentityState {
  authenticated?: boolean
  /** The verified issuer, or the legacy display provider during initial setup. */
  provider: ProviderIdentity | null
  /** The local display name, which is what the header shows where there is none. */
  displayName: string
}

const LOCAL: IdentityState = { provider: null, get displayName() { return msg("local user") } }

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
  const locale = useLocale()
  const verified = useVerifiedSession()
  const { config, sources, userNameDefaulted } = useEffectiveConfig()
  const provider = config.identity.provider
  const state = useMemo<IdentityState>(
    () => verified?.issuer ? {
      provider: { issuerHost: issuerHost(verified.issuer), label: null },
      displayName: verified.name || verified.email || verified.subject,
      authenticated: true
    } : ({
      provider:
        provider === null
          ? null
          : { issuerHost: issuerHost(provider.issuer), label: provider.label },
      displayName: sources.user === 'default' || userNameDefaulted ? msg('local user') : config.user.displayName
    }),
    [provider, config.user.displayName, sources.user, userNameDefaulted, locale, verified]
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
