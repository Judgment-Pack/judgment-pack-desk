/**
 * The rail: the primary action, what the project contains, and Admin.
 *
 * **The rail must never call `useConfiguredGraphs()`.** That hook falls back to
 * `useGraphMatrix(undefined, !graphInventorySupported)` — a whole-project
 * `experimental_test_graphs` walk — on any runtime that does not advertise
 * `experimental_list_graphs`. Today only `ProjectHome` pays that; hoisting it
 * into the shell would fire it on `/author`, on `/packs/:id/evaluate` and on
 * every other route, on every navigation. So the rail reads
 * `useGraphInventory()` — which is itself disabled unless the tool is
 * advertised — and otherwise renders the Graphs entry unconditionally and
 * quiet. There is a test, and a mutation row.
 *
 * **The shell derives no verdict.** No status colour, no rollup count, no
 * "N failing" pill. A red pill in a nav rail is a gate the runtime never
 * issued.
 *
 * The structure is a typed array, so adding an entry is visible in review.
 * There is no Recents, no Favourites and no Starred: each is per-viewer
 * retained state, and each arrives as its own two-line change if it ever
 * should.
 */
import { Collapsible, Dialog, DropdownMenu, Separator, Tooltip, VisuallyHidden } from 'radix-ui'
import { useRef, useState, type ReactNode, type RefObject } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { useGraphInventory, usePacks } from '../mcp/queries'
import { ADMIN_SECTIONS } from '../routes/adminSections'
import { CreatePackDialog } from './CreatePackDialog'
import { useAuthorDirty } from './authorBridge'
import {
  IconChevronDown,
  IconChevronLeft,
  IconChevronRight,
  IconClose,
  IconGear,
  IconGraph,
  IconHelp,
  IconMatrix,
  IconPack,
  IconPencil,
  IconPlus
} from './icons'
import type { LeftRailMode } from './paneState'

/** How many packs the group lists before it hands over to the project home. */
const PACK_CAP = 30

interface RailItem {
  to: string
  label: string
  icon: ReactNode
}

/** The middle group, in the order the rail draws it. */
const RAIL_ITEMS: readonly RailItem[] = [
  { to: '/matrix', label: 'Matrix and coverage', icon: <IconMatrix /> },
  { to: '/graphs', label: 'Graphs', icon: <IconGraph /> },
  { to: '/author', label: 'Author', icon: <IconPencil /> }
]

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
              <RailBody
                mode="expanded"
                onToggle={onToggle}
                showCollapse={false}
                onNavigate={() => onDrawerOpenChange(false)}
              />
            </nav>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    )
  }
  return (
    <nav className="desk-rail" id="desk-rail" aria-label="Project" data-mode={mode}>
      <RailBody mode={mode} onToggle={onToggle} showCollapse />
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
  const dirty = useAuthorDirty()
  // The cheap inventory, and only ever this one. It is disabled inside the
  // hook unless the runtime advertises `experimental_list_graphs`, so against
  // an older runtime it costs nothing and the entry is simply rendered quiet.
  // Its message is printed where it failed, verbatim, and never summarised.
  const graphs = useGraphInventory()
  const [creating, setCreating] = useState(false)
  const toggleRef = useRef<HTMLButtonElement | null>(null)

  return (
    <>
      <Labelled icons={icons} label="Create a pack">
        <button
          type="button"
          className="desk-create"
          aria-label="Create a pack"
          onClick={() => setCreating(true)}
        >
          <IconPlus />
          {!icons && <span className="desk-nav-label">Create pack</span>}
        </button>
      </Labelled>
      {/* Mounted only while it is open. Mounted unconditionally, its body ran
          on every route: `list_examples` was called on first paint everywhere
          and refetched on every `desk/fileChanged`, for a dialog nobody had
          opened. That is the same objection this file raises about the graph
          walk, one order of magnitude smaller, and it gets the same answer. */}
      {creating && <CreatePackDialog open onOpenChange={setCreating} />}

      <PacksGroup icons={icons} onNavigate={onNavigate} />

      <Separator.Root className="desk-rule-h" decorative />

      {RAIL_ITEMS.map((item) => (
        <Labelled key={item.to} icons={icons} label={item.label}>
          <NavLink
            className="desk-nav-item"
            to={item.to}
            aria-label={item.label}
            onClick={onNavigate}
            title={item.to === '/graphs' && graphs.error ? graphs.error.message : undefined}
          >
            {item.icon}
            {!icons && <span className="desk-nav-label">{item.label}</span>}
            {item.to === '/author' && dirty && (
              <span className="desk-dirty" aria-label="unsaved changes" role="img" />
            )}
          </NavLink>
        </Labelled>
      ))}

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
        <button
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
        </button>
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
  children: ReactNode
}) {
  if (!icons) return <>{children}</>
  return (
    <Tooltip.Root>
      <Tooltip.Trigger asChild>{children}</Tooltip.Trigger>
      <Tooltip.Portal>
        <Tooltip.Content className="desk-menu" side="right" sideOffset={6}>
          {label}
        </Tooltip.Content>
      </Tooltip.Portal>
    </Tooltip.Root>
  )
}

/**
 * The Packs group.
 *
 * A failed listing shows the failure rather than an empty list: "this project
 * declares no packs" and "the listing did not answer" are two different
 * statements, and only one of them is about the project.
 */
function PacksGroup({ icons, onNavigate }: { icons: boolean; onNavigate?: () => void }) {
  const { data, error } = usePacks()
  const location = useLocation()
  const [open, setOpen] = useState(true)
  const packs = data?.packs ?? []
  const shown = packs.slice(0, PACK_CAP)

  if (icons) {
    return (
      <Labelled icons label="Packs">
        <NavLink className="desk-nav-item" to="/" aria-label="Packs" onClick={onNavigate}>
          <IconPack />
        </NavLink>
      </Labelled>
    )
  }

  return (
    <Collapsible.Root open={open} onOpenChange={setOpen}>
      <Collapsible.Trigger className="desk-nav-item" aria-label="Packs">
        <IconPack />
        <span className="desk-nav-label">Packs</span>
        <IconChevronDown />
      </Collapsible.Trigger>
      <Collapsible.Content>
        {error ? (
          <p className="desk-pane-empty">The pack listing did not answer — {error.message}</p>
        ) : (
          <>
            {shown.map((pack) => {
              const base = `/packs/${encodeURIComponent(pack.id)}`
              const selected = location.pathname.startsWith(base)
              return (
                <div key={pack.id}>
                  <NavLink className="desk-nav-item" to={base} onClick={onNavigate}>
                    <span className="desk-nav-label">{pack.id}</span>
                  </NavLink>
                  {selected && (
                    <>
                      <NavLink
                        className="desk-nav-item desk-nav-child"
                        to={base}
                        end
                        onClick={onNavigate}
                      >
                        <span className="desk-nav-label">Document</span>
                      </NavLink>
                      <NavLink
                        className="desk-nav-item desk-nav-child"
                        to={`${base}/evaluate`}
                        onClick={onNavigate}
                      >
                        <span className="desk-nav-label">Evaluate</span>
                      </NavLink>
                      <NavLink
                        className="desk-nav-item desk-nav-child"
                        to={`${base}/matrix`}
                        onClick={onNavigate}
                      >
                        <span className="desk-nav-label">Matrix</span>
                      </NavLink>
                    </>
                  )}
                </div>
              )
            })}
            {packs.length > PACK_CAP && (
              <NavLink className="desk-nav-item desk-nav-child" to="/" onClick={onNavigate}>
                <span className="desk-nav-label">show all →</span>
              </NavLink>
            )}
          </>
        )}
      </Collapsible.Content>
    </Collapsible.Root>
  )
}
