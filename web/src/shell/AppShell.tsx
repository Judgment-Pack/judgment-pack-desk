/**
 * The frame: header, rail, main, inspector, console, strip.
 *
 * Header, rail, workspace and status strip follow the shell grid. Inside the
 * workspace, main, Inspector and console share one rounded boundary. The
 * wrapper is a div, so every landmark keeps its own name and role. Collapse
 * still writes custom properties; nothing here computes a height.
 *
 * **Main never remounts on a shell state change.** No pane flag conditionally
 * renders it, no shell value keys it, and it never moves between parents. That
 * is not a style preference: `AuthorView` holds an unsaved buffer in component
 * state, and a frame that remounted `<main>` when a pane opened would throw
 * that buffer away for a layout change. There is a test that types, toggles
 * all three panes, and reads the buffer back.
 *
 * The skip link is the first element in the DOM and is a **plain class with a
 * `:focus` rule**, not `VisuallyHidden`: that primitive applies clip/1px/
 * absolute as inline styles, which a class rule cannot beat without
 * `!important` on every property.
 */
import { TooltipProvider } from '../ui/Tooltip'
import { useMatch } from 'react-router-dom'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode
} from 'react'
import { useDeskConfigRead, useEffectiveConfig } from '../config/DeskConfigProvider'
import { useFileListing } from '../files/queries'
import { BottomPane } from './BottomPane'
import { HeaderBar } from './HeaderBar'
import { InspectorSlotContext, type InspectorSlot } from './InspectorSlot'
import { LeftRail } from './LeftRail'
import { SettingsNavigationProvider } from './SettingsNavigation'
import { RightPane } from './RightPane'
import { StatusStrip } from './StatusStrip'
import { AppearanceProvider } from './appearanceState'
import { useMeasuredBox } from './measured'
import { inspectorGeometry } from './inspectorGeometry'
import { PaneDivider } from '../ui/PaneDivider'
import { ShellStateProvider, useShellState } from './paneState'
import { installShortcuts } from './shortcuts'
import { INSPECTOR_DRAWER_BELOW, RAIL_DRAWER_BELOW, useMediaQuery } from './useMediaQuery'

/**
 * The provider layer.
 *
 * Separate from the frame because the project identity is only known once the
 * chassis' file listing has answered, and a provider that re-keyed mid-render
 * would lose the layout it had just restored. That listing's `root` is the
 * identity, not the runtime's `configPath`: the root is pinned at startup and
 * is there whether or not the project carries a `jpack.json`, and keying on a
 * config path meant every configless project on this origin shared one record.
 * Until it answers the key is provisional and nothing is written under it.
 *
 * The listing is the query `/author` and the Create dialog already use, under
 * the same `['desk-files']` key, so this costs one request per connection and
 * not one per route.
 */
export function AppShell({ children }: { children: ReactNode }) {
  const listing = useFileListing()
  const railIsDrawer = useMediaQuery(RAIL_DRAWER_BELOW)
  const inspectorIsDrawer = useMediaQuery(INSPECTOR_DRAWER_BELOW)
  const { config } = useEffectiveConfig()
  // Whether `config.appearance` is the file's or the schema standing in for it.
  // The two are indistinguishable in the value, and the appearance ladder must
  // not apply the second as though it were the first.
  const configRead = useDeskConfigRead()
  return (
    <ShellStateProvider
      projectIdentity={listing.data?.root}
      panes={config.panes}
      viewport={{ railIsDrawer, inspectorIsDrawer }}
    >
      {/* The same identity, for the same reason: a preference belongs to a
          viewer *on a project*, and the record is keyed on the root the
          chassis pinned. `appearance` from the file is handed in as the
          default this desk falls back to, never as the answer. */}
      <AppearanceProvider
        projectIdentity={listing.data?.root}
        projectDefault={config.appearance}
        projectDefaultKnown={configRead}
      >
        <TooltipProvider>
          <ShellFrame railIsDrawer={railIsDrawer} inspectorIsDrawer={inspectorIsDrawer}>
            {children}
          </ShellFrame>
        </TooltipProvider>
      </AppearanceProvider>
    </ShellStateProvider>
  )
}

/**
 * Below 900px the rail is an overlay drawer, and its opener is in the header
 * rather than in the rail — a control inside a closed drawer opens nothing.
 * That is why `railIsDrawer` is threaded into `HeaderBar` at all.
 */
function ShellFrame({
  railIsDrawer,
  inspectorIsDrawer: defaultInspectorIsDrawer,
  children
}: {
  railIsDrawer: boolean
  inspectorIsDrawer: boolean
  children: ReactNode
}) {
  const shell = useShellState()
  const settingsPage = useMatch('/admin') !== null
  const { config, declaredPanes } = useEffectiveConfig()
  const [railDrawerOpen, setRailDrawerOpen] = useState(false)

  /**
   * The two header controls that open a drawer, held by reference.
   *
   * Radix restores focus to a dialog's own `Dialog.Trigger` when it closes.
   * Neither drawer has one — both openers live in the header, which is a
   * separate grid child — so each drawer's `onCloseAutoFocus` puts focus back
   * on the button that opened it. Without this, Escape out of either drawer
   * dropped focus on `<body>`.
   */
  const railOpenerRef = useRef<HTMLButtonElement | null>(null)
  const inspectorOpenerRef = useRef<HTMLButtonElement | null>(null)
  const inspectionGestureRef = useRef<HTMLElement | null>(null)

  /**
   * The Inspector slot, held **here** and not in the pane.
   *
   * A provider around `RightPane` is a sibling of `<main>`, so a route calling
   * `useInspectorSlot()` read the closed default and its portal went nowhere.
   * The target arrives through a callback ref, which fires on every mount and
   * unmount: a drawer that starts closed reports null and reports the element
   * when it opens, and a breakpoint swap replaces the node rather than keeping
   * a detached one.
   */
  const [inspectorTarget, setInspectorTarget] = useState<HTMLElement | null>(null)
  const [inspectorPane, setInspectorPane] = useState<HTMLElement | null>(null)
  const [inspectorTab, setInspectorTab] = useState<string | null>(null)
  /**
   * How many routes are publishing into the slot right now.
   *
   * A portal writes into a DOM node that is not React's child here, so the
   * pane cannot see its own contents; this count is how it learns. Held in the
   * frame beside the target, because both describe the same slot.
   */
  const [inspectorClaims, setInspectorClaims] = useState(0)
  const claim = useCallback(() => {
    setInspectorClaims((count) => count + 1)
    return () => setInspectorClaims((count) => Math.max(0, count - 1))
  }, [])
  const publishTarget = useCallback((target: HTMLDivElement | null) => {
    setInspectorTarget(target)
  }, [])
  const publishPane = useCallback((pane: HTMLElement | null) => {
    setInspectorPane(pane)
  }, [])
  const [workspaceElement, setWorkspaceElement] = useState<HTMLDivElement | null>(null)
  const workspaceBox = useMeasuredBox(workspaceElement)
  const workingWidths = useRef(new Map<symbol, number>())
  const [minimumMainWidth, setMinimumMainWidth] = useState(0)
  const requestWorkingWidth = useCallback((pixels: number) => {
    const owner = Symbol()
    const update = () => setMinimumMainWidth(Math.max(0, ...workingWidths.current.values()))
    workingWidths.current.set(owner, pixels); update()
    return () => { workingWidths.current.delete(owner); update() }
  }, [])
  // Measure the whole workspace so opening the inspector cannot change the
  // input to this decision and cause a dock/drawer feedback loop.
  const inspectorLayout = inspectorGeometry(workspaceBox?.width,
    shell.inspectorWidth ?? config.panes.inspector.width, minimumMainWidth, defaultInspectorIsDrawer)
  const inspectorWidth = inspectorLayout.width
  const inspectorIsDrawer = inspectorLayout.drawer
  // Publish the rendered width, including drawer/CSS caps, rather than the
  // saved preference or the project's configured default.
  const inspectorBox = useMeasuredBox(inspectorPane)
  /**
   * Opening the pane because a route was asked to inspect something.
   *
   * A gesture, not a seed. `toggleInspector` marks the pane as chosen, which is
   * correct here: the reader picked a member and the panel is what they picked
   * it for.
   */
  // **Set, not flip.** "If closed, toggle" is the same gesture read twice, and
  // React's StrictMode runs an effect twice on purpose — so an arrival that
  // opened the pane immediately closed it again, and the link somebody sent
  // landed on a closed Inspector. `openInspector` is idempotent, so calling it
  // twice is calling it once and calling it on an open pane does nothing.
  const reveal = useCallback(() => {
    if (document.activeElement instanceof HTMLElement && document.activeElement !== document.body) {
      // Keep the actual route gesture through the dock/drawer remount.
      if (!document.activeElement.closest('#desk-inspector')) inspectionGestureRef.current = document.activeElement
    }
    shell.openInspector()
  }, [shell.openInspector])
  const slot = useMemo<InspectorSlot>(
    () => ({
      open: shell.inspector.open,
      size: shell.inspector.open ? (inspectorBox?.width ?? 0) : 0,
      tab: inspectorTab,
      setTab: setInspectorTab,
      target: inspectorTarget,
      claim,
      reveal,
      requestWorkingWidth
    }),
    [shell.inspector.open, inspectorBox, inspectorTab, inspectorTarget, claim, reveal, requestWorkingWidth]
  )

  useEffect(
    () =>
      installShortcuts({
        toggleRail: railIsDrawer ? () => setRailDrawerOpen((open) => !open) : shell.toggleRail,
        toggleInspector: shell.toggleInspector,
        toggleConsole: shell.toggleConsole
      }),
    [railIsDrawer, shell.toggleRail, shell.toggleInspector, shell.toggleConsole]
  )

  // Rail and console use configuration; Inspector uses its bounded viewer
  // preference. Collapse only chooses between the effective size and zero.
  const style = {
    '--rail-w': `${config.panes.left.width}px`,
    '--inspector-w': `${inspectorWidth}px`,
    '--console-h': `${config.panes.console.height}px`,
    '--rail-current': railIsDrawer
      ? '0px'
      : settingsPage || shell.left.mode === 'expanded'
        ? 'var(--rail-w)'
        : 'var(--rail-w-icon)',
    '--inspector-current':
      shell.inspector.open && !inspectorIsDrawer ? 'var(--inspector-w)' : '0px',
    '--console-current': shell.console.open ? 'var(--console-h)' : '0px'
  } as CSSProperties

  return (
    <InspectorSlotContext.Provider value={slot}>
      <SettingsNavigationProvider>
        <div className="desk" style={style} data-rail-drawer={railIsDrawer || undefined}>
          <a className="desk-skip" href="#main">
            Skip to main content
          </a>

          <HeaderBar
            inspectorOpen={shell.inspector.open}
            inspectorIsDrawer={inspectorIsDrawer}
            consoleOpen={shell.console.open}
            onToggleInspector={() => { inspectionGestureRef.current = null; shell.toggleInspector() }}
            onToggleConsole={shell.toggleConsole}
            inspectorOpenerRef={inspectorOpenerRef}
            railIsDrawer={railIsDrawer}
            railDrawerOpen={railDrawerOpen}
            onOpenRail={() => setRailDrawerOpen(true)}
            railOpenerRef={railOpenerRef}
          />

          <LeftRail
            mode={settingsPage ? 'expanded' : shell.left.mode}
            onToggle={railIsDrawer ? () => setRailDrawerOpen((open) => !open) : shell.toggleRail}
            asDrawer={railIsDrawer}
            drawerOpen={railDrawerOpen}
            onDrawerOpenChange={setRailDrawerOpen}
            openerRef={railOpenerRef}
          />

          <div className="desk-workspace" ref={setWorkspaceElement}>
            <main id="main" tabIndex={-1} className="desk-main">
              <div className="desk-measure">{children}</div>
            </main>

            {shell.inspector.open && !inspectorIsDrawer && <PaneDivider label="Inspector" controls="desk-inspector"
              value={inspectorWidth} min={inspectorLayout.min} max={inspectorLayout.max}
              onChange={shell.resizeInspector} onReset={shell.resetInspectorWidth}
              onCollapse={() => { inspectorOpenerRef.current?.focus(); shell.toggleInspector() }} />}

            <RightPane
              open={shell.inspector.open}
              onClose={shell.toggleInspector}
              asDrawer={inspectorIsDrawer}
              declaredWidth={declaredPanes.inspectorWidth || minimumMainWidth > 0 || shell.inspectorWidth !== undefined ? inspectorWidth : undefined}
              publishTarget={publishTarget}
              publishPane={publishPane}
              openerRef={inspectorOpenerRef}
              restoreFocusRef={inspectionGestureRef}
              showEmpty={inspectorClaims === 0}
            />

            <BottomPane
              open={shell.console.open}
              tab={shell.console.tab}
              onTabChange={shell.setConsoleTab}
            />
          </div>

          <StatusStrip consoleOpen={shell.console.open} onToggleConsole={shell.toggleConsole} />
        </div>
      </SettingsNavigationProvider>
    </InspectorSlotContext.Provider>
  )
}
