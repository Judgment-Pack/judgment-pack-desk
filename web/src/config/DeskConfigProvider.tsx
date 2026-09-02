/**
 * The effective configuration, in one context.
 *
 * The React default value is the same value the query resolves to when nothing
 * was read — the built-in defaults — so the shell renders correctly for the
 * render before the query answers, and in any test that never provides a
 * provider at all. A context whose default was `undefined` would make every
 * consumer carry a null check for a state that is not interesting.
 */
import { createContext, useContext, type ReactNode } from 'react'
import { effectiveConfig, type EffectiveConfig } from './deskConfig'
import { useDeskConfig } from './queries'

const DEFAULTS: EffectiveConfig = effectiveConfig(undefined)

const DeskConfigContext = createContext<EffectiveConfig>(DEFAULTS)

export function useEffectiveConfig(): EffectiveConfig {
  return useContext(DeskConfigContext)
}

export function DeskConfigProvider({ children }: { children: ReactNode }) {
  const { data } = useDeskConfig()
  return <DeskConfigContext.Provider value={data ?? DEFAULTS}>{children}</DeskConfigContext.Provider>
}

/** For a test that wants one configuration without a query behind it. */
export function DeskConfigFixture({
  value,
  children
}: {
  value: EffectiveConfig
  children: ReactNode
}) {
  return <DeskConfigContext.Provider value={value}>{children}</DeskConfigContext.Provider>
}
