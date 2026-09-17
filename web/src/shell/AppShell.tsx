import { msg, useLocale } from '../i18n'
import { InspectorPresentationContext, type InspectorPresentation } from './InspectorPresentation'
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
import { DetailsSlotContext, type DetailsSlot } from './DetailsSlot'
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
  useLocale()
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
  useLocale()
  const shell = useShellState()
  const [detailsTarget, setDetailsTarget] = useState<HTMLDivElement | null>(null)
  const [detailsClaims, setDetailsClaims] = useState(0)
  const [showDetails, setShowDetails] = useState(false)
  const [bottomMaximized, setBottomMaximized] = useState(false)
  const detailsClaim = useCallback(() => {
    setDetailsClaims(count => count + 1)
    return () => setDetailsClaims(count => Math.max(0, count - 1))
  }, [])
  const revealDetails = useCallback(() => { setShowDetails(true); shell.openConsole() }, [shell.openConsole])
  const detailsSlot = useMemo<DetailsSlot>(() => ({ target: detailsTarget, open: shell.console.open && showDetails, claim: detailsClaim, reveal: revealDetails }), [detailsTarget, shell.console.open, showDetails, detailsClaim, revealDetails])
  const [presentation, setPresentation] = useState<InspectorPresentation | null>(null)
  const registerPresentation = useCallback((next: InspectorPresentation) => {
    setPresentation(next)
    return () => setPresentation(current => current === next ? null : current)
  }, [])
  const inspectorOpen = presentation?.open ?? shell.inspector.open
  const openInspector = useCallback(() => {
    if (presentation) presentation.onOpenChange(true)
    else shell.openInspector()
  }, [presentation, shell.openInspector])
  const toggleInspector = useCallback(() => {
    if (presentation) presentation.onOpenChange(!presentation.open)
    else shell.toggleInspector()
  }, [presentation, shell.toggleInspector])
  const settingsPage = useMatch('/admin') !== null
  const packPage = useMatch('/packs/:packId') !== null
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
  const consoleOpenerRef = useRef<HTMLButtonElement | null>(null)
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
  const [inspectorHeaderTarget, setInspectorHeaderTarget] = useState<HTMLDivElement | null>(null)
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
  const bottomRoom = Math.max(0, (workspaceBox?.height || 700) - 2)
  const bottomMax = Math.min(1600, Math.max(bottomRoom - 120, Math.min(80, bottomRoom)))
  const bottomMin = Math.min(120, bottomMax)
  const bottomHeight = bottomMaximized ? bottomMax : Math.max(bottomMin, Math.min(bottomMax, shell.consoleHeight ?? config.panes.console.height))
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
    presentation?.width ?? shell.inspectorWidth ?? config.panes.inspector.width,
    Math.max(minimumMainWidth, presentation?.minimumMainWidth ?? 0), defaultInspectorIsDrawer, presentation?.maximumWidth)
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
    openInspector()
  }, [openInspector])
  const closeInspector = useCallback(() => {
    if (!inspectorOpen) return
    toggleInspector()
    const gesture = inspectionGestureRef.current
    if (gesture?.isConnected && gesture.getClientRects().length) gesture.focus()
    else inspectorOpenerRef.current?.focus()
  }, [inspectorOpen, toggleInspector])
  useEffect(() => {
    if (!presentation?.closeOnEscape || !inspectorOpen) return
    const close = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return
      // Menus, drawers and the splitter own Escape while they have focus.
      if (event.target instanceof Element && event.target.closest('[role="menu"], [role="listbox"], [role="dialog"], [role="separator"]')) return
      event.preventDefault()
      closeInspector()
    }
    document.addEventListener('keydown', close)
    return () => document.removeEventListener('keydown', close)
  }, [presentation?.closeOnEscape, inspectorOpen, closeInspector])
  const slot = useMemo<InspectorSlot>(
    () => ({
      open: inspectorOpen,
      size: inspectorOpen ? (inspectorBox?.width ?? 0) : 0,
      tab: inspectorTab,
      setTab: setInspectorTab,
      target: inspectorTarget,
      headerTarget: inspectorHeaderTarget,
      claim,
      reveal,
      close: closeInspector,
      requestWorkingWidth
    }),
    [inspectorOpen, inspectorBox, inspectorTab, inspectorTarget, inspectorHeaderTarget, claim, reveal, closeInspector, requestWorkingWidth]
  )

  useEffect(
    () =>
      installShortcuts({
        toggleRail: railIsDrawer ? () => setRailDrawerOpen((open) => !open) : shell.toggleRail,
        toggleInspector,
        toggleConsole: shell.toggleConsole
      }),
    [railIsDrawer, shell.toggleRail, toggleInspector, shell.toggleConsole]
  )

  // Rail and console use configuration; Inspector uses its bounded viewer
  // preference. Collapse only chooses between the effective size and zero.
  const style = {
    '--rail-w': `${config.panes.left.width}px`,
    '--inspector-w': `${inspectorWidth}px`,
    '--console-h': `${bottomHeight}px`,
    '--rail-current': railIsDrawer
      ? '0px'
      : settingsPage || shell.left.mode === 'expanded'
        ? 'var(--rail-w)'
        : 'var(--rail-w-icon)',
    '--inspector-current':
      inspectorOpen && !inspectorIsDrawer ? 'var(--inspector-w)' : '0px',
    '--console-current': shell.console.open ? 'var(--console-h)' : '0px'
  } as CSSProperties

  return (
    <InspectorPresentationContext.Provider value={registerPresentation}>
    <InspectorSlotContext.Provider value={slot}>
    <DetailsSlotContext.Provider value={detailsSlot}>
      <SettingsNavigationProvider>
        <div className="desk" style={style} data-rail-drawer={railIsDrawer || undefined}>
          <a className="desk-skip" href="#main">{msg("Skip to main content")}</a>

          <HeaderBar
            inspectorAvailable={presentation?.available}
            consoleOpenerRef={consoleOpenerRef}
            inspectorTitle={presentation?.title ?? (packPage ? "Assistant" : "Inspector")}
            inspectorOpen={inspectorOpen}
            inspectorIsDrawer={inspectorIsDrawer}
            consoleOpen={shell.console.open}
            onToggleInspector={() => { inspectionGestureRef.current = null; toggleInspector() }}
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

            {inspectorOpen && !inspectorIsDrawer && <PaneDivider label={presentation?.title ?? (packPage ? msg("Assistant") : msg("Inspector"))} controls="desk-inspector"
              value={inspectorWidth} min={inspectorLayout.min} max={inspectorLayout.max}
              onChange={presentation?.onResize ?? shell.resizeInspector} onReset={presentation?.onReset ?? shell.resetInspectorWidth}
              onCollapse={() => { inspectorOpenerRef.current?.focus(); toggleInspector() }} />}

            <RightPane
              title={presentation?.title ?? (packPage ? msg("Assistant") : undefined)}
              open={inspectorOpen}
              onClose={closeInspector}
              asDrawer={inspectorIsDrawer}
              declaredWidth={presentation || declaredPanes.inspectorWidth || minimumMainWidth > 0 || shell.inspectorWidth !== undefined ? inspectorWidth : undefined}
              publishTarget={publishTarget}
              publishHeaderTarget={setInspectorHeaderTarget}
              publishPane={publishPane}
              openerRef={inspectorOpenerRef}
              restoreFocusRef={inspectionGestureRef}
              showEmpty={inspectorClaims === 0}
            />

            {shell.console.open && <PaneDivider orientation="horizontal" label={msg("Details and activity")} controls="desk-console"
              value={bottomHeight} min={bottomMin} max={bottomMax}
              onChange={height => { setBottomMaximized(false); shell.resizeConsole(height) }}
              onReset={() => { setBottomMaximized(false); shell.resetConsoleHeight() }}
              onCollapse={() => { consoleOpenerRef.current?.focus(); shell.toggleConsole() }} />}
            <BottomPane
              open={shell.console.open}
              tab={shell.console.tab}
              onTabChange={tab => { setShowDetails(false); shell.setConsoleTab(tab) }}
              details={detailsClaims > 0}
              showDetails={showDetails}
              onDetails={() => setShowDetails(true)}
              publishTarget={setDetailsTarget}
              onClose={() => { consoleOpenerRef.current?.focus(); shell.toggleConsole() }}
              maximized={bottomMaximized}
              onMaximize={() => setBottomMaximized(value => !value)}
            />
          </div>

          <StatusStrip consoleOpen={shell.console.open} onToggleConsole={shell.toggleConsole} />
        </div>
      </SettingsNavigationProvider>
    </DetailsSlotContext.Provider>
    </InspectorSlotContext.Provider>
    </InspectorPresentationContext.Provider>
  )
}
