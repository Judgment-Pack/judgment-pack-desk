import { Tooltip } from '../ui/Tooltip'
import { type ReactElement } from 'react'
/** Primary navigation stays about destinations. Tests and pack flows live
 * inside Packs; project file editing is available from the project menu.
 * The shell never runs tests or fetches graph inventory to draw navigation. */
import { Dialog, DropdownMenu, Separator, VisuallyHidden } from 'radix-ui'
import { useRef, type RefObject } from 'react'
import { Link, NavLink, useLocation, useMatch } from 'react-router-dom'
import { usePacks } from '../mcp/queries'
import { ADMIN_SECTIONS } from '../routes/adminSections'
import {
  IconChevronLeft,
  IconChevronRight,
  IconClose,
  IconGear,
  IconHelp,
  IconPack
} from './icons'
import type { LeftRailMode } from './paneState'
import { SettingsNavigationTarget } from './SettingsNavigation'

export function LeftRail({
  mode,
  onToggle,
  asDrawer,
  drawerOpen,
  onDrawerOpenChange,
  openerRef
}: {
  mode: LeftRailMode
  onToggle: () => void
  asDrawer: boolean
  drawerOpen: boolean
  onDrawerOpenChange: (open: boolean) => void
  /**
   * The header button that opened the drawer. This drawer has no
   * `Dialog.Trigger` — the opener is two grid cells away — so Radix has no ref
   * to restore focus to and it is restored by hand.
   */
  openerRef?: RefObject<HTMLButtonElement | null>
}) {
  const settings = useMatch('/admin') !== null
  const body = (onNavigate?: () => void) => settings ? (
    <div className="desk-settings" onClick={(event) => {
      if ((event.target as HTMLElement).closest('a')) onNavigate?.()
    }}>
      <NavLink to="/packs" className="desk-nav-item">
        <IconChevronLeft /><span>Back to app</span>
      </NavLink>
      <SettingsNavigationTarget onNavigate={onNavigate} />
    </div>
  ) : <RailBody mode={asDrawer ? 'expanded' : mode} onToggle={onToggle} showCollapse={!asDrawer} onNavigate={onNavigate} />
  if (asDrawer) {
    return (
      <Dialog.Root open={drawerOpen} onOpenChange={onDrawerOpenChange}>
        <Dialog.Portal>
          <Dialog.Overlay className="desk-overlay" />
          <Dialog.Content
            className="desk-drawer desk-drawer-left"
            id="desk-rail"
            onCloseAutoFocus={(event) => {
              event.preventDefault()
              openerRef?.current?.focus()
            }}
          >
            <VisuallyHidden.Root>
              <Dialog.Title>Project navigation</Dialog.Title>
            </VisuallyHidden.Root>
            {/* A visible way out. Escape closed it and the overlay closed it,
                and neither is something a viewer can see — on the width whose
                likeliest device has no keyboard at all. */}
            <div className="desk-drawer-head">
              <Dialog.Close asChild>
                <button type="button" className="desk-icon-button" aria-label="Close navigation">
                  <IconClose />
                </button>
              </Dialog.Close>
            </div>
            {/* The landmark travels with the rail. Without this the drawer
                form offered no `navigation` at all, so the desk below 900px
                had one fewer landmark than the README's region table says it
                has — and the difference was the breakpoint, not the state. */}
            <nav aria-label="Project">
              {body(() => onDrawerOpenChange(false))}
            </nav>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    )
  }
  return (
    <nav className="desk-rail" id="desk-rail" aria-label="Project" data-mode={mode}>
      {body()}
    </nav>
  )
}

function RailBody({
  mode,
  onToggle,
  showCollapse,
  onNavigate
}: {
  mode: LeftRailMode
  onToggle: () => void
  showCollapse: boolean
  /**
   * Dismiss the thing this rail is inside, where it is inside one.
   *
   * In drawer form the rail is a **modal** dialog: the page beneath it is
   * `aria-hidden`, so a NavLink that navigated and left the drawer standing
   * put the destination behind an overlay the viewer had to dismiss to see
   * what they had just asked for. Undefined in column form, where there is
   * nothing to dismiss.
   */
  onNavigate?: () => void
}) {
  const icons = mode === 'icons'
  const toggleRef = useRef<HTMLButtonElement | null>(null)

  return (
    <>
      <PacksGroup icons={icons} onNavigate={onNavigate} />

      <div className="desk-spacer" />
      <Separator.Root className="desk-rule-h" decorative />

      <div className="desk-admin-row">
        <Labelled icons={icons} label="Admin">
          <NavLink className="desk-nav-item" to="/admin" aria-label="Admin" onClick={onNavigate}>
            <IconGear />
            {!icons && <span className="desk-nav-label">Admin</span>}
          </NavLink>
        </Labelled>
        <DropdownMenu.Root>
          <DropdownMenu.Trigger className="desk-icon-button" aria-label="Admin sections">
            <IconChevronRight />
          </DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content className="desk-menu" side="top" align="start" sideOffset={6}>
              {ADMIN_SECTIONS.map((section) => (
                <DropdownMenu.Item asChild key={section.id} className="desk-menu-item">
                  <NavLink to={`/admin#${section.id}`} onClick={onNavigate}>
                    {section.title}
                  </NavLink>
                </DropdownMenu.Item>
              ))}
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>
      </div>

      <Labelled icons={icons} label="Help & About">
        <NavLink className="desk-nav-item" to="/help" aria-label="Help & About" onClick={onNavigate}>
          <IconHelp />
          {!icons && <span className="desk-nav-label">Help &amp; About</span>}
        </NavLink>
      </Labelled>

      {showCollapse && (
        <Tooltip content="Expand navigation" disabled={!icons} side="right"><button
          type="button"
          ref={toggleRef}
          className="desk-nav-item"
          aria-expanded={!icons}
          aria-controls="desk-rail"
          onClick={() => {
            // Focus moves to the toggle *before* the width changes, so a rail
            // that collapses under the keyboard does not leave focus on an
            // element that is about to be 56px of icon.
            toggleRef.current?.focus()
            onToggle()
          }}
        >
          {icons ? <IconChevronRight /> : <IconChevronLeft />}
          {!icons && <span className="desk-nav-label">Collapse navigation</span>}
          {icons && <VisuallyHidden.Root>Expand navigation</VisuallyHidden.Root>}
        </button></Tooltip>
      )}
    </>
  )
}

/**
 * A tooltip in icon mode, nothing in expanded mode.
 *
 * The tooltip is never the accessible name — every control inside carries its
 * own `aria-label`. It is a hint for a sighted viewer looking at a glyph.
 */
function Labelled({
  icons,
  label,
  children
}: {
  icons: boolean
  label: string
  children: ReactElement
}) {
  if (!icons) return <>{children}</>
  return (
    <Tooltip content={label} side="right">{children}</Tooltip>
  )
}

/**
 * The Packs destination.
 *
 * One entry, and a count beside it — **and no count at all** where the listing
 * failed or has not answered. `0` would be a claim about the project, and the
 * one thing the desk knows in that state is that it does not know. That rule
 * came here with the list it used to draw, and the failure is still shown as
 * the failure rather than as an empty project.
 *
 * The count is **in the accessible name**, not only in the markup. Every rail
 * entry carries an `aria-label`, which replaces its contents for a screen
 * reader — so a count rendered as a child of one is a number only a sighted
 * reader gets. The name says it instead.
 */
function PacksGroup({ icons, onNavigate }: { icons: boolean; onNavigate?: () => void }) {
  const { data, error } = usePacks()
  const { pathname } = useLocation()
  const active = /^\/(packs(?:\/|$)|matrix$|graphs(?:\/|$))/.test(pathname)
  const count = error === null && data !== undefined ? (data.packs ?? []).length : undefined

  return (
    <>
      <Labelled icons={icons} label="Packs">
        <Link
          className="desk-nav-item"
          to="/packs"
          aria-current={active ? 'page' : undefined}
          aria-label={count === undefined ? 'Packs' : `Packs, ${count}`}
          onClick={onNavigate}
        >
          <IconPack />
          {!icons && <span className="desk-nav-label">Packs</span>}
          {!icons && count !== undefined && <span className="desk-nav-count">{count}</span>}
        </Link>
      </Labelled>
      {!icons && error && (
        <p className="desk-pane-empty">The pack listing did not answer — {error.message}</p>
      )}
    </>
  )
}
