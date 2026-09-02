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
import { useEffect, useState, type CSSProperties, type ReactNode } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { usePacks } from '../mcp/queries'
import { BottomPane } from './BottomPane'
import { HeaderBar } from './HeaderBar'
import { LeftRail } from './LeftRail'
import { RightPane, INSPECTOR_WIDTH } from './RightPane'
import { StatusStrip } from './StatusStrip'
import { ShellStateProvider, projectKey, useShellState } from './paneState'
import { installShortcuts } from './shortcuts'
import { INSPECTOR_DRAWER_BELOW, RAIL_DRAWER_BELOW, useMediaQuery } from './useMediaQuery'

/**
 * The provider layer.
 *
 * Separate from the frame because the project key is only known once
 * `list_packs` has answered, and a provider that re-keyed mid-render would
 * lose the layout it had just restored. `configPath` is undefined until then,
 * which reads as the literal `default` — the same record a project whose
 * runtime reports no config path gets, and the honest one for both.
 */
export function AppShell({ children }: { children: ReactNode }) {
  const { data } = usePacks()
  const railIsDrawer = useMediaQuery(RAIL_DRAWER_BELOW)
  const inspectorIsDrawer = useMediaQuery(INSPECTOR_DRAWER_BELOW)
  const { config } = useEffectiveConfig()
  return (
    <ShellStateProvider
      projectKey={projectKey(data?.configPath)}
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
  const [railDrawerOpen, setRailDrawerOpen] = useState(false)

  useEffect(
    () =>
      installShortcuts({
        toggleRail: railIsDrawer ? () => setRailDrawerOpen((open) => !open) : shell.toggleRail,
        toggleInspector: shell.toggleInspector,
        toggleConsole: shell.toggleConsole
      }),
    [railIsDrawer, shell.toggleRail, shell.toggleInspector, shell.toggleConsole]
  )

  // Collapse is three custom properties on the grid element. Nothing else.
  const style = {
    '--rail-current': railIsDrawer
      ? '0px'
      : shell.left.mode === 'expanded'
        ? 'var(--rail-w)'
        : 'var(--rail-w-icon)',
    '--inspector-current':
      shell.inspector.open && !inspectorIsDrawer ? `${INSPECTOR_WIDTH}px` : '0px',
    '--console-current': shell.console.open ? 'var(--console-h)' : '0px'
  } as CSSProperties

  return (
    <div className="desk" style={style}>
      <a className="desk-skip" href="#main">
        Skip to main content
      </a>

      <HeaderBar
        inspectorOpen={shell.inspector.open}
        consoleOpen={shell.console.open}
        onToggleInspector={shell.toggleInspector}
        onToggleConsole={shell.toggleConsole}
      />

      <LeftRail
        mode={shell.left.mode}
        onToggle={railIsDrawer ? () => setRailDrawerOpen((open) => !open) : shell.toggleRail}
        asDrawer={railIsDrawer}
        drawerOpen={railDrawerOpen}
        onDrawerOpenChange={setRailDrawerOpen}
      />

      <main id="main" tabIndex={-1} className="desk-main">
        <div className="desk-measure">{children}</div>
      </main>

      <RightPane
        open={shell.inspector.open}
        onClose={shell.toggleInspector}
        asDrawer={inspectorIsDrawer}
      />

      <BottomPane
        open={shell.console.open}
        tab={shell.console.tab}
        onTabChange={shell.setConsoleTab}
      />

      <StatusStrip consoleOpen={shell.console.open} onToggleConsole={shell.toggleConsole} />
    </div>
  )
}
