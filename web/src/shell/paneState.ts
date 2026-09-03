/**
 * Which panes are open, per project, per browser.
 *
 * **Phase A persists collapse flags and the console's channel, and nothing
 * else.** No widths, no heights: phase A is collapse-only, so a stored number
 * no viewer can change would be a record of a choice nobody made. Sizes come
 * from the config's `panes` block and then from the built-in defaults; when
 * drag arrives it writes a `v2` record, which is why the version is in the
 * value rather than implied by its shape.
 *
 * **And it persists them one pane at a time.** A record is preferred over the
 * configuration on the next read, so a section written because a *sibling* was
 * toggled is a built-in default that silently outranks `jpack-desk.json` for
 * ever. Touched is therefore three bits and not one, the write serializes only
 * the panes those bits name, and the re-seed replaces only the panes they do
 * not.
 *
 * **The project is the root the chassis pinned**, not the runtime's config
 * path: a project without a `jpack.json` has no config path, so every such
 * project on one origin shared the literal `default` record. Until the file
 * listing answers, the key is provisional and nothing is written under it.
 *
 * This is per-viewer convenience, not configuration. Strictness lives in the
 * config decoder — a typo'd key there is refused by name — and here an
 * unreadable record is simply discarded, silently: a banner about a browser's
 * own storage would be the desk reporting on the wrong thing.
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
import type { PanesConfig } from '../config/deskConfig'

export type LeftRailMode = 'expanded' | 'icons'
export type ConsoleTab = 'connection' | 'calls' | 'files' | 'notices'

export interface ShellState {
  left: { mode: LeftRailMode }
  inspector: { open: boolean }
  console: { open: boolean; tab: ConsoleTab }
}

export const BUILT_IN_SHELL_STATE: ShellState = {
  left: { mode: 'expanded' },
  inspector: { open: false },
  console: { open: false, tab: 'connection' }
}

const RECORD_VERSION = 1

/** One record's key. Exported so a test — and Admin's reset — can name it. */
export function shellStateKey(projectKey: string): string {
  return `jpack-desk:shell:v${RECORD_VERSION}:${projectKey}`
}

/**
 * The project this layout belongs to.
 *
 * **The identity is the chassis' own project root**, not the runtime's
 * `configPath`, and the difference is the whole of the fix. A project with no
 * `jpack.json` reports no config path, so every configless project on one
 * origin mapped to the literal `default` and shared a single record — two
 * different directories, one layout, and a README that claimed per-project
 * isolation while the code did not have it. The root is pinned by the chassis
 * at startup and is there whether or not a runtime configuration file exists.
 *
 * `default` remains, and now means exactly one thing: the identity has not
 * been read yet. Nothing is written under it — see `ShellStateProvider`.
 *
 * The slug is truncated for legibility and the hash is taken over the **whole**
 * untruncated path and appended after the truncation, so two long paths sharing
 * a 64-character prefix get different keys rather than one shared record.
 */
export function projectKey(projectRoot: string | undefined): string {
  const path = (projectRoot ?? '').trim()
  if (path === '') return 'default'
  const slug =
    path
      .toLowerCase()
      .replace(/[^a-z0-9._-]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 64) || 'project'
  return `${slug}-${fnv1a32(path)}`
}

/** True where the chassis has actually told the page which project this is. */
export function identityIsResolved(projectRoot: string | undefined): boolean {
  return typeof projectRoot === 'string' && projectRoot.trim() !== ''
}

/** FNV-1a, 32-bit, as eight lowercase hex digits. */
function fnv1a32(text: string): string {
  let hash = 0x811c9dc5
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index)
    hash = Math.imul(hash, 0x01000193)
  }
  return (hash >>> 0).toString(16).padStart(8, '0')
}

/**
 * Read one record.
 *
 * **Every access is in try/catch**, and not defensively: a private window or a
 * browser set to block site data *throws* on the accessor itself rather than
 * answering null, and a thrown accessor must still render the default shell.
 * A value that is not JSON, is not an object, or carries another version is
 * discarded — a record written by a different shell is not a record this one
 * can honour, and half-honouring it would restore a layout nobody chose.
 */
export function readShellState(key: string): Partial<ShellState> | undefined {
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

  const restored: Partial<ShellState> = {}
  const left = record.left as { mode?: unknown } | undefined
  if (left && (left.mode === 'expanded' || left.mode === 'icons')) {
    restored.left = { mode: left.mode }
  }
  const inspector = record.inspector as { open?: unknown } | undefined
  if (inspector && typeof inspector.open === 'boolean') {
    restored.inspector = { open: inspector.open }
  }
  const consoleRecord = record.console as { open?: unknown; tab?: unknown } | undefined
  if (consoleRecord && typeof consoleRecord.open === 'boolean') {
    const tab = consoleRecord.tab
    restored.console = {
      open: consoleRecord.open,
      tab: isConsoleTab(tab) ? tab : BUILT_IN_SHELL_STATE.console.tab
    }
  }
  return restored
}

function isConsoleTab(value: unknown): value is ConsoleTab {
  return value === 'connection' || value === 'calls' || value === 'files' || value === 'notices'
}

/** Which panes a viewer has moved by hand. One bit each, never one for all. */
export interface TouchedPanes {
  left: boolean
  inspector: boolean
  console: boolean
}

export const NOTHING_TOUCHED: TouchedPanes = { left: false, inspector: false, console: false }

/**
 * Write **only the sections the viewer chose**, and nothing beside them.
 *
 * A record is preferred over the configuration on the next read, so a section
 * serialized on the strength of a sibling's toggle is a built-in default that
 * silently outranks `panes` in `jpack-desk.json` for ever. One global touched
 * bit did exactly that: collapsing the Console stored a rail mode and an
 * Inspector flag nobody had chosen.
 */
export function writeShellState(key: string, state: ShellState, touched: TouchedPanes): void {
  const record: Record<string, unknown> = { v: RECORD_VERSION }
  if (touched.left) record.left = state.left
  if (touched.inspector) record.inspector = state.inspector
  if (touched.console) record.console = state.console
  try {
    window.localStorage.setItem(key, JSON.stringify(record))
  } catch {
    // A viewer whose storage refuses writes still gets a working desk; the
    // layout simply does not survive the reload, which is what they asked for.
  }
}

/**
 * Forget this project's layout on this machine.
 *
 * Exactly one key, and the reason is worth the sentence: `localStorage.clear()`
 * would take the session token's neighbour keys and every other project's
 * layout with it, and a "reset panes" control that logged the viewer out of
 * something would be a control that lied about its scope.
 */
export function resetShellState(key: string): boolean {
  try {
    window.localStorage.removeItem(key)
    // Read back, because `removeItem` resolves on a storage that keeps the
    // value: the page must not say "cleared" on the strength of having asked.
    return window.localStorage.getItem(key) === null
  } catch {
    return false
  }
}

/** The breakpoints a restored layout is clamped to. */
export interface Viewport {
  /** True below 900px: the rail is a drawer, so it restores as icons. */
  railIsDrawer: boolean
  /** True below 1100px: the inspector is a drawer, so it restores closed. */
  inspectorIsDrawer: boolean
}

/**
 * The layout to paint first: stored record, then the config's defaults, then
 * the built-in ones — clamped to the viewport that is actually there.
 *
 * The clamp is the point of the function. A layout remembered on a wide
 * monitor would otherwise open on a laptop with main squeezed to nothing, and
 * a viewer who has never seen this desk before would meet it broken.
 */
export function initialShellState(
  stored: Partial<ShellState> | undefined,
  configured: PanesConfig | undefined,
  viewport: Viewport
): ShellState {
  const fromConfig: ShellState = {
    left: { mode: configured?.left.mode ?? BUILT_IN_SHELL_STATE.left.mode },
    inspector: { open: configured?.inspector.open ?? BUILT_IN_SHELL_STATE.inspector.open },
    console: {
      open: configured?.console.open ?? BUILT_IN_SHELL_STATE.console.open,
      tab: BUILT_IN_SHELL_STATE.console.tab
    }
  }
  const merged: ShellState = {
    left: stored?.left ?? fromConfig.left,
    inspector: stored?.inspector ?? fromConfig.inspector,
    console: stored?.console ?? fromConfig.console
  }
  return {
    left: { mode: viewport.railIsDrawer ? 'icons' : merged.left.mode },
    inspector: { open: viewport.inspectorIsDrawer ? false : merged.inspector.open },
    console: merged.console
  }
}

/**
 * What a reset did. Three outcomes, because they are three different facts and
 * a page that reported all of them as "Cleared." would be stating one it never
 * observed.
 */
export type ResetOutcome = 'cleared' | 'refused' | 'unresolved'

export interface ShellStateApi extends ShellState {
  toggleRail: () => void
  toggleInspector: () => void
  toggleConsole: () => void
  setConsoleTab: (tab: ConsoleTab) => void
  /** The key this project's record lives under, for Admin › Panes. */
  storageKey: string
  /** False while the chassis has not yet said which project this is. */
  keyResolved: boolean
  /**
   * Forget this project's layout: cancel any pending write, clear the record,
   * put the live state back on the configured defaults — and say what happened.
   */
  resetPanes: () => ResetOutcome
}

/**
 * A real default value, so the shell renders correctly for the render before
 * the config query resolves and in any test that provides no provider.
 */
const DEFAULT_API: ShellStateApi = {
  ...BUILT_IN_SHELL_STATE,
  toggleRail: () => {},
  toggleInspector: () => {},
  toggleConsole: () => {},
  setConsoleTab: () => {},
  storageKey: shellStateKey('default'),
  keyResolved: false,
  resetPanes: () => 'unresolved'
}

const ShellStateContext = createContext<ShellStateApi>(DEFAULT_API)

export function useShellState(): ShellStateApi {
  return useContext(ShellStateContext)
}

const WRITE_DEBOUNCE_MS = 250

export function ShellStateProvider({
  projectIdentity,
  panes,
  viewport,
  children
}: {
  /**
   * The chassis' project root, or undefined until the file listing answers.
   * Not the runtime's `configPath`: a project with no `jpack.json` has none,
   * and every such project would share one record.
   */
  projectIdentity?: string
  panes?: PanesConfig
  viewport: Viewport
  children: ReactNode
}) {
  const keyResolved = identityIsResolved(projectIdentity)
  const storageKey = shellStateKey(projectKey(projectIdentity))
  // Seeded once per key. StrictMode mounts the provider twice; a lazy
  // initializer runs per mount and reads the same record, which is why the
  // read is idempotent and carries no side effect of its own.
  const [state, setState] = useState<ShellState>(() =>
    initialShellState(readShellState(storageKey), panes, viewport)
  )

  /**
   * Which panes the viewer has moved themselves — **one bit each**.
   *
   * Everything below re-seeds the layout, and this is what stops it doing so
   * over someone's shoulder. One global bit made a single toggle speak for
   * all three: collapsing the Console froze the rail and the Inspector against
   * a configuration file that had not been read yet, and then wrote the
   * built-in values for both into a record that outranks the file for ever.
   *
   * It is a ref rather than state because re-seeding must not itself be a
   * render.
   */
  const touched = useRef<TouchedPanes>({ ...NOTHING_TOUCHED })

  /**
   * Why the seed is not once and for all.
   *
   * All three inputs arrive **after** the first paint. The project identity is
   * unknown until the chassis' file listing answers with its root, the config's
   * `panes` block is unknown until `jpack-desk.json` has been read over the
   * file API, and the viewport can change under a viewer who rotates a tablet
   * or drags a window wider. A provider that seeded only on mount would honour
   * none of them: the layout in the file would silently never apply, which is
   * worse than not reading the file at all — the key is in the schema, in Admin
   * and in the README, and it would do nothing.
   *
   * So the seed is re-taken whenever any of them actually changes, **per pane
   * and only where that pane is untouched**. The viewport is in the signature
   * for the same reason the others are: a desk opened narrow clamps the rail to
   * icons and the Inspector closed, and widening it must give an untouched pane
   * back the layout the file asked for rather than leave a 56px rail behind.
   * The config's signature is its own JSON, because the object identity changes
   * on every query render while its content does not.
   */
  const panesSignature = JSON.stringify(panes ?? null)
  const viewportSignature = `${viewport.railIsDrawer}|${viewport.inspectorIsDrawer}`
  const seededFrom = useRef(`${storageKey}|${panesSignature}|${viewportSignature}`)
  useEffect(() => {
    const signature = `${storageKey}|${panesSignature}|${viewportSignature}`
    if (seededFrom.current === signature) return
    seededFrom.current = signature
    const chosen = touched.current
    if (chosen.left && chosen.inspector && chosen.console) return
    setState((previous) => {
      const seeded = initialShellState(readShellState(storageKey), panes, viewport)
      return {
        left: chosen.left ? previous.left : seeded.left,
        inspector: chosen.inspector ? previous.inspector : seeded.inspector,
        console: chosen.console ? previous.console : seeded.console
      }
    })
    // `panes` and `viewport` are read at the moment the seed is re-taken and
    // are deliberately not dependencies: their signatures above are, and an
    // object identity that changes on every render is not a change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey, panesSignature, viewportSignature])

  /**
   * Debounced, idempotent — and **only for the sections the viewer chose**.
   *
   * The guard is the other half of the seed above, and without it the seed is
   * defeated by the shell writing to itself. A record persisted on mount is
   * preferred over the config on the next read, so a desk that had ever been
   * opened would ignore `panes` in `jpack-desk.json` for ever: the first visit
   * stored the built-in defaults, and every visit after that restored them.
   * The same write also beat the config on the *first* visit whenever the file
   * read answered more than 250ms after the listing did.
   *
   * A layout that came from the file or from the built-ins does not need
   * storing: it is re-derived on every load from inputs that are still there.
   * The only thing worth a record is a choice this browser's viewer made, and
   * `touched` is exactly that, one pane at a time. It is read inside the
   * timeout so the toggles stay ordinary state updates.
   *
   * **Nothing is written under a provisional key.** Until the chassis says
   * which project this is, `storageKey` is the literal `default` — a record
   * written there is one project's layout stored under a name that belongs to
   * whichever project answers slowly next.
   */
  const pending = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => {
    if (!keyResolved) return
    const timer = setTimeout(() => {
      pending.current = undefined
      const chosen = touched.current
      if (!chosen.left && !chosen.inspector && !chosen.console) return
      writeShellState(storageKey, state, chosen)
    }, WRITE_DEBOUNCE_MS)
    pending.current = timer
    return () => {
      clearTimeout(timer)
      if (pending.current === timer) pending.current = undefined
    }
  }, [storageKey, state, keyResolved])

  /**
   * The reset, here rather than in Admin.
   *
   * Admin used to call `resetShellState` directly and report success without
   * asking. Three things were wrong with that and all three are fixed by the
   * control living where the state does: a debounced write already in flight
   * rewrote the key a moment later, an early press cleared the provisional
   * `default` key instead of this project's, and a storage that refused the
   * deletion was reported as "Cleared."
   */
  const resetPanes = useCallback((): ResetOutcome => {
    if (!keyResolved) return 'unresolved'
    // Belt and braces, and labelled as such: the state change below re-runs
    // the write effect, whose cleanup cancels this same timer, so breaking
    // these three lines alone leaves every test green. What makes a pending
    // write harmless is the touched reset further down — that is the line the
    // mutation row breaks, and this one is kept because a reset that reads as
    // "cancel, then clear, then re-seed" is worth more than three lines saved.
    if (pending.current !== undefined) {
      clearTimeout(pending.current)
      pending.current = undefined
    }
    const cleared = resetShellState(storageKey)
    touched.current = { ...NOTHING_TOUCHED }
    setState(initialShellState(undefined, panes, viewport))
    return cleared ? 'cleared' : 'refused'
    // `panes` and `viewport` are read at the moment the reset runs, exactly as
    // the seed reads them; their signatures are the dependencies.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keyResolved, storageKey, panesSignature, viewportSignature])

  // `touched` is marked in the handler and never inside an updater: an
  // updater must stay pure, and React reserves the right to run one twice.
  const toggleRail = useCallback(() => {
    touched.current.left = true
    setState((previous) => ({
      ...previous,
      left: { mode: previous.left.mode === 'expanded' ? 'icons' : 'expanded' }
    }))
  }, [])
  const toggleInspector = useCallback(() => {
    touched.current.inspector = true
    setState((previous) => ({ ...previous, inspector: { open: !previous.inspector.open } }))
  }, [])
  const toggleConsole = useCallback(() => {
    touched.current.console = true
    setState((previous) => ({
      ...previous,
      console: { ...previous.console, open: !previous.console.open }
    }))
  }, [])
  const setConsoleTab = useCallback((tab: ConsoleTab) => {
    touched.current.console = true
    setState((previous) => ({ ...previous, console: { ...previous.console, tab } }))
  }, [])

  const value = useMemo<ShellStateApi>(
    () => ({
      ...state,
      toggleRail,
      toggleInspector,
      toggleConsole,
      setConsoleTab,
      storageKey,
      keyResolved,
      resetPanes
    }),
    [
      state,
      toggleRail,
      toggleInspector,
      toggleConsole,
      setConsoleTab,
      storageKey,
      keyResolved,
      resetPanes
    ]
  )
  return createElement(ShellStateContext.Provider, { value }, children)
}
