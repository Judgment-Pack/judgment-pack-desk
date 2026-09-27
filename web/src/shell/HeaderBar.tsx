import { msg, useLocale } from '../i18n'
import { SHORTCUTS } from './shortcuts'
import { DeskSwitcher } from '../desks/DeskSwitcher'
import { useEffect } from 'react'
import { QuickSwitcher } from './QuickSwitcher'
import { type RefObject } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { UserControl } from '../identity/UserControl'
import { PaneToggle } from './PaneToggle'
import { DEFAULT_DESK_LOGO, markToDataUri } from '../ui/BrandMark'
export { markToDataUri } from '../ui/BrandMark'

/** The desk name is navigation; organization branding supplies only its mark. */
export function HeaderBar({ railIsDrawer, railOpen, onToggleRail, railOpenerRef }: {
  railIsDrawer: boolean
  railOpen: boolean
  onToggleRail: () => void
  railOpenerRef?: RefObject<HTMLButtonElement | null>
}) {
  useLocale()
  const { config } = useEffectiveConfig()
  const favicon = markToDataUri(config.organization.favicon ?? null) ?? markToDataUri(config.organization.mark) ?? DEFAULT_DESK_LOGO
  useEffect(() => {
    const link = document.querySelector<HTMLLinkElement>('#desk-favicon')
    if (link) {
      // An uploaded PNG or ICO must not keep the boot page's SVG MIME hint.
      link.type = /^data:([^;,]+)/.exec(favicon)?.[1] ?? 'image/svg+xml'
      link.href = favicon
    }
  }, [favicon])

  return (
    <header className="desk-head">
      <div className="desk-head-left">
        <PaneToggle label={railOpen ? msg('Collapse navigation') : msg('Expand navigation')}
          expanded={railOpen} controls={!railIsDrawer || railOpen ? 'desk-rail' : undefined}
          onClick={onToggleRail} buttonRef={railOpenerRef} shortcut={SHORTCUTS[0]?.keys} />
        <DeskSwitcher />
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
