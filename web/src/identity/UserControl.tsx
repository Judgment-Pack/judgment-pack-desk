import { useVerifiedSession } from '../auth/VerifiedSession'
import { msg, useLocale } from '../i18n'
import { LanguageMenu } from '../i18n/LanguageMenu'
/** Account menu: verified session identity takes precedence over legacy display
 * configuration. Ending the Desk session leaves source integrations connected.
 * Language, appearance and pane preferences remain browser-local. */
import { Avatar, DropdownMenu } from 'radix-ui'
import { SignOutDialog } from '../auth/SignOutDialog'
import { useRef, useState } from 'react'
import { useConnectionsPane } from '../connections/ConnectionPaneContext'
import { Link } from 'react-router-dom'
import {
  DENSITIES,
  THEME_CHOICES,
  type AppearanceConfig,
  type Density,
  type ThemeChoice
} from '../config/deskConfig'
import { useAppearance } from '../shell/appearanceState'
import { IconCheck, IconChevronDown } from '../shell/icons'
import { useShellState, type ResetOutcome } from '../shell/paneState'
import { useIdentity } from './IdentityProvider'

export const NONE_MENU_SENTENCE =
  'No identity provider is configured. This desk is authorized by the session this tab holds, ' +
  'the loopback bind, and the origin check.'

export const SESSION_SENTENCE = 'The installation owner needs to configure sign-in once on this computer.'

export const PROVIDER_PHASE_NOTE = 'Sign-in is not configured.'

/**
 * What a reset did, in four sentences rather than one.
 *
 * They are four different facts and a menu that reported all of them as
 * "Cleared." would be stating one it never observed: the record may be gone,
 * this browser's storage may have refused the deletion, the chassis may not yet
 * have said which project this desk is open on — in which case the key is
 * provisional and nothing is cleared under it — or what is stored under that
 * key may be something this shell never wrote, which it leaves alone.
 */
export const RESET_SAYS: Record<ResetOutcome, string> = {
  get cleared() { return msg("Cleared — the panes are back on their defaults.") },
  get refused() { return msg("this browser did not clear the record — the layout is unchanged") },
  get unresolved() { return msg("nothing was cleared: this desk has not been told which project it is open on") },
  get foreign() { return msg("nothing was cleared: what is stored there is not a record this shell wrote") }
}

/**
 * What choosing a theme actually does, said where the choice is offered.
 *
 * It used to say that dark set an attribute and no colour, which was true and
 * is not any more: both palettes are authored now. What still needs saying is
 * what `system` means, because it is the one choice whose answer this desk does
 * not hold — it follows the computer's own setting and changes with it, which
 * a reader picking between three words has no other way to know.
 */
export const THEME_SAYS =
  'Light and dark each paint their own palette. Under system the desk follows this ' +
  'computer’s own setting and changes with it.'

/**
 * The other half of the same group, and it used to say the member was read by
 * nothing. It is read now, so what it says is what compact actually does.
 */
export const DENSITY_SAYS =
  'Compact tightens the rows, the controls, the cells and the type on the dense surfaces.'

/**
 * What this menu says before the project's file has been read.
 *
 * Not the schema's values under the project's name. `appearance` is filled in
 * from the built-in defaults until the file answers, so naming them here would
 * be this menu attributing a value to a file it has not seen — and a reader who
 * pressed "Use the project's default" on the strength of it would get something
 * else.
 */
export const PROJECT_DEFAULT_UNKNOWN = 'The project’s default has not been read yet.'

/**
 * What a viewer who clears their preference gets back.
 *
 * The project file's value, or the schema's where the file says nothing — the
 * decoder has already made those one value. It is named rather than implied
 * because "use the default" is otherwise a control whose result the reader can
 * only discover by pressing it, and it is named only once it is known.
 */
export function projectDefaultSays(appearance: AppearanceConfig | undefined): string {
  if (appearance === undefined) return msg(PROJECT_DEFAULT_UNKNOWN)
  return msg("Project default: {{value0}}, {{value1}}", { value0: appearanceLabel(appearance.theme), value1: appearanceLabel(appearance.density) })
}

/**
 * What clearing the preference did, on the reset's own four terms.
 *
 * The same four facts about the same class of record, and the same reason for
 * saying which one happened: the record may be gone, this browser's storage may
 * have refused the deletion, the chassis may not yet have said which project
 * this is, or what is under that key may be something this desk never wrote.
 */
export const RESTORED_SAYS: Record<ResetOutcome, string> = {
  get cleared() { return msg("Cleared — this project’s default is in force again.") },
  get refused() { return msg("this browser did not clear the record — your choice is unchanged") },
  get unresolved() { return msg("nothing was cleared: this desk has not been told which project it is open on") },
  get foreign() { return msg("nothing was cleared: what is stored there is not a record this desk wrote") }
}

/** Up to two initials, from whatever the name happens to be. */
export function monogram(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '·'
  const letters = parts.slice(0, 2).map((part) => part[0]!.toUpperCase())
  return letters.join('')
}

export function UserControl() {
  useLocale()
  const connections = useConnectionsPane()
  const [signOutOpen, setSignOutOpen] = useState(false)
  const opener = useRef<HTMLButtonElement>(null)
  const { provider, displayName, authenticated } = useIdentity()
  const localAccess = useVerifiedSession()?.localAccess === true
  // A verified account supplies the display name; legacy config is setup-only.
  const name = authenticated || provider === null ? displayName : (provider.label ?? provider.issuerHost)

  return (
    <>
    <DropdownMenu.Root>
      <DropdownMenu.Trigger ref={opener} className="desk-user" aria-label={msg("Account and desk settings")}>
        <Avatar.Root className="desk-avatar">
          <Avatar.Fallback delayMs={0}>{monogram(name)}</Avatar.Fallback>
        </Avatar.Root>
        <span className="desk-user-name">{name}</span>
        {provider !== null && (
          <span className="desk-tag">{provider.issuerHost}</span>
        )}
        <IconChevronDown />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className="desk-menu desk-header-menu" align="end" sideOffset={6} collisionPadding={16}>
          <DropdownMenu.Label className="desk-menu-note">
            {localAccess ? msg('Personal · This computer') : authenticated ? msg('Signed in with {{provider}}', { provider: provider?.label ?? provider?.issuerHost ?? '' }) : provider === null ? msg(NONE_MENU_SENTENCE) : msg(PROVIDER_PHASE_NOTE)}
          </DropdownMenu.Label>
          <DropdownMenu.Label className="desk-menu-note">{authenticated || localAccess ? msg('Signing out ends this Desk session. Your connected sources stay connected.') : msg(SESSION_SENTENCE)}</DropdownMenu.Label>
          <DropdownMenu.Separator className="desk-rule-h" />
          <DropdownMenu.Item className="desk-menu-item" onSelect={() => requestAnimationFrame(() => connections.open({ opener: opener.current }))}>{msg("My connections")}</DropdownMenu.Item>
          <LanguageMenu />
          <AppearanceItems />
          <DropdownMenu.Separator className="desk-rule-h" />
          <ResetPanesItem />
          <DropdownMenu.Item asChild className="desk-menu-item">
            <Link to="/help#shortcuts">{msg("Keyboard shortcuts")}</Link>
          </DropdownMenu.Item>
          <DropdownMenu.Item asChild className="desk-menu-item">
            <Link to="/admin">{msg("Admin")}</Link>
          </DropdownMenu.Item>
          <DropdownMenu.Item asChild className="desk-menu-item">
            <Link to="/help">{msg("About")}</Link>
          </DropdownMenu.Item>
          <DropdownMenu.Separator className="desk-rule-h" />
          <DropdownMenu.Item className="desk-menu-item" onSelect={() => requestAnimationFrame(() => setSignOutOpen(true))}>{msg('End session')}</DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
    <SignOutDialog open={signOutOpen} onOpenChange={setSignOutOpen} openerRef={opener} />
    </>
  )
}

/**
 * Forget this project's layout on this machine, and say what happened.
 *
 * Its own component so that the state it needs — the last outcome — belongs to
 * the action rather than to the control around it, and so that removing the
 * action is removing one element.
 *
 * **The menu stays open while it answers.** A `DropdownMenu.Item` closes the
 * menu on select, and the answer is a sentence: a menu that closed would take
 * it with it.
 *
 * **And the verdict goes when the menu does**, because a verdict from the last
 * time the menu was open is not a verdict about this one. That is the portal
 * unmounting this component with the content it is inside, which is why the
 * outcome is held here and not by the control around it — a verdict kept one
 * level up would greet whoever opened the menu next.
 */
function ResetPanesItem() {
  useLocale()
  const shell = useShellState()
  const [reset, setReset] = useState<ResetOutcome | undefined>(undefined)
  return (
    <>
      <DropdownMenu.Item
        className="desk-menu-item"
        onSelect={(event) => {
          // The reset runs inside the provider that owns the record — it
          // cancels a write already on its way, refuses to clear the
          // provisional key before the chassis has said which project this is,
          // and reads the key back afterwards.
          event.preventDefault()
          setReset(shell.resetPanes())
        }}
      >{msg("Reset panes")}</DropdownMenu.Item>
      {reset !== undefined && (
        <DropdownMenu.Label className="desk-menu-note">{RESET_SAYS[reset]}</DropdownMenu.Label>
      )}
    </>
  )
}

/**
 * This viewer's theme and density, applied as they are picked.
 *
 * **Radio groups, because these are choices and not commands**, and the
 * primitive carries the semantics: `menuitemradio`, `aria-checked`, arrow keys
 * and type-ahead all come from Radix rather than from anything written here.
 * The value each group shows is the **effective** one — the preference where
 * there is one, the project's default where there is not — so a viewer who has
 * chosen nothing still sees what is actually in force.
 *
 * **And nothing is shown as chosen before this desk knows what is.** The record
 * needs a root the chassis has not reported yet and the default needs a file
 * that has not been read, so a menu opened in that moment would tick the
 * schema's values as though somebody had settled on them. `undefined` on the
 * group is no item checked, which is the honest picture of a desk still
 * reading.
 *
 * **The menu stays open on a pick.** A `RadioItem` closes it on select, and
 * theme and density are two choices: a menu that closed after the first would
 * make the second a second trip. Radix applies the value before it consults
 * `defaultPrevented`, so preventing the default keeps the menu open and still
 * changes the value — a dependency `radixGround.test.tsx` holds.
 *
 * **And the verdict goes when the menu does**, for `ResetPanesItem`'s reason: a
 * verdict from the last time the menu was open is not a verdict about this one,
 * so it is held here, inside the portal that unmounts with the content.
 */
function AppearanceItems() {
  useLocale()
  const appearance = useAppearance()
  const [restored, setRestored] = useState<ResetOutcome | undefined>(undefined)
  return (
    <>
      <DropdownMenu.Label className="desk-menu-heading">{msg("Theme")}</DropdownMenu.Label>
      <DropdownMenu.RadioGroup
        aria-label={msg("Theme")}
        value={appearance.theme}
        onValueChange={(value) => appearance.setTheme(value as ThemeChoice)}
      >
        {THEME_CHOICES.map((choice) => (
          <AppearanceChoice key={choice} value={choice} />
        ))}
      </DropdownMenu.RadioGroup>
      <DropdownMenu.Label className="desk-menu-note">{msg(THEME_SAYS)}</DropdownMenu.Label>
      <DropdownMenu.Label className="desk-menu-heading">{msg("Density")}</DropdownMenu.Label>
      <DropdownMenu.RadioGroup
        aria-label={msg("Density")}
        value={appearance.density}
        onValueChange={(value) => appearance.setDensity(value as Density)}
      >
        {DENSITIES.map((choice) => (
          <AppearanceChoice key={choice} value={choice} />
        ))}
      </DropdownMenu.RadioGroup>
      <DropdownMenu.Label className="desk-menu-note">{msg(DENSITY_SAYS)}</DropdownMenu.Label>
      <DropdownMenu.Label className="desk-menu-note">
        {projectDefaultSays(appearance.projectDefault)}
      </DropdownMenu.Label>
      <DropdownMenu.Item
        className="desk-menu-item"
        onSelect={(event) => {
          // Inside the provider that owns the record: it refuses to clear the
          // provisional key before the chassis has said which project this is,
          // leaves a value this desk did not write alone, and reads the key
          // back afterwards rather than reporting on having asked.
          event.preventDefault()
          setRestored(appearance.restoreProjectDefault())
        }}
      >{msg("Use the project’s default")}</DropdownMenu.Item>
      {restored !== undefined && (
        <DropdownMenu.Label className="desk-menu-note">{RESTORED_SAYS[restored]}</DropdownMenu.Label>
      )}
    </>
  )
}

/** Labels are localized; persisted appearance values remain canonical. */
function appearanceLabel(value: string): string {
  switch (value) {
    case 'light': return msg('light')
    case 'dark': return msg('dark')
    case 'system': return msg('system')
    case 'compact': return msg('compact')
    case 'comfortable': return msg('comfortable')
    default: return value
  }
}

function AppearanceChoice({ value }: { value: string }) {
  useLocale()
  return (
    <DropdownMenu.RadioItem
      className="desk-menu-item desk-menu-choice"
      value={value}
      onSelect={(event) => event.preventDefault()}
    >
      <DropdownMenu.ItemIndicator className="desk-menu-tick">
        <IconCheck />
      </DropdownMenu.ItemIndicator>
      {appearanceLabel(value)}
    </DropdownMenu.RadioItem>
  )
}
