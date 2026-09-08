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

/**
 * Whether the value above came from a read, as opposed to standing in for one.
 *
 * A second context rather than a member of `EffectiveConfig`, because that
 * shape is the *configuration* and this is a fact about the query behind it —
 * and because every one of its consumers is right not to care. The one that
 * does is the appearance ladder: the built-in defaults and a file that says the
 * same thing are indistinguishable in the value, and applying the first as
 * though it were the second is a theme this desk had not yet decided on.
 */
const DeskConfigReadContext = createContext(false)

export function useEffectiveConfig(): EffectiveConfig {
  return useContext(DeskConfigContext)
}

/** True once the configuration query has actually answered. */
export function useDeskConfigRead(): boolean {
  return useContext(DeskConfigReadContext)
}

export function DeskConfigProvider({ children }: { children: ReactNode }) {
  const { data } = useDeskConfig()
  const value = data ?? DEFAULTS
  // **The theme is no longer applied here**, and the move is the point.
  // `appearance` in the project file is now the *default*, not the answer: what
  // this desk paints is the viewer's own preference where they have one, and
  // this layer cannot see that. `AppearanceProvider` resolves the ladder and
  // applies the result, and it is still exactly one place.
  return (
    <DeskConfigReadContext.Provider value={data !== undefined}>
      <DeskConfigContext.Provider value={value}>{children}</DeskConfigContext.Provider>
    </DeskConfigReadContext.Provider>
  )
}

/** For a test that wants one configuration without a query behind it. */
export function DeskConfigFixture({
  value,
  read = true,
  children
}: {
  value: EffectiveConfig
  /** A fixture is a configuration that was read, unless a test says otherwise. */
  read?: boolean
  children: ReactNode
}) {
  return (
    <DeskConfigReadContext.Provider value={read}>
      <DeskConfigContext.Provider value={value}>{children}</DeskConfigContext.Provider>
    </DeskConfigReadContext.Provider>
  )
}
