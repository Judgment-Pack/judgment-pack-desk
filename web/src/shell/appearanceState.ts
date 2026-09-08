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
  useState,
  type ReactNode
} from 'react'
import { createElement } from 'react'
import {
  DENSITIES,
  THEME_CHOICES,
  type AppearanceConfig,
  type Density,
  type ThemeChoice
} from '../config/deskConfig'
import { useAppliedDensity, useAppliedTheme } from '../config/theme'
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
 * The preference a record carries, where it is a record this writer could have
 * produced — and nothing at all where it is not.
 *
 * **Ownership is every byte of the record**, and it has been narrowed twice.
 * `localStorage` is one namespace shared with everything this origin has ever
 * served, under a key derived from a path the viewer never chose, so this
 * question is "did *this* desk write these bytes" and nothing weaker will do.
 * Reading the version alone admitted
 * `{"v":1,"writer":"another-app","theme":"dark"}` — applied to the page as this
 * desk's preference and deleted by "Use the project's default". Adding the
 * member *names* still admitted `{"v":1,"theme":17}`, which this writer cannot
 * emit either: it was owned, read as a record with nothing usable in it, and
 * deleted on the same terms.
 *
 * So a record is this desk's when it carries the version, no member this writer
 * does not write, at least one that it does, and **a value in its own union for
 * every member present**. Anything else is somebody else's value under a name
 * this desk merely computed: not applied, not deleted, and named as such.
 *
 * Validating and extracting in one pass is the point rather than a convenience.
 * Two passes are two opinions about what a member is, and the second one — the
 * lenient one, which took whatever the first had accepted — was the defect
 * both times.
 */
function ownPreference(record: Record<string, unknown>): AppearancePreference | undefined {
  if (record.v !== RECORD_VERSION) return undefined
  const preference: AppearancePreference = {}
  for (const [member, value] of Object.entries(record)) {
    if (member === 'v') continue
    if (member === 'theme') {
      if (!isTheme(value)) return undefined
      preference.theme = value
    } else if (member === 'density') {
      if (!isDensity(value)) return undefined
      preference.density = value
    } else {
      return undefined
    }
  }
  // A record exists because somebody chose something, so one with nothing
  // chosen in it is not one of this writer's either.
  if (preference.theme === undefined && preference.density === undefined) return undefined
  return preference
}

/**
 * Read one record.
 *
 * Every access in try/catch, for `paneState`'s reason: a private window or a
 * browser set to block site data *throws* on the accessor rather than answering
 * null, and a thrown accessor must still render a working desk on the project's
 * own default.
 *
 * `undefined` is the one answer for every way this can fail: unreadable
 * storage, bytes that are not JSON, an object that is not a record of this
 * version, and a record this writer could not have produced. They are one
 * answer because they have one consequence — the project's default applies, and
 * whatever is under the key is left exactly where it is.
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
  return ownPreference(parsed as Record<string, unknown>)
}

function isTheme(value: unknown): value is ThemeChoice {
  return THEME_CHOICES.includes(value as ThemeChoice)
}

function isDensity(value: unknown): value is Density {
  return DENSITIES.includes(value as Density)
}

/** A preference with nothing in it: the viewer has chosen neither member. */
export const NOTHING_CHOSEN: AppearancePreference = {}

/**
 * Write the members the viewer chose this visit, over the ones they chose
 * before, and invent none.
 *
 * **What was chosen is exactly which members are present**, which is why the
 * choice is sparse: "this viewer wants dark" and "this viewer wants dark and
 * whatever the density happens to be today" are different statements and only
 * the first is one anybody made. A `density` serialized because the *theme* was
 * chosen would be a built-in value silently outranking `jpack-desk.json` for
 * ever, since a record is preferred over the file on the next read.
 *
 * The base is the record already on disk, read back through the same validator
 * so nothing unreadable is carried forward, and only the chosen members
 * override it. Starting from `{v}` instead would erase a theme chosen on an
 * earlier visit the moment a density was picked.
 */
export function writeAppearance(key: string, chosen: AppearancePreference): void {
  const kept = readAppearance(key) ?? {}
  const theme = chosen.theme ?? kept.theme
  const density = chosen.density ?? kept.density
  // **A record with neither member is not a record this writer produces**, and
  // its own reader would call one foreign. Nothing reaches here with both
  // undefined — a chosen member always carries a value — but a writer able to
  // emit bytes its reader disowns is the shape of the defect above, so the
  // absence of a preference is the absence of a record.
  if (theme === undefined && density === undefined) {
    resetAppearance(key)
    return
  }
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
 *
 * **A default that is not known yet is not a rung**, and each member answers
 * for itself. `undefined` out of here is "this desk cannot say", which is a
 * third state and the first one every load is in: the preference is unreadable
 * until the chassis names the project, and the default is the schema's until
 * the file has been read. What comes back is never provisional — it is the
 * viewer's own answer, or the file's, or nothing.
 */
export function effectiveAppearance(
  preference: AppearancePreference | undefined,
  projectDefault: AppearanceConfig | undefined
): { theme: ThemeChoice | undefined; density: Density | undefined } {
  return {
    theme: preference?.theme ?? projectDefault?.theme,
    density: preference?.density ?? projectDefault?.density
  }
}

/**
 * A choice, and the key it was made under.
 *
 * The stamp is what makes a preference belong to one project rather than to one
 * visit. It carries the resolution as well as the key, so a root that happens
 * to encode to the provisional key's own spelling cannot be read as "no project
 * yet".
 */
interface Choice {
  key: string
  resolved: boolean
  value: AppearancePreference
}

/**
 * Whether a stamped choice belongs to somewhere this desk has left.
 *
 * A choice made under the **provisional** key belongs to nowhere yet, so it
 * belongs to wherever the listing lands; every other stamp belongs to one
 * project, and a key that is not that project's is a project this choice has
 * nothing to say about.
 */
function leftBehind(choice: Choice, key: string): boolean {
  return choice.resolved && choice.key !== key
}

/**
 * A stamped choice's members, and none where the stamp was left behind.
 *
 * Only a setter needs this, and only for one window: a pick taken in the same
 * batch as a root change runs against the key it was drawn under, so the
 * members it would otherwise merge in are another project's.
 */
function keptFrom(choice: Choice, key: string): AppearancePreference {
  return leftBehind(choice, key) ? NOTHING_CHOSEN : choice.value
}

export interface AppearanceApi {
  /**
   * The theme and the density actually in force — or `undefined` for each while
   * this desk does not yet know.
   *
   * **Not knowing is a state, and it is the first one.** The record needs the
   * root the chassis has not reported yet and the default needs a file that has
   * not been read, so a value here before both have answered would be the
   * schema's, offered as the viewer's. The menu shows nothing as chosen while
   * it is undefined, and nothing is written onto the root element.
   */
  theme: ThemeChoice | undefined
  density: Density | undefined
  /**
   * The project file's value — or the schema's, where the file says nothing;
   * `undefined` until the file has answered at all. What "Use the project's
   * default" returns to, and what the menu names so that clearing is not a leap
   * in the dark — which means naming it only once it is known.
   */
  projectDefault: AppearanceConfig | undefined
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
  // Nothing is known outside a provider, which is the truthful default: a
  // consumer with none is a consumer that has been told nothing.
  theme: undefined,
  density: undefined,
  projectDefault: undefined,
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
  projectDefaultKnown,
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
  /**
   * Whether the project's configuration has actually been read.
   *
   * `projectDefault` is filled in from the schema until it has, and the two are
   * indistinguishable from here — which is the whole of this flag. A provider
   * that could not tell them apart applied the schema's `system` while it
   * waited, then the file's value, then the stored preference: three
   * applications for one load and two of them wrong.
   */
  projectDefaultKnown: boolean
  children: ReactNode
}) {
  const keyResolved = identityIsResolved(projectIdentity)
  const storageKey = appearanceKey(projectKey(projectIdentity))

  /**
   * What the viewer has chosen this visit, and the key they chose it under.
   *
   * **This is the only state here**, and the record is not part of it. Holding
   * the stored record in state meant re-reading it in an effect whenever the
   * key changed, and an effect runs after a render: the moment the chassis
   * named the project, the provider rendered once with the *old* preference and
   * the *new* key — long enough to apply the project's default over a viewer
   * whose stored answer was something else, and then apply the stored one a
   * beat later. Two applications for one load, the second undoing the first.
   * The record is read during render instead, from whichever key is current, so
   * there is no moment at which the two disagree.
   *
   * **And a choice is stamped with the key it was made under**, which is what
   * makes it belong to one project. It was visit-wide, and the review found the
   * leak: one tab whose chassis reconnects reports a different root, and a
   * member chosen under root A was carried through the re-seed and written into
   * root B's record — permanently, over a record B may never have had. A stamp
   * that does not match simply stops applying, in the same render, with nothing
   * to sequence and no effect to get wrong.
   *
   * The resolution is stamped beside the key rather than inferred from it, so a
   * root that happens to encode to the provisional key's own spelling cannot be
   * read as "no project yet".
   */
  const [choice, setChoice] = useState<Choice>(() => ({
    key: storageKey,
    resolved: keyResolved,
    value: {}
  }))

  /**
   * **A choice this desk has left behind is discarded, not hidden.**
   *
   * Filtering it at the point of use was round 1's fix and round 2 found what
   * it left standing: the value stayed in state, so A → B → A brought it back,
   * ahead of A's own record — and where another tab had changed A's preference
   * meanwhile, the write effect put the resurrected value over it. A stale
   * choice must stop existing, not stop being read.
   *
   * **During render, and not in an effect.** This is React's documented shape
   * for state that has to follow a prop: the update restarts this component's
   * render before anything is committed, so there is no pass in which the old
   * choice reaches the page — which is the flash round 1 removed and must not
   * come back to fix this. An effect would run after a paint, and the value
   * that paint carried would be the previous project's.
   *
   * A pick taken in the same batch as a root change goes with it. It was made
   * for the project this desk was on, that project is not this one, and the
   * only alternatives are writing it somewhere the viewer did not choose or
   * writing it to a project this tab has left.
   *
   * And then it is read with **no filter at all**: the discard has already run
   * for this render, so the stamp either names this key or names the
   * provisional one — and the provisional one is the carry-forward, a choice
   * made in the moment before the listing landed, for whichever project it then
   * named.
   */
  if (leftBehind(choice, storageKey)) {
    setChoice({ key: storageKey, resolved: keyResolved, value: NOTHING_CHOSEN })
  }
  const chosen = choice.value

  /**
   * The stored record, or nothing at all while the key is provisional.
   *
   * The read is gated as well as the write. Until the chassis names the
   * project, `storageKey` is the literal `default` — a key some earlier build
   * may have written to — and a preference that belongs to a project this may
   * not be is not one to apply.
   *
   * Read on every render rather than seeded once: it is one `getItem` and one
   * short `JSON.parse`, and it is what makes the key and the value it names
   * incapable of disagreeing.
   */
  const stored = keyResolved ? readAppearance(storageKey) : undefined

  /** This browser's answer: what was chosen this visit, over what was stored. */
  const preference: AppearancePreference = {
    theme: chosen.theme ?? stored?.theme,
    density: chosen.density ?? stored?.density
  }

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
   *
   * The re-stamp is the second half of the carry-forward: a choice made before
   * the listing landed has now been written to a real project's record, and it
   * belongs to that project and to no other one this tab is later pointed at.
   * It changes the stamp and never the value, so it costs a render and no
   * paint.
   *
   * **Its condition is a render saved and not a rule**, and that is written
   * down because a mutation row was written for it and could not discriminate.
   * A stamp that named another project has already been discarded during
   * render, so by the time this runs the only stamp that is not this key's is
   * the provisional one — and an unconditional re-stamp would write back the
   * stamp already there in every other case.
   */
  useEffect(() => {
    if (!keyResolved) return
    if (chosen.theme === undefined && chosen.density === undefined) return
    writeAppearance(storageKey, chosen)
    if (!choice.resolved) {
      setChoice({ key: storageKey, resolved: true, value: chosen })
    }
  }, [storageKey, keyResolved, chosen, choice.resolved])

  // Each setter records the member and the key it was chosen under, and leaves
  // the other member alone: a `density` stored because the *theme* was picked
  // would be a value nobody chose, outranking the file for ever.
  const setTheme = useCallback(
    (theme: ThemeChoice) => {
      setChoice((previous) => ({
        key: storageKey,
        resolved: keyResolved,
        value: { ...keptFrom(previous, storageKey), theme }
      }))
    },
    [storageKey, keyResolved]
  )
  const setDensity = useCallback(
    (density: Density) => {
      setChoice((previous) => ({
        key: storageKey,
        resolved: keyResolved,
        value: { ...keptFrom(previous, storageKey), density }
      }))
    },
    [storageKey, keyResolved]
  )

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
    setChoice({ key: storageKey, resolved: true, value: {} })
    return 'cleared'
  }, [keyResolved, storageKey])

  /**
   * **The default is a rung only once it is the file's, and only once this
   * browser's own answer has been read.**
   *
   * Both halves are the same rule — apply nothing this desk has not
   * established. `projectDefault` is the schema standing in until the file
   * answers, so applying it before then is applying a value nobody wrote. And
   * the record is unreadable until the chassis names the project, so falling
   * back to the *file's* value before then would paint `light` over a viewer
   * whose stored answer is `dark`. With a stored `dark` and a file that says
   * `light`, this applies `dark` once and neither `system` nor `light` ever.
   *
   * A member the viewer has actually chosen needs neither: it is the top of the
   * ladder and nothing below it can change the answer, so it applies the moment
   * it is picked — including in the moment before the listing lands, which is
   * the one case where the choice is honoured on screen and stored later.
   */
  const knownDefault = keyResolved && projectDefaultKnown ? projectDefault : undefined
  // Destructured rather than held as an object: `effectiveAppearance` returns a
  // fresh one on every render, so the memo below depends on the two values it
  // actually carries.
  const { theme, density } = effectiveAppearance(preference, knownDefault)

  // **The two configuration values that are applied rather than displayed**,
  // and they are applied here because this is where the ladder is resolved.
  // The theme used to be applied from the file, in `DeskConfigProvider`, which
  // is a layer that cannot see the viewer's own choice; the density was applied
  // nowhere at all — decoded, stored, offered in the menu, and read by nothing.
  // One ladder, one resolution, two attributes. `undefined` writes neither.
  useAppliedTheme(theme)
  useAppliedDensity(density)

  // Named only once the file has answered. The flag is the *file's* alone —
  // this line is about the project and not about what is in force — so it is
  // shown on a desk whose root is still unknown, and the effective value above
  // is not.
  const namedDefault = projectDefaultKnown ? projectDefault : undefined

  const value = useMemo<AppearanceApi>(
    () => ({
      theme,
      density,
      projectDefault: namedDefault,
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
      namedDefault,
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
