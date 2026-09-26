import { PaneToggle } from './PaneToggle'
import { useChats } from '../chat/ChatProvider'
import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { Tooltip } from '../ui/Tooltip'
import { type ReactElement } from 'react'
/** Primary navigation stays about destinations. Packs and Judgment Graphs have separate entries; tests live
 * inside each pack or graph; project file editing is available from the project menu.
 * The shell never runs tests or fetches graph inventory to draw navigation. */
import { Dialog, DropdownMenu, Separator, VisuallyHidden } from 'radix-ui'
import { type RefObject } from 'react'
import { Link, NavLink, useLocation, useMatch } from 'react-router-dom'
import { usePacks } from '../mcp/queries'
import { ADMIN_SECTIONS } from '../routes/adminSections'
import {
  IconChevronLeft,
  IconChevronRight,
  IconGear,
  IconHelp,
  IconPack,
  IconGraph,
  IconHistory
} from './icons'
import type { LeftRailMode } from './paneState'
import { SettingsNavigationTarget } from './SettingsNavigation'

export function LeftRail({
  mode,
  asDrawer,
  drawerOpen,
  onDrawerOpenChange,
  openerRef
}: {
  mode: LeftRailMode
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
  useLocale()
  const settings = useMatch('/admin') !== null
  const body = (onNavigate?: () => void) => settings ? (
    <div className="desk-settings" onClick={(event) => {
      if ((event.target as HTMLElement).closest('a')) onNavigate?.()
    }}>
      <NavLink to="/packs" className="desk-nav-item">
        <IconChevronLeft /><span>{msg("Back to app")}</span>
      </NavLink>
      <SettingsNavigationTarget onNavigate={onNavigate} />
    </div>
  ) : <RailBody mode={asDrawer ? 'expanded' : mode} onNavigate={onNavigate} />
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
              <Dialog.Title>{msg("Project navigation")}</Dialog.Title>
            </VisuallyHidden.Root>
            {/* A visible way out. Escape closed it and the overlay closed it,
                and neither is something a viewer can see — on the width whose
                likeliest device has no keyboard at all. */}
            <div className="desk-drawer-head">
              <PaneToggle label={msg('Collapse navigation')} expanded onClick={() => onDrawerOpenChange(false)} controls="desk-rail" />
            </div>
            {/* The landmark travels with the rail. Without this the drawer
                form offered no `navigation` at all, so the desk below 900px
                had one fewer landmark than the README's region table says it
                has — and the difference was the breakpoint, not the state. */}
            <nav aria-label={msg("Project")}>
              {body(() => onDrawerOpenChange(false))}
            </nav>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    )
  }
  return (
    <nav className="desk-rail" hidden={settings && mode === 'icons'} id="desk-rail" aria-label={msg("Project")} data-mode={mode}>
      {body()}
    </nav>
  )
}

function RailBody({
  mode,
  onNavigate
}: {
  mode: LeftRailMode
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
  useLocale()
  const icons = mode === 'icons'

  return (
    <>
      <PacksGroup icons={icons} onNavigate={onNavigate} />
      <Labelled icons={icons} label={msg("Graphs")}>
        <NavLink className="desk-nav-item" to="/graphs" aria-label={msg("Graphs")} onClick={onNavigate}>
          <IconGraph />
          {!icons && <span className="desk-nav-label">{msg("Graphs")}</span>}
        </NavLink>
      </Labelled>
      <Labelled icons={icons} label={msg("Jobs")}>
        <NavLink className="desk-nav-item" to="/jobs" aria-label={msg("Jobs")} onClick={onNavigate}>
          <IconHistory />{!icons && <span className="desk-nav-label">{msg("Jobs")}</span>}
        </NavLink>
      </Labelled>

      <div className="desk-spacer" />
      <Separator.Root className="desk-rule-h" decorative />

      <div className="desk-admin-row">
        <Labelled icons={icons} label={msg("Admin")}>
          <NavLink className="desk-nav-item" to="/admin" aria-label={msg("Admin")} onClick={onNavigate}>
            <IconGear />
            {!icons && <span className="desk-nav-label">{msg("Admin")}</span>}
          </NavLink>
        </Labelled>
        <DropdownMenu.Root>
          <DropdownMenu.Trigger className="desk-icon-button" aria-label={msg("Admin sections")}>
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

      <Labelled icons={icons} label={msg("Help & About")}>
        <NavLink className="desk-nav-item" to="/help" aria-label={msg("Help & About")} onClick={onNavigate}>
          <IconHelp />
          {!icons && <span className="desk-nav-label">{msg("Help & About")}</span>}
        </NavLink>
      </Labelled>


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
  useLocale()
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
  useLocale()
  const { data, error } = usePacks()
  const { pathname } = useLocation()
  const active = /^\/packs(?:\/|$)/.test(pathname)
  const {packDrafts}=useChats()
  const count = error === null && data !== undefined ? (data.packs ?? []).length + packDrafts.filter(item=>!item.finalized).length : undefined

  return (
    <>
      <Labelled icons={icons} label={msg("Packs")}>
        <Link
          className="desk-nav-item"
          to="/packs"
          aria-current={active ? 'page' : undefined}
          aria-label={count === undefined ? msg("Packs") : msg("Packs, {{value0}}", { value0: count })}
          onClick={onNavigate}
        >
          <IconPack />
          {!icons && <span className="desk-nav-label">{msg("Packs")}</span>}
          {!icons && count !== undefined && <span className="desk-nav-count">{count}</span>}
        </Link>
      </Labelled>
      {!icons && error && (
        <p className="desk-pane-empty"><Message text={"The pack listing did not answer — <0/>"} slots={[error.message]} /></p>
      )}
    </>
  )
}
