/**
 * The frame: header, rail, main, inspector, console, strip.
 *
 * All six are **direct children of one CSS grid**, which is what makes the
 * landmark count come out right — a `banner`, a `navigation`, a `main`, a
 * `complementary`, a `region` and a `contentinfo`, each exactly once — and
 * what makes collapse the writing of a custom property rather than a
 * measurement in JavaScript. Nothing here computes a height.
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
import { Tooltip } from 'radix-ui'
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode
} from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useFileListing } from '../files/queries'
import { BottomPane } from './BottomPane'
import { HeaderBar } from './HeaderBar'
import { InspectorSlotContext, type InspectorSlot } from './InspectorSlot'
import { LeftRail } from './LeftRail'
import { RightPane } from './RightPane'
import { StatusStrip } from './StatusStrip'
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
  return (
    <ShellStateProvider
      projectIdentity={listing.data?.root}
      panes={config.panes}
      viewport={{ railIsDrawer, inspectorIsDrawer }}
    >
      <Tooltip.Provider delayDuration={300}>
        <ShellFrame railIsDrawer={railIsDrawer} inspectorIsDrawer={inspectorIsDrawer}>
          {children}
        </ShellFrame>
      </Tooltip.Provider>
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
  inspectorIsDrawer,
  children
}: {
  railIsDrawer: boolean
  inspectorIsDrawer: boolean
  children: ReactNode
}) {
  const shell = useShellState()
  const { config } = useEffectiveConfig()
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
  const [inspectorTab, setInspectorTab] = useState<string | null>(null)
  const publishTarget = useCallback((target: HTMLDivElement | null) => {
    setInspectorTarget(target)
  }, [])
  const inspectorWidth = config.panes.inspector.width
  const slot = useMemo<InspectorSlot>(
    () => ({
      open: shell.inspector.open,
      size: shell.inspector.open ? inspectorWidth : 0,
      tab: inspectorTab,
      setTab: setInspectorTab,
      target: inspectorTarget
    }),
    [shell.inspector.open, inspectorWidth, inspectorTab, inspectorTarget]
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

  // Geometry is six custom properties on the grid element and nothing else.
  // The first three are the configured sizes — decoded, shown on Admin as the
  // effective values, and until now not applied to anything; the last three
  // are what collapse writes, each one of the two values above it.
  const style = {
    '--rail-w': `${config.panes.left.width}px`,
    '--inspector-w': `${inspectorWidth}px`,
    '--console-h': `${config.panes.console.height}px`,
    '--rail-current': railIsDrawer
      ? '0px'
      : shell.left.mode === 'expanded'
        ? 'var(--rail-w)'
        : 'var(--rail-w-icon)',
    '--inspector-current':
      shell.inspector.open && !inspectorIsDrawer ? 'var(--inspector-w)' : '0px',
    '--console-current': shell.console.open ? 'var(--console-h)' : '0px'
  } as CSSProperties

  return (
    <InspectorSlotContext.Provider value={slot}>
      <div className="desk" style={style}>
        <a className="desk-skip" href="#main">
          Skip to main content
        </a>

        <HeaderBar
          inspectorOpen={shell.inspector.open}
          inspectorIsDrawer={inspectorIsDrawer}
          consoleOpen={shell.console.open}
          onToggleInspector={shell.toggleInspector}
          onToggleConsole={shell.toggleConsole}
          inspectorOpenerRef={inspectorOpenerRef}
          railIsDrawer={railIsDrawer}
          railDrawerOpen={railDrawerOpen}
          onOpenRail={() => setRailDrawerOpen(true)}
          railOpenerRef={railOpenerRef}
        />

        <LeftRail
          mode={shell.left.mode}
          onToggle={railIsDrawer ? () => setRailDrawerOpen((open) => !open) : shell.toggleRail}
          asDrawer={railIsDrawer}
          drawerOpen={railDrawerOpen}
          onDrawerOpenChange={setRailDrawerOpen}
          openerRef={railOpenerRef}
        />

        <main id="main" tabIndex={-1} className="desk-main">
          <div className="desk-measure">{children}</div>
        </main>

        <RightPane
          open={shell.inspector.open}
          onClose={shell.toggleInspector}
          asDrawer={inspectorIsDrawer}
          width={inspectorWidth}
          publishTarget={publishTarget}
          openerRef={inspectorOpenerRef}
        />

        <BottomPane
          open={shell.console.open}
          tab={shell.console.tab}
          onTabChange={shell.setConsoleTab}
        />

        <StatusStrip consoleOpen={shell.console.open} onToggleConsole={shell.toggleConsole} />
      </div>
    </InspectorSlotContext.Provider>
  )
}
