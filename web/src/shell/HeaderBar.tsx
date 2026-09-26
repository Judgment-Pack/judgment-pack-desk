import { msg, useLocale } from '../i18n'
import { SHORTCUTS } from './shortcuts'
import { QuickSwitcher } from './QuickSwitcher'
/**
 * The header: whose desk this is, what it points at, and who is looking.
 * The status strip owns the persistent connection indicator.
 *
 * A direct child of the grid, so it is the `banner` landmark. (Safe: every
 * route's own `<header className="detail-head">` is nested inside `<main>` and
 * is therefore not a second banner.)
 *
 * The centre stays quiet; the compact quick switcher lives beside the user menu.
 *
 * The organization name is **local configuration and never a token claim**,
 * even where an issuer offers a tenant or org claim: an issuer's label for a
 * customer is not the customer's brand, and reading it would let one issuer
 * set the open desk's chrome. Absent, it falls back to `judgment‑pack desk` —
 * never to an invented company.
 */
import { Avatar, DropdownMenu, Separator } from 'radix-ui'
import { type RefObject } from 'react'
import { Link } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { DESK_FALLBACK_NAME } from '../config/deskConfig'
import { UserControl, monogram } from '../identity/UserControl'
import { usePacks } from '../mcp/queries'
import { useAuthorDirty } from './authorBridge'
import { IconChevronDown } from './icons'
import { PaneToggle } from './PaneToggle'

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

export function HeaderBar({ railIsDrawer, railOpen, onToggleRail, railOpenerRef }: {
  railIsDrawer: boolean
  railOpen: boolean
  onToggleRail: () => void
  railOpenerRef?: RefObject<HTMLButtonElement | null>
}) {
  useLocale()
  const { config } = useEffectiveConfig()
  const name = config.organization.name ?? DESK_FALLBACK_NAME
  const mark = markToDataUri(config.organization.mark)

  return (
    <header className="desk-head">
      <div className="desk-head-left">
        <PaneToggle label={railOpen ? msg('Collapse navigation') : msg('Expand navigation')}
          expanded={railOpen} controls={!railIsDrawer || railOpen ? 'desk-rail' : undefined}
          onClick={onToggleRail} buttonRef={railOpenerRef} shortcut={SHORTCUTS[0]?.keys} />
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

      {/* Flexible space keeps the project and user controls at their edges. */}
      <div className="desk-head-centre" />

      <div className="desk-head-right">
        <QuickSwitcher />
        <UserControl />
      </div>
    </header>
  )
}

/** Project context and advanced file access. Opening another project still
 * requires starting Desk in that directory; this menu does not switch roots. */
function ProjectChip() {
  useLocale()
  const dirty = useAuthorDirty()
  const { data } = usePacks()
  const configPath = data?.configPath
  const label = configPath ? basename(configPath) : msg("this project")
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger className="desk-chip">
        <span className="desk-chip-name">{label}</span>
        {dirty && <span className="desk-dirty" aria-label={msg("unsaved changes")} role="img" />}
        <IconChevronDown />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className="desk-menu desk-header-menu" align="start" sideOffset={6} collisionPadding={16}>
          <DropdownMenu.Item asChild className="desk-menu-item">
            <Link to="/author">{msg("Project files")}</Link>
          </DropdownMenu.Item>
          <DropdownMenu.Label className="desk-menu-note">{msg("To open another project, start Desk in that project’s folder.")}</DropdownMenu.Label>
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
