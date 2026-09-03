/**
 * The header: whose desk this is, what it points at, whether it is connected,
 * and who is looking.
 *
 * A direct child of the grid, so it is the `banner` landmark. (Safe: every
 * route's own `<header className="detail-head">` is nested inside `<main>` and
 * is therefore not a second banner.)
 *
 * **The centre is empty and reserved.** Nothing is painted there — no search,
 * no palette, no ⌘K. That is a decision recorded by an empty element and a
 * comment, not an omission.
 *
 * The organization name is **local configuration and never a token claim**,
 * even where an issuer offers a tenant or org claim: an issuer's label for a
 * customer is not the customer's brand, and reading it would let one issuer
 * set the open desk's chrome. Absent, it falls back to `judgment‑pack desk` —
 * never to an invented company.
 */
import { Avatar, DropdownMenu, Separator, Toggle } from 'radix-ui'
import { type RefObject } from 'react'
import { Link } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { DESK_FALLBACK_NAME } from '../config/deskConfig'
import { UserControl, monogram } from '../identity/UserControl'
import { useMcp } from '../mcp/McpProvider'
import { usePacks } from '../mcp/queries'
import { IconChevronDown, IconPanelBottom, IconPanelLeft, IconPanelRight } from './icons'

/**
 * The mark as a `data:` URI.
 *
 * An inline SVG string is encoded rather than injected: the page holds the
 * session token, and `dangerouslySetInnerHTML` on a value out of a project
 * file is how that token leaves. A `data:` URI in an `<img>` cannot script.
 */
export function markToDataUri(mark: string | null): string | undefined {
  if (mark === null) return undefined
  const trimmed = mark.trim()
  if (trimmed.startsWith('data:image/')) return trimmed
  if (trimmed.startsWith('<svg')) {
    return `data:image/svg+xml,${encodeURIComponent(trimmed)}`
  }
  return undefined
}

export function HeaderBar({
  inspectorOpen,
  inspectorIsDrawer,
  consoleOpen,
  onToggleInspector,
  onToggleConsole,
  inspectorOpenerRef,
  railIsDrawer,
  railDrawerOpen,
  onOpenRail,
  railOpenerRef
}: {
  inspectorOpen: boolean
  /** True below 1100px, where the Inspector is a drawer rather than a column. */
  inspectorIsDrawer: boolean
  consoleOpen: boolean
  onToggleInspector: () => void
  onToggleConsole: () => void
  /** Held by the frame, so a closed drawer can hand focus back to it. */
  inspectorOpenerRef?: RefObject<HTMLButtonElement | null>
  /** True below 900px, where the rail is an overlay rather than a column. */
  railIsDrawer: boolean
  railDrawerOpen: boolean
  onOpenRail: () => void
  railOpenerRef?: RefObject<HTMLButtonElement | null>
}) {
  const { config } = useEffectiveConfig()
  const name = config.organization.name ?? DESK_FALLBACK_NAME
  const mark = markToDataUri(config.organization.mark)

  return (
    <header className="desk-head">
      <div className="desk-head-left">
        {/* The drawer's only pointer affordance, and it has to live outside
            the drawer: in overlay form the rail renders no collapse toggle,
            so without this the entire left menu was reachable by Mod+B alone
            — on a width whose likeliest device has no keyboard at all. */}
        {railIsDrawer && (
          <button
            type="button"
            ref={railOpenerRef}
            className="desk-icon-button"
            aria-label="Project navigation"
            aria-expanded={railDrawerOpen}
            /* Only while the drawer is actually in the document. A closed
               `Dialog` unmounts its portal, so an unconditional IDREF here
               named an element that does not exist — which offers assistive
               technology a broken relationship rather than none. `aria-expanded`
               carries the state either way. */
            aria-controls={railDrawerOpen ? 'desk-rail' : undefined}
            onClick={onOpenRail}
          >
            <IconPanelLeft />
          </button>
        )}
        <Avatar.Root className="desk-orgmark">
          {mark && <Avatar.Image src={mark} alt="" />}
          <Avatar.Fallback delayMs={0}>{monogram(name)}</Avatar.Fallback>
        </Avatar.Root>
        {/* A router `Link`, not an `<a href>`. A full document load here would
            restart the SPA, refetch every query and drop `/ws` — and the
            chassis kills the runtime subprocess when the socket that started
            it closes, so clicking the brand would have respawned `jpack mcp`.

            The mark is beside the link rather than inside it, departing from
            one clause of the spec: the link's accessible name and text are the
            organization name exactly — the fallback's non-breaking hyphen is
            asserted character for character — and a monogram inside the anchor
            puts two initials in front of both. */}
        <Link className="desk-brand" to="/">
          {name}
        </Link>
        <Separator.Root className="desk-rule" decorative orientation="vertical" />
        <ProjectChip />
      </div>

      {/* The centre zone. Reserved, and painted with nothing: no palette and
          no search lands here in this line of work. */}
      <div className="desk-head-centre" />

      <div className="desk-head-right">
        <ConnectionBadge />
        <Separator.Root className="desk-rule" decorative orientation="vertical" />
        <Toggle.Root
          ref={inspectorOpenerRef}
          className="desk-icon-button"
          aria-label="Inspector"
          /* In column form the panel is always in the document — `hidden`, not
             absent — so the reference resolves whether it is open or shut.
             In drawer form it exists only while it is open. */
          aria-controls={!inspectorIsDrawer || inspectorOpen ? 'desk-inspector' : undefined}
          pressed={inspectorOpen}
          onPressedChange={onToggleInspector}
        >
          <IconPanelRight />
        </Toggle.Root>
        <Toggle.Root
          className="desk-icon-button"
          aria-label="Console"
          aria-controls="desk-console"
          pressed={consoleOpen}
          onPressedChange={onToggleConsole}
        >
          <IconPanelBottom />
        </Toggle.Root>
        <Separator.Root className="desk-rule" decorative orientation="vertical" />
        <UserControl />
      </div>
    </header>
  )
}

/**
 * The project, as a label.
 *
 * **Not a switcher**, and the menu says so in words: the chassis pins one
 * `os.Root` at startup, so there is no second project for this desk to move
 * to. The words *workspace* and *tenant* appear nowhere — both imply a
 * server-side bounded space this desk does not have.
 */
function ProjectChip() {
  const { data } = usePacks()
  const configPath = data?.configPath
  const label = configPath ? basename(configPath) : 'this project'
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger className="desk-chip">
        <span className="desk-chip-name">{label}</span>
        <IconChevronDown />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className="desk-menu" align="start" sideOffset={6}>
          <DropdownMenu.Label className="desk-menu-note">
            This is a label, not a switcher. The chassis opens one project directory at startup
            and holds it for the life of the process; to work on another, start a desk there.
          </DropdownMenu.Label>
          {configPath && (
            <DropdownMenu.Label className="desk-menu-note">
              <code>{configPath}</code>
            </DropdownMenu.Label>
          )}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  )
}

function basename(path: string): string {
  const parts = path.split(/[/\\]/).filter(Boolean)
  return parts[parts.length - 1] ?? path
}

/**
 * Moved out of `App.tsx`, not rewritten: the same `badge badge-${status}`
 * classes, the same `title`, the same four-way label ladder.
 */
function ConnectionBadge() {
  const { status } = useMcp()
  const label =
    status === 'ready'
      ? 'connected'
      : status === 'connecting'
        ? 'connecting'
        : status === 'reconnecting'
          ? 'reconnecting'
          : 'offline'
  return (
    <span className={`badge badge-${status}`} title={`MCP connection: ${label}`}>
      {label}
    </span>
  )
}
