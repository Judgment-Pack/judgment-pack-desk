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
 * The project this layout belongs to, from the `configPath` the runtime
 * reports beside its pack listing.
 *
 * One desk on one origin serves whichever project it was started against, and
 * a layout chosen for a three-pack project is not the one chosen for a forty-
 * pack one — so the record is keyed per project rather than per origin.
 *
 * The slug is truncated for legibility and the hash is taken over the **whole**
 * untruncated path and appended after the truncation, so two long paths sharing
 * a 64-character prefix get different keys rather than one shared record.
 */
export function projectKey(configPath: string | undefined): string {
  const path = (configPath ?? '').trim()
  if (path === '') return 'default'
  const slug =
    path
      .toLowerCase()
      .replace(/[^a-z0-9._-]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 64) || 'project'
  return `${slug}-${fnv1a32(path)}`
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

function writeShellState(key: string, state: ShellState): void {
  try {
    window.localStorage.setItem(
      key,
      JSON.stringify({
        v: RECORD_VERSION,
        left: state.left,
        inspector: state.inspector,
        console: state.console
      })
    )
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
export function resetShellState(key: string): void {
  try {
    window.localStorage.removeItem(key)
  } catch {
    // Nothing to do: a storage that will not delete had nothing stored either.
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

export interface ShellStateApi extends ShellState {
  toggleRail: () => void
  toggleInspector: () => void
  toggleConsole: () => void
  setConsoleTab: (tab: ConsoleTab) => void
  /** The key this project's record lives under, for Admin › Panes. */
  storageKey: string
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
  storageKey: shellStateKey('default')
}

const ShellStateContext = createContext<ShellStateApi>(DEFAULT_API)

export function useShellState(): ShellStateApi {
  return useContext(ShellStateContext)
}

const WRITE_DEBOUNCE_MS = 250

export function ShellStateProvider({
  projectKey: key,
  panes,
  viewport,
  children
}: {
  projectKey: string
  panes?: PanesConfig
  viewport: Viewport
  children: ReactNode
}) {
  const storageKey = shellStateKey(key)
  // Seeded once per key. StrictMode mounts the provider twice; a lazy
  // initializer runs per mount and reads the same record, which is why the
  // read is idempotent and carries no side effect of its own.
  const [state, setState] = useState<ShellState>(() =>
    initialShellState(readShellState(storageKey), panes, viewport)
  )

  /**
   * True once the viewer has moved a pane themselves.
   *
   * Everything below re-seeds the layout, and this is what stops it doing so
   * over someone's shoulder. It is a ref rather than state because re-seeding
   * must not itself be a render.
   */
  const touched = useRef(false)

  /**
   * Why the seed is not once and for all.
   *
   * Both of the inputs arrive **after** the first paint. The project key is
   * unknown until `list_packs` answers with a `configPath`, and the config's
   * `panes` block is unknown until `jpack-desk.json` has been read over the
   * file API. A provider that seeded only on mount would therefore honour
   * neither: the layout in the file would silently never apply, which is worse
   * than not reading the file at all — the key is in the schema, in Admin and
   * in the README, and it would do nothing.
   *
   * So the seed is re-taken whenever either input actually changes, and never
   * after the viewer has touched a pane. The signature is the config's own
   * JSON, because the object identity changes on every query render while its
   * content does not.
   */
  const panesSignature = JSON.stringify(panes ?? null)
  const seededFrom = useRef(`${storageKey}|${panesSignature}`)
  useEffect(() => {
    const signature = `${storageKey}|${panesSignature}`
    if (seededFrom.current === signature) return
    seededFrom.current = signature
    if (touched.current) return
    setState(initialShellState(readShellState(storageKey), panes, viewport))
    // `panes` and `viewport` are read at the moment the seed is re-taken and
    // are deliberately not dependencies: a viewport change must not re-clamp a
    // layout the viewer has since chosen by hand.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storageKey, panesSignature])

  /**
   * Debounced, idempotent — and **only for a layout the viewer chose**.
   *
   * The guard is the other half of the seed above, and without it the seed is
   * defeated by the shell writing to itself. A record persisted on mount is
   * preferred over the config on the next read, so a desk that had ever been
   * opened would ignore `panes` in `jpack-desk.json` for ever: the first visit
   * stored the built-in defaults, and every visit after that restored them.
   * The same write also beat the config on the *first* visit whenever the file
   * read answered more than 250ms after `list_packs` did.
   *
   * A layout that came from the file or from the built-ins does not need
   * storing: it is re-derived on every load from inputs that are still there.
   * The only thing worth a record is a choice this browser's viewer made, and
   * `touched` is exactly that. It is read inside the timeout so the toggles
   * stay ordinary state updates.
   */
  useEffect(() => {
    const timer = setTimeout(() => {
      if (!touched.current) return
      writeShellState(storageKey, state)
    }, WRITE_DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [storageKey, state])

  // `touched` is marked in the handler and never inside an updater: an
  // updater must stay pure, and React reserves the right to run one twice.
  const toggleRail = useCallback(() => {
    touched.current = true
    setState((previous) => ({
      ...previous,
      left: { mode: previous.left.mode === 'expanded' ? 'icons' : 'expanded' }
    }))
  }, [])
  const toggleInspector = useCallback(() => {
    touched.current = true
    setState((previous) => ({ ...previous, inspector: { open: !previous.inspector.open } }))
  }, [])
  const toggleConsole = useCallback(() => {
    touched.current = true
    setState((previous) => ({
      ...previous,
      console: { ...previous.console, open: !previous.console.open }
    }))
  }, [])
  const setConsoleTab = useCallback((tab: ConsoleTab) => {
    touched.current = true
    setState((previous) => ({ ...previous, console: { ...previous.console, tab } }))
  }, [])

  const value = useMemo<ShellStateApi>(
    () => ({ ...state, toggleRail, toggleInspector, toggleConsole, setConsoleTab, storageKey }),
    [state, toggleRail, toggleInspector, toggleConsole, setConsoleTab, storageKey]
  )
  return createElement(ShellStateContext.Provider, { value }, children)
}
