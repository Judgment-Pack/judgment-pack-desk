/**
 * The theme and the density this viewer chose, per browser.
 *
 * **Appearance is a person's, not an organization's.** It used to be a card on
 * Admin that wrote `appearance` into `jpack-desk.json` — a file in the
 * project's repository — so one person choosing dark chose it for everyone who
 * ever cloned it. The member is still in the schema, still decoded and still
 * honoured: it is now the **default**, for everyone on this project who has not
 * chosen. What a viewer chooses lives here instead, in this browser, under one
 * key, and is never written to any file.
 *
 * **The ladder is three deep and is computed in one place.** The viewer's
 * preference where they set it, else the project file's `appearance`, else the
 * schema's built-in default — and the last two are already one value by the
 * time this module sees them, because the decoder fills an absent member in
 * with `DESK_DEFAULTS`. `effectiveAppearance` is the whole of the rule and
 * `useAppearance` is the only thing that runs it, so no surface can compute a
 * fourth answer of its own.
 *
 * **The record is the pane record's shape, deliberately.** Same key prefix
 * grammar — the whole project root, percent-encoded — same version in the
 * value, same try/catch around every access, same treatment of anything it
 * cannot read: discarded silently, because this is a per-viewer convenience and
 * a banner about a browser's own storage would be the desk reporting on the
 * wrong thing. A value outside the decoder's own unions is *absent*: never
 * applied, never shown as chosen, never written back.
 *
 * **And it persists one member at a time**, for the reason `paneState` gives
 * about panes: a record is preferred over the configuration on the next read,
 * so a `density` serialized because the *theme* was chosen would be a built-in
 * value silently outranking `jpack-desk.json` for ever. Chosen is two bits, the
 * write serializes only the members those bits name, and the re-seed replaces
 * only the members they do not.
 *
 * **There is no identity-bound store here.** The identity slot has no real user
 * yet; when it does, this record can move server-side and follow a person
 * between browsers. Until then it is per browser, and the README says so rather
 * than the menu pretending otherwise.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode
} from 'react'
import { createElement } from 'react'
import {
  DENSITIES,
  DESK_DEFAULTS,
  THEME_CHOICES,
  type AppearanceConfig,
  type Density,
  type ThemeChoice
} from '../config/deskConfig'
import { useAppliedTheme } from '../config/theme'
import {
  identityIsResolved,
  projectKey,
  type RecordReset,
  type ResetOutcome
} from './paneState'

/**
 * What this browser holds: each member present only where the viewer chose it.
 *
 * Sparse on purpose. "This viewer wants dark" and "this viewer wants dark and
 * whatever the density happens to be today" are different statements, and only
 * the first is one anybody made.
 */
export interface AppearancePreference {
  theme?: ThemeChoice
  density?: Density
}

const RECORD_VERSION = 1

/** One record's key. Exported so a test — and the menu — can name it. */
export function appearanceKey(project: string): string {
  return `jpack-desk:appearance:v${RECORD_VERSION}:${project}`
}

/**
 * Read one record.
 *
 * Every access in try/catch, for `paneState`'s reason: a private window or a
 * browser set to block site data *throws* on the accessor rather than answering
 * null, and a thrown accessor must still render a working desk on the project's
 * own default.
 *
 * **A value outside the union is absent**, not a problem to report. Strictness
 * lives in the config decoder, which refuses a typo'd theme by name at the file
 * it was written in; here a `"midnight"` somebody put in `localStorage` by hand
 * is simply not a preference, and the project's default applies as though
 * nothing were stored.
 */
export function readAppearance(key: string): AppearancePreference | undefined {
  let raw: string | null = null
  try {
    raw = window.localStorage.getItem(key)
  } catch {
    return undefined
  }
  if (raw === null) return undefined
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return undefined
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return undefined
  const record = parsed as Record<string, unknown>
  if (record.v !== RECORD_VERSION) return undefined
  const preference: AppearancePreference = {}
  if (isTheme(record.theme)) preference.theme = record.theme
  if (isDensity(record.density)) preference.density = record.density
  return preference
}

function isTheme(value: unknown): value is ThemeChoice {
  return THEME_CHOICES.includes(value as ThemeChoice)
}

function isDensity(value: unknown): value is Density {
  return DENSITIES.includes(value as Density)
}

/** Which members the viewer chose **this visit**. One bit each, never one for both. */
export interface ChosenAppearance {
  theme: boolean
  density: boolean
}

export const NOTHING_CHOSEN: ChosenAppearance = { theme: false, density: false }

/**
 * Write the members the viewer chose this visit, over the ones they chose
 * before, and invent none.
 *
 * The base is the record already on disk, read back through the same validator
 * so nothing unreadable is carried forward, and only the currently chosen
 * members override it. Starting from `{v}` instead would erase a theme chosen
 * on an earlier visit the moment a density was picked.
 */
export function writeAppearance(
  key: string,
  preference: AppearancePreference,
  chosen: ChosenAppearance
): void {
  const kept = readAppearance(key) ?? {}
  const theme = chosen.theme ? preference.theme : kept.theme
  const density = chosen.density ? preference.density : kept.density
  const record: Record<string, unknown> = { v: RECORD_VERSION }
  if (theme !== undefined) record.theme = theme
  if (density !== undefined) record.density = density
  try {
    window.localStorage.setItem(key, JSON.stringify(record))
  } catch {
    // A viewer whose storage refuses writes still gets the appearance they
    // chose for this visit; it simply does not survive the reload.
  }
}

/**
 * Forget this browser's appearance, and say what became of the record.
 *
 * Exactly one key — `localStorage.clear()` would take the session token and
 * every other project's record with it — and **only a record this desk wrote**:
 * the key is derived from a path the viewer never chose, on an origin this desk
 * shares with whatever else has been served from it, so removing whatever
 * happens to be sitting there would be a control deleting somebody else's value
 * under a name it merely computed.
 */
export function resetAppearance(key: string): RecordReset {
  let raw: string | null
  try {
    raw = window.localStorage.getItem(key)
  } catch {
    return 'refused'
  }
  // Nothing there is not a foreign value: an absent record is the state this
  // asks for, and removing it again is harmless.
  if (raw !== null && readAppearance(key) === undefined) return 'foreign'
  try {
    window.localStorage.removeItem(key)
    // Read back, because `removeItem` resolves on a storage that keeps the
    // value: the menu must not say "the project's default is back" on the
    // strength of having asked.
    return window.localStorage.getItem(key) === null ? 'cleared' : 'refused'
  } catch {
    return 'refused'
  }
}

/**
 * The appearance that is actually in force: the preference where it is set,
 * and the project's default where it is not.
 *
 * The whole rule, in one pure function, so that every consumer is reading the
 * same answer and a test can drive the ladder without a browser.
 */
export function effectiveAppearance(
  preference: AppearancePreference | undefined,
  projectDefault: AppearanceConfig
): AppearanceConfig {
  return {
    theme: preference?.theme ?? projectDefault.theme,
    density: preference?.density ?? projectDefault.density
  }
}

export interface AppearanceApi extends AppearanceConfig {
  /**
   * The project file's value — or the schema's, where the file says nothing.
   * What "Use the project's default" returns to, and what the menu names so
   * that clearing is not a leap in the dark.
   */
  projectDefault: AppearanceConfig
  /** What this browser holds. Empty where the viewer has chosen nothing. */
  preference: AppearancePreference
  setTheme: (theme: ThemeChoice) => void
  setDensity: (density: Density) => void
  /** Forget this browser's choice, and say what happened to the record. */
  restoreProjectDefault: () => ResetOutcome
  /** The key this project's record lives under, so a test can name it. */
  storageKey: string
  /** False while the chassis has not yet said which project this is. */
  keyResolved: boolean
}

/**
 * A real default value, so the page renders correctly for the render before the
 * config query resolves and in any test that provides no provider — the same
 * reason `paneState` and `DeskConfigProvider` each carry one.
 */
const DEFAULT_API: AppearanceApi = {
  ...DESK_DEFAULTS.appearance,
  projectDefault: DESK_DEFAULTS.appearance,
  preference: {},
  setTheme: () => {},
  setDensity: () => {},
  restoreProjectDefault: () => 'unresolved',
  storageKey: appearanceKey('default'),
  keyResolved: false
}

const AppearanceContext = createContext<AppearanceApi>(DEFAULT_API)

export function useAppearance(): AppearanceApi {
  return useContext(AppearanceContext)
}

export function AppearanceProvider({
  projectIdentity,
  projectDefault,
  children
}: {
  /**
   * The chassis' project root, or undefined until the file listing answers.
   * The same identity the pane record is keyed on, and not the runtime's
   * `configPath`: a project with no `jpack.json` has none, and every such
   * project would share one record.
   */
  projectIdentity?: string
  /** `appearance` as the decoder produced it: the file's value, or the schema's. */
  projectDefault: AppearanceConfig
  children: ReactNode
}) {
  const keyResolved = identityIsResolved(projectIdentity)
  const storageKey = appearanceKey(projectKey(projectIdentity))

  /**
   * The stored record, or nothing at all while the key is provisional.
   *
   * The read is gated as well as the write. Until the chassis names the
   * project, `storageKey` is the literal `default` — a key some earlier build
   * may have written to — and a preference that belongs to a project this may
   * not be is not one to apply.
   */
  const storedForKey = () => (keyResolved ? readAppearance(storageKey) : undefined)
  const [preference, setPreference] = useState<AppearancePreference>(() => storedForKey() ?? {})

  /**
   * Which members the viewer chose **for this project**.
   *
   * A ref rather than state because re-seeding must not itself be a render, and
   * because marking a choice belongs to the handler that took it.
   *
   * **A choice belongs to the project it was made in**, and it was visit-wide
   * until the review found the leak. One tab whose chassis reconnects reports a
   * different root, and a member still marked chosen from root A survived the
   * re-seed and was then written into root B's record — one project's
   * preference in another project's key, permanently, over a record B may never
   * have had. What is chosen under one root is now never written under another.
   */
  const chosen = useRef<ChosenAppearance>({ ...NOTHING_CHOSEN })

  /**
   * The seed is re-taken when the key changes, because the key **is not known
   * at first paint**: the chassis' file listing answers afterwards, and the
   * record is unreadable until it does.
   *
   * **What carries across that change is decided by what the old key was.**
   * The provisional key is not a project — nothing can be read under it and
   * nothing is written under it — so a viewer who picked dark in the moment
   * before the listing landed picked it for whichever project the listing then
   * names, and that choice is carried and written under the real key rather
   * than dropped. A key that *was* a project is the other case entirely: the
   * new root is a different project, so every choice is forgotten and both
   * members are re-seeded from the new key's own record, or from that project's
   * default where it has none.
   *
   * The resolution is held beside the key rather than inferred from it, so that
   * a root which happens to encode to the provisional key's own spelling cannot
   * be read as "no project yet".
   */
  const seededFrom = useRef({ key: storageKey, resolved: keyResolved })
  useEffect(() => {
    const previous = seededFrom.current
    if (previous.key === storageKey && previous.resolved === keyResolved) return
    seededFrom.current = { key: storageKey, resolved: keyResolved }
    // Carried only out of the provisional key. Assigned before the early
    // return below, so that leaving a project always forgets its choices —
    // including the case where the viewer had chosen both members, which is
    // exactly the case that used to return before it cleared anything.
    const carried = previous.resolved ? { ...NOTHING_CHOSEN } : { ...chosen.current }
    chosen.current = { ...carried }
    if (carried.theme && carried.density) return
    setPreference((previous) => {
      const seeded = storedForKey() ?? {}
      return {
        theme: carried.theme ? previous.theme : seeded.theme,
        density: carried.density ? previous.density : seeded.density
      }
    })
    // `storedForKey` reads the two values in the dependency list and nothing
    // else, which is why it is not one itself.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey, keyResolved])

  /**
   * **Only a choice is written, and never under a provisional key.**
   *
   * A record written on mount would be a preference nobody set, and a record
   * is preferred over the file on the next read — so the first visit would
   * store the project's own default and every visit after it would ignore the
   * file. Nothing is stored until somebody picks something.
   *
   * There is no debounce, unlike the pane record: a theme is picked, not
   * dragged, so there is no burst of intermediate values to collapse and no
   * write in flight for the reset to cancel.
   */
  // It is the re-seed above that makes this safe across a root change: effects
  // run in the order they are declared, so by the time this one sees a new key
  // the choices made under the old one have already been forgotten and there is
  // nothing for it to write.
  useEffect(() => {
    if (!keyResolved) return
    const chose = chosen.current
    if (!chose.theme && !chose.density) return
    writeAppearance(storageKey, preference, chose)
  }, [storageKey, keyResolved, preference])

  const setTheme = useCallback((theme: ThemeChoice) => {
    chosen.current.theme = true
    setPreference((previous) => ({ ...previous, theme }))
  }, [])
  const setDensity = useCallback((density: Density) => {
    chosen.current.density = true
    setPreference((previous) => ({ ...previous, density }))
  }, [])

  /**
   * **Nothing changes unless the record actually went.** Clearing the live
   * preference on a storage that refused the deletion would leave the menu
   * showing the project's default while the record was still there to come
   * back on the next reload — and so would clearing it over a value this desk
   * never wrote. Both are no-ops in every respect, which is what the menu's
   * sentences claim.
   */
  const restoreProjectDefault = useCallback((): ResetOutcome => {
    if (!keyResolved) return 'unresolved'
    const record = resetAppearance(storageKey)
    if (record !== 'cleared') return record
    chosen.current = { ...NOTHING_CHOSEN }
    setPreference({})
    return 'cleared'
  }, [keyResolved, storageKey])

  // Destructured rather than held as an object: `effectiveAppearance` returns a
  // fresh one on every render, so the memo below depends on the two values it
  // actually carries.
  const { theme, density } = effectiveAppearance(preference, projectDefault)

  // **The one configuration value that is applied rather than displayed**, and
  // it is applied here because this is where the ladder is resolved. It used to
  // be applied from the file, in `DeskConfigProvider`, which is a layer that
  // cannot see the viewer's own choice.
  useAppliedTheme(theme)

  const value = useMemo<AppearanceApi>(
    () => ({
      theme,
      density,
      projectDefault,
      preference,
      setTheme,
      setDensity,
      restoreProjectDefault,
      storageKey,
      keyResolved
    }),
    [
      theme,
      density,
      projectDefault,
      preference,
      setTheme,
      setDensity,
      restoreProjectDefault,
      storageKey,
      keyResolved
    ]
  )
  return createElement(AppearanceContext.Provider, { value }, children)
}
