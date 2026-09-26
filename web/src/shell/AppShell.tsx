import { AssistantReferenceProvider } from '../chat/AssistantReference'
import { msg, useLocale } from '../i18n'
import { InspectorPresentationContext, type InspectorPresentation } from './InspectorPresentation'
/**
 * The frame: header, navigation, main, contextual right pane and status strip.
 *
 * Header, rail, workspace and status strip follow the shell grid. Inside the
 * workspace, main and the contextual right pane share one rounded boundary. The
 * wrapper is a div, so every landmark keeps its own name and role. Collapse
 * still writes custom properties; nothing here computes a height.
 *
 * **Main never remounts on a shell state change.** No pane flag conditionally
 * renders it, no shell value keys it, and it never moves between parents. That
 * is not a style preference: `AuthorView` holds an unsaved buffer in component
 * state, and a frame that remounted `<main>` when a pane opened would throw
 * that buffer away for a layout change. There is a test that types, toggles
 * navigation and the right pane, and reads the buffer back.
 *
 * The skip link is the first element in the DOM and is a **plain class with a
 * `:focus` rule**, not `VisuallyHidden`: that primitive applies clip/1px/
 * absolute as inline styles, which a class rule cannot beat without
 * `!important` on every property.
 */
import { ConnectionPaneContext, type ConnectionPaneRequest } from '../connections/ConnectionPaneContext'
import { WebSourcePane } from '../connections/WebSourcePane'
import { ConnectionsPane } from '../connections/ConnectionsPane'
import { ProviderIcon } from '../connections/ProviderIcon'
import { providerName } from '../connections/registry'
import { Tooltip } from '../ui/Tooltip'
import { IconAssistant, IconDetails, IconActivity, IconGear, IconChevronLeft } from './icons'
import { TooltipProvider } from '../ui/Tooltip'
import { useLocation, useMatch } from 'react-router-dom'
import {
  Fragment,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode
} from 'react'
import { useDeskConfigRead, useEffectiveConfig } from '../config/DeskConfigProvider'
import { useFileListing } from '../files/queries'
import { DetailsSlotContext, type DetailsSlot } from './DetailsSlot'
import { DiagnosticsContext, useConnectionLog } from './Diagnostics'
import { HeaderBar } from './HeaderBar'
import { InspectorSlotContext, InspectorSizeProvider, type InspectorSlot } from './InspectorSlot'
import { LeftRail } from './LeftRail'
import { SettingsNavigationProvider } from './SettingsNavigation'
import { BriefContext, type BriefSubject } from '../briefs/context'
import { BriefPane } from '../briefs/BriefPane'
import { IconBrief } from './icons'
import { RightPane } from './RightPane'
import { StatusStrip } from './StatusStrip'
import { AppearanceProvider } from './appearanceState'
import { useMeasuredBox } from './measured'
import { inspectorGeometry } from './inspectorGeometry'
import { overlayGeometry, PACK_PEEK } from './overlayGeometry'
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
  useConnectionLog()
  const [rightTool, setRightTool] = useState<'brief' | 'details' | 'activity' | 'runtime' | 'diagnostics' | null>(null)
  const [briefSubject, setBriefSubject] = useState<BriefSubject | null>(null)
  const [briefHeaderTarget, setBriefHeaderTarget] = useState<HTMLDivElement | null>(null)
  const registerBrief = useCallback((subject: BriefSubject) => { setBriefSubject(subject); return () => setBriefSubject(current => current === subject ? null : current) }, [])
  const [auxOpen, setAuxOpen] = useState(false)
  const [toolExpanded, setToolExpanded] = useState(false)
  const expandButtonRef = useRef<HTMLButtonElement | null>(null)
  const paneWasCovering = useRef(false)

  const [detailsTarget, setDetailsTarget] = useState<HTMLDivElement | null>(null)
  const [detailsClaims, setDetailsClaims] = useState(0)
  const [inspection, setInspection] = useState<{ owner: number; content: ReactNode } | null>(null)
  const inspectionNumber = useRef(0)
  const detailOpener = useRef<HTMLElement | null>(null)
  const route = useLocation()
  useEffect(() => { setInspection(null); setRightTool(null); setAuxOpen(false); setToolExpanded(false) }, [route.pathname])
  const inspect = useCallback((content: ReactNode, opener: HTMLElement | null) => {
    const owner = ++inspectionNumber.current
    detailOpener.current = opener
    setInspection({ owner, content }); setRightTool('details'); setAuxOpen(true)
    return () => setInspection(current => current?.owner === owner ? null : current)
  }, [])
  const dismissInspection = useCallback(() => {
    if (detailOpener.current?.isConnected) detailOpener.current.focus()
    setInspection(null)
    setRightTool(null); setAuxOpen(false)
  }, [])
  const detailsClaim = useCallback(() => {
    setDetailsClaims(count => count + 1)
    return () => setDetailsClaims(count => Math.max(0, count - 1))
  }, [])
  const revealDetails = useCallback(() => {
    if (document.activeElement instanceof HTMLElement) inspectionGestureRef.current = document.activeElement
    setInspection(null); setRightTool('details'); setAuxOpen(true)
  }, [])

  const [routePresentation, setPresentation] = useState<InspectorPresentation | null>(null)
  const registerPresentation = useCallback((next: InspectorPresentation) => {
    setPresentation(next)
    return () => setPresentation(current => current === next ? null : current)
  }, [])
  // A utility overlays the route presentation; closing it restores the exact
  // route state rather than registering another last-writer-wins portal.
  const [connection, setConnection] = useState<ConnectionPaneRequest | null>(null)
  const [connectionWidth, setConnectionWidth] = useState(480)
  const [connectionTarget, setConnectionTarget] = useState<HTMLDivElement | null>(null)
  const [busyChatId, setBusyChatId] = useState<string | undefined>()
  const connectionOpener = useRef<HTMLElement | null>(null)
  const connectionChat = useRef<string | undefined>(undefined)
  const closeConnection = useCallback((focusComposer = false, restoreFocus = true) => {
    setConnection(null); setBusyChatId(undefined)
    if (!restoreFocus) return
    requestAnimationFrame(() => {
      if (focusComposer && connectionChat.current) {
        const panel = [...document.querySelectorAll<HTMLElement>('[data-chat-id]')].find(node => node.dataset.chatId === connectionChat.current)
        const composer = panel?.querySelector<HTMLTextAreaElement>('textarea')
        if (composer) { composer.focus({ preventScroll: true }); return }
      }
      if (connectionOpener.current?.isConnected) connectionOpener.current.focus({ preventScroll: true })
    })
  }, [])
  useEffect(() => { setConnection(null); setBusyChatId(undefined) }, [route.key])
  const openConnection = useCallback((request: ConnectionPaneRequest) => {
    connectionOpener.current = request.opener
    connectionChat.current = request.chatId
    setConnection(request)
  }, [])
  const connectionContext = useMemo(() => ({ open: openConnection, busyChatId, activeChatId: connection?.chatId,
    close: (options?: { restoreFocus?: boolean }) => closeConnection(false, options?.restoreFocus ?? true)
  }), [openConnection, busyChatId, connection?.chatId, closeConnection])
  const presentation: InspectorPresentation | null = connection ? {
    title: connection.source === 'web' ? msg('Add link') : connection.provider ? providerName(connection.provider, connection.descriptor) : msg('Connections'),
    available: true, open: true, onOpenChange: open => { if (!open) closeConnection() },
    width: connectionWidth, onResize: setConnectionWidth, onReset: () => setConnectionWidth(480),
    minimumMainWidth: 560, maximumWidth: 560, closeOnEscape: true, restoreFocusRef: connectionOpener
  } : routePresentation
  const settingsPage = useMatch('/admin') !== null
  const packPage = useMatch('/packs/:packId') !== null
  const workspaceTools = !!briefSubject || packPage || Boolean(routePresentation?.workspaceTools)
  // Only routes with real pane content may restore an Assistant or preview.
  // Old saved Inspector/Console flags never open empty UI on other pages.
  const routeAvailable = routePresentation ? routePresentation.available !== false : packPage
  const inspectorOpen = connection ? true : rightTool ? auxOpen : routeAvailable && (presentation?.open ?? shell.inspector.open)
  const openInspector = useCallback(() => {
    if (!routeAvailable) return
    setRightTool(null)
    if (routePresentation) routePresentation.onOpenChange(true)
    else shell.openInspector()
  }, [routeAvailable, routePresentation, shell.openInspector])
  const toggleInspector = useCallback(() => {
    if (rightTool && !connection) { setAuxOpen(value => !value); return }
    if (presentation) presentation.onOpenChange(!presentation.open)
    else if (routeAvailable) shell.toggleInspector()
  }, [rightTool, connection, presentation, routeAvailable, shell.toggleInspector])
  const openDiagnostics = useCallback(() => {
    inspectionGestureRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    closeConnection(false, false)
    setRightTool('diagnostics'); setAuxOpen(true)
  }, [closeConnection])
  const detailsVisible = !connection && inspectorOpen && rightTool === 'details'
  const detailsSlot = useMemo<DetailsSlot>(() => ({ target: detailsTarget, open: detailsVisible, claim: detailsClaim, reveal: revealDetails, inspect, dismissInspection }), [detailsTarget, detailsVisible, detailsClaim, revealDetails, inspect, dismissInspection])
  const paneTitle = connection ? presentation!.title : rightTool === 'brief' ? msg('Brief') : rightTool === 'details' ? msg('Details') : rightTool === 'activity' ? msg('Activity') : rightTool === 'runtime' ? msg('Runtime details') : rightTool === 'diagnostics' ? msg('Diagnostics') : presentation?.title ?? (packPage ? msg('Assistant') : msg('Details'))
  const chooseTool = (next: typeof rightTool) => {
    if (document.activeElement instanceof HTMLElement) inspectionGestureRef.current = document.activeElement
    if (connection) closeConnection(false, false)
    if (next === null) { setRightTool(null); if (rightTool || connection || !inspectorOpen) openInspector(); else toggleInspector() }
    else { setRightTool(next); setAuxOpen(rightTool !== next || !inspectorOpen || Boolean(connection)) }
  }
  const { config, declaredPanes } = useEffectiveConfig()
  const [railDrawerOpen, setRailDrawerOpen] = useState(false)

  // Drawer focus returns to navigation's header control or the contextual
  // action that opened the right pane. Neither uses a Radix Dialog.Trigger.
  const railOpenerRef = useRef<HTMLButtonElement | null>(null)
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
  const leadingWidths = useRef(new Map<symbol, number>())
  const [leadingWidth, setLeadingWidth] = useState(0)
  const requestLeadingWidth = useCallback((pixels: number) => {
    const owner = Symbol()
    const update = () => setLeadingWidth(Math.max(0, ...leadingWidths.current.values()))
    leadingWidths.current.set(owner, Number.isFinite(pixels) ? Math.max(0, pixels) : 0); update()
    return () => { leadingWidths.current.delete(owner); update() }
  }, [])
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
  const inspectorLayout = inspectorGeometry(workspaceBox?.width ? workspaceBox.width - (workspaceTools ? 40 : 0) : undefined,
    presentation?.width ?? shell.inspectorWidth ?? (briefSubject && !declaredPanes.inspectorWidth ? 440 : config.panes.inspector.width),
    Math.max(minimumMainWidth, presentation?.minimumMainWidth ?? 0, briefSubject ? 480 : 0), defaultInspectorIsDrawer, presentation?.maximumWidth)
  const inspectorWidth = inspectorLayout.width
  const overlayLayout = overlayGeometry(workspaceBox?.width ? workspaceBox.width - 42 : defaultInspectorIsDrawer ? 800 : 1200, shell.overlayWidth, leadingWidth)
  const takeover = workspaceTools && (inspectorLayout.drawer || toolExpanded && !connection && overlayLayout.full)
  const overlay = workspaceTools && inspectorOpen && toolExpanded && !connection && !takeover
  const covering = inspectorOpen && (overlay || takeover)
  const inspectorIsDrawer = !workspaceTools && inspectorLayout.drawer
  // Publish the rendered width, including drawer/CSS caps, rather than the
  // saved preference or the project's configured default.
  // Pane measurements are isolated from the shell and its route controls.
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
    setToolExpanded(false)
    toggleInspector()
    const gesture = presentation?.restoreFocusRef?.current ?? inspectionGestureRef.current
    const focus = () => {
      if (gesture?.isConnected && gesture.getClientRects().length) gesture.focus({ preventScroll: true })
      else document.getElementById('main')?.focus({ preventScroll: true })
    }
    // The pack remains inert until React applies the return to the document.
    if (covering) requestAnimationFrame(focus)
    else focus()
  }, [inspectorOpen, toggleInspector, presentation?.restoreFocusRef, covering])
  const returnToPack = useCallback(() => {
    setToolExpanded(false)
    // If both columns cannot fit, returning exposes the pack and closes the pane.
    if (inspectorLayout.drawer) closeInspector()
    else requestAnimationFrame(() => expandButtonRef.current?.focus({ preventScroll: true }))
  }, [inspectorLayout.drawer, closeInspector])

  useLayoutEffect(() => {
    const wasCovering = paneWasCovering.current
    paneWasCovering.current = covering
    if (covering && !wasCovering) {
      // Keep an already focused composer. A keyboard focus in the newly inert
      // pack must move into the pane, while the global navigation stays usable.
      const active = document.activeElement
      if (active === document.body || workspaceElement?.querySelector('#main')?.contains(active)) {
        expandButtonRef.current?.focus({ preventScroll: true })
      }
    }
  }, [covering, workspaceElement])

  useEffect(() => {
    if ((!presentation?.closeOnEscape && !covering) || !inspectorOpen) return
    const close = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return
      // Menus, drawers and the splitter own Escape while they have focus.
      if (event.target instanceof Element && event.target.closest('[role="menu"], [role="listbox"], [role="dialog"], [role="separator"]')) return
      event.preventDefault()
      if (covering && !connection) returnToPack()
      else closeInspector()
    }
    document.addEventListener('keydown', close)
    return () => document.removeEventListener('keydown', close)
  }, [presentation?.closeOnEscape, inspectorOpen, closeInspector, covering, connection, returnToPack])
  const slot = useMemo<InspectorSlot>(
    () => ({
      open: !connection && !rightTool && inspectorOpen,
      size: 0, // Live measurements are supplied only to useInspectorSlot readers.
      tab: inspectorTab,
      setTab: setInspectorTab,
      target: inspectorTarget,
      headerTarget: inspectorHeaderTarget,
      claim,
      reveal,
      close: closeInspector,
      mainCovered: covering,
      revealMain: covering ? returnToPack : undefined,
      requestWorkingWidth, minimumMainWidth, requestLeadingWidth
    }),
    [connection, rightTool, inspectorOpen, inspectorTab, inspectorTarget, inspectorHeaderTarget, claim, reveal, closeInspector, requestWorkingWidth, minimumMainWidth, requestLeadingWidth, covering, returnToPack]
  )

  useEffect(
    () =>
      installShortcuts({
        toggleRail: railIsDrawer ? () => setRailDrawerOpen((open) => !open) : shell.toggleRail,
        toggleInspector,
        toggleConsole: openDiagnostics
      }),
    [railIsDrawer, shell.toggleRail, toggleInspector, openDiagnostics]
  )

  // Navigation uses configuration; the contextual pane uses its bounded viewer
  // preference. Collapse only chooses between the effective size and zero.
  const style = {
    '--rail-w': `${config.panes.left.width}px`,
    '--inspector-w': `${inspectorWidth}px`,
    '--overlay-w': `${overlayLayout.width}px`,
    '--overlay-peek': `${PACK_PEEK + leadingWidth}px`,
    '--console-h': '0px',
    '--rail-current': railIsDrawer
      ? '0px'
      : shell.left.mode === 'expanded'
        ? 'var(--rail-w)'
        : settingsPage ? '0px' : 'var(--rail-w-icon)',
    '--inspector-current':
      inspectorOpen && !inspectorIsDrawer ? 'var(--inspector-w)' : '0px',
    '--console-current': '0px'
  } as CSSProperties

  return (
    <DiagnosticsContext.Provider value={openDiagnostics}>
    <ConnectionPaneContext.Provider value={connectionContext}>
    <InspectorPresentationContext.Provider value={registerPresentation}>
    <InspectorSlotContext.Provider value={slot}><InspectorSizeProvider pane={inspectorPane} open={inspectorOpen}>
    <DetailsSlotContext.Provider value={detailsSlot}><BriefContext.Provider value={registerBrief}>
      <AssistantReferenceProvider><SettingsNavigationProvider>
        <div className="desk" style={style} data-rail-drawer={railIsDrawer || undefined}>
          <a className="desk-skip" href="#main">{msg("Skip to main content")}</a>

          <HeaderBar railIsDrawer={railIsDrawer}
            railOpen={railIsDrawer ? railDrawerOpen : shell.left.mode === 'expanded'}
            onToggleRail={railIsDrawer ? () => setRailDrawerOpen(open => !open) : shell.toggleRail}
            railOpenerRef={railOpenerRef} />

          <LeftRail
            mode={shell.left.mode}
            asDrawer={railIsDrawer}
            drawerOpen={railDrawerOpen}
            onDrawerOpenChange={setRailDrawerOpen}
            openerRef={railOpenerRef}
          />

          <div className="desk-workspace" data-workspace-tools={workspaceTools || undefined} data-tool-takeover={takeover && inspectorOpen || undefined} data-tool-overlay={overlay || undefined} ref={setWorkspaceElement}>
            <main id="main" tabIndex={-1} className="desk-main" inert={covering || undefined}>
              <div className="desk-measure">{children}</div>
            </main>

            {overlay && <button type="button" className="desk-pane-backdrop" tabIndex={-1} aria-hidden="true" onPointerDown={event => event.preventDefault()} aria-label={msg('Return to split view')} onClick={returnToPack} />}
            {inspectorOpen && !inspectorIsDrawer && !takeover && !overlay && <PaneDivider label={paneTitle} controls="desk-inspector"
              value={inspectorWidth} min={inspectorLayout.min} max={inspectorLayout.max}
              preview={{ element: workspaceElement, property: '--inspector-current' }}
              onChange={presentation?.onResize ?? shell.resizeInspector} onReset={presentation?.onReset ?? shell.resetInspectorWidth}
              onCollapse={closeInspector} />}

            <RightPane
              title={paneTitle}
              brief={briefSubject && <BriefPane key={briefSubject.id} subject={briefSubject} headerTarget={briefHeaderTarget} />} publishBriefHeaderTarget={setBriefHeaderTarget}
              workspaceTools={workspaceTools} tool={rightTool ?? 'route'} expanded={overlay} fullWidth={takeover}
              contextTitle={routePresentation?.contextTitle ?? briefSubject?.title}
              returnLabel={route.pathname.startsWith('/jobs/') ? msg('Back to job') : undefined}
              expandButtonRef={expandButtonRef}
              onExpand={() => setToolExpanded(true)} onReturn={returnToPack}
              resize={overlay ? { value: overlayLayout.width, min: overlayLayout.min, max: overlayLayout.max, onChange: shell.resizeOverlay, onReset: shell.resetOverlayWidth, preview: { element: workspaceElement, property: '--overlay-preview' } } : undefined}
              publishDetailsTarget={setDetailsTarget} hasDetails={detailsClaims > 0}
              inspection={inspection ? <Fragment key={inspection.owner}>{inspection.content}</Fragment> : undefined}
              open={inspectorOpen}
              onClose={closeInspector}
              asDrawer={inspectorIsDrawer}
              declaredWidth={briefSubject || presentation || declaredPanes.inspectorWidth || minimumMainWidth > 0 || shell.inspectorWidth !== undefined ? inspectorWidth : undefined}
              publishTarget={publishTarget}
              publishHeaderTarget={setInspectorHeaderTarget}
              publishPane={publishPane}
              restoreFocusRef={presentation?.restoreFocusRef ?? inspectionGestureRef}
              showEmpty={inspectorClaims === 0}
              utility={Boolean(connection)}
              publishUtilityTarget={setConnectionTarget}
              navigation={connection && <>{(connection.provider || packPage) && <button type="button" className="desk-icon-button" aria-label={connection.provider ? msg('All connections') : msg('Back to assistant')}
                onClick={() => connection.provider ? setConnection({ ...connection, provider: undefined }) : closeConnection()}><IconChevronLeft /></button>}{connection.provider && <ProviderIcon provider={connection.provider} descriptor={connection.descriptor} />}</>}
            />

            {workspaceTools && <nav className="desk-tool-rail" aria-label={msg('Workspace tools')}>
              {([{ key: null, label: msg('Assistant'), icon: <IconAssistant /> }, { key: 'brief', label: msg('Brief'), icon: <IconBrief /> }, { key: 'details', label: msg('Details'), icon: <IconDetails /> }, { key: 'activity', label: msg('Activity'), icon: <IconActivity /> }, { key: 'runtime', label: msg('Runtime details'), icon: <IconGear /> }] as const).filter(item => item.key === 'brief' ? !!briefSubject : item.key === null ? routeAvailable : true).map(item => <Tooltip key={item.label} content={item.label} side="left"><button type="button" className="desk-icon-button" aria-label={item.label} aria-controls="desk-inspector" aria-pressed={!connection && inspectorOpen && rightTool === item.key} onClick={() => chooseTool(item.key)}><span>{item.icon}</span></button></Tooltip>)}
            </nav>}

            {connection?.source === 'web' ? <WebSourcePane request={connection} target={connectionTarget} onClose={closeConnection} onAttached={() => closeConnection(true)} onBusy={setBusyChatId} /> : connection && <ConnectionsPane key={connection.provider ?? 'catalog'} request={connection} target={connectionTarget}
              onProvider={(provider, descriptor) => setConnection({ ...connection, provider, descriptor })} onClose={closeConnection} onAttached={() => closeConnection(true)} onBusy={setBusyChatId} />}


          </div>

          <StatusStrip />
        </div>
      </SettingsNavigationProvider></AssistantReferenceProvider>
    </BriefContext.Provider></DetailsSlotContext.Provider>
    </InspectorSizeProvider></InspectorSlotContext.Provider>
    </InspectorPresentationContext.Provider>
    </ConnectionPaneContext.Provider>
    </DiagnosticsContext.Provider>
  )
}
