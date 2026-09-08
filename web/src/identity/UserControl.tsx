/**
 * The header's user control.
 *
 * **NONE — the only fully-live state in phase A.** A monogram, the local
 * display name, a `local` tag, and a menu whose first line is non-interactive
 * and says what actually authorizes this desk. There is **no Sign out and no
 * disabled Sign out**: there is no session to end, and a greyed control that
 * will never enable is an affordance that lies. There is no Sign in either —
 * the route to a provider is Admin.
 *
 * **Provider configured — honest and inert.** Where a provider object is
 * present the header names the issuer host and says sign-in arrives in phase
 * B. Nothing else changes anywhere: no route, no pane, no endpoint. The
 * sign-in flow itself — discovery, JWKS, PKCE — is a separate piece of work,
 * and it is the one that falsifies the README's "opens no outbound
 * connection", which it must amend in the same commit.
 *
 * **The panes' reset lives here**, and this is the menu it belongs in. The
 * record it clears is per viewer and per browser — the same class of thing as
 * the two settings links above it — and it is about all three panes, so it is
 * not one pane's header control: the Inspector's header carries the Inspector's
 * own close and nothing else, and the Console has no header at all. It used to
 * be a button on Admin › Panes, which is a settings page reaching into a
 * browser's own storage; this is the shell's own menu, beside the panes it
 * clears. The menu **stays open** while it answers, because what happened is a
 * sentence and a menu that closed would take it away with it.
 *
 * **Appearance is set here, and it is a preference rather than a setting.**
 * Theme and density are a person's, not an organization's — Admin's card wrote
 * them into a file in the project's repository, so one person's dark was
 * everyone's — so they are two radio groups in this menu, stored in this
 * browser, applied the moment they are picked. There is no Save: a preference
 * is not a file. The project file's `appearance` is what a viewer who has
 * chosen nothing gets, and the menu names it so that clearing is not a leap in
 * the dark.
 *
 * The sentence about the token is checked against the code rather than
 * inherited from the spec: `McpProvider` copies `?token=` into `sessionStorage`
 * on first load, and nothing calls `history.replaceState` — so the token
 * leaves the address bar at the first in-app navigation, not at load. Saying
 * "it leaves the URL immediately" would be a claim this desk does not keep.
 */
import { Avatar, DropdownMenu } from 'radix-ui'
import { useState } from 'react'
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
  'No identity provider is configured. This desk is authorized by the session token this ' +
  'browser tab holds, the loopback bind, and the origin check.'

export const TOKEN_SENTENCE =
  'The token arrives in the URL the chassis prints; the page copies it into sessionStorage ' +
  'under jpack-desk-token on first load, and it leaves the address bar at the first in-app ' +
  'navigation.'

export const PROVIDER_PHASE_NOTE = 'provider configured · sign-in arrives in phase B'

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
  cleared: 'Cleared — the panes are back on their defaults.',
  refused: 'this browser did not clear the record — the layout is unchanged',
  unresolved: 'nothing was cleared: this desk has not been told which project it is open on',
  foreign: 'nothing was cleared: what is stored there is not a record this shell wrote'
}

/**
 * What choosing a theme actually does, said where the choice is offered.
 *
 * Admin's Appearance card carried this sentence and Admin's card is gone; a
 * control that moved somewhere its own caveat did not follow is a control that
 * has quietly started overstating itself. Both palettes carry the light values
 * — the three condition verdict colours cannot be mechanically inverted — so
 * dark changes an attribute and no colour, and this is the third place that is
 * written down rather than inferred.
 */
export const THEME_SAYS =
  'The palette it selects is the light one; dark sets an attribute and no colour.'

/** The other half of the same card, for the member nothing reads yet. */
export const DENSITY_SAYS = 'Recorded, and read by nothing yet.'

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
  if (appearance === undefined) return PROJECT_DEFAULT_UNKNOWN
  return `Project default: ${appearance.theme}, ${appearance.density}`
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
  cleared: 'Cleared — this project’s default is in force again.',
  refused: 'this browser did not clear the record — your choice is unchanged',
  unresolved: 'nothing was cleared: this desk has not been told which project it is open on',
  foreign: 'nothing was cleared: what is stored there is not a record this desk wrote'
}

/** Up to two initials, from whatever the name happens to be. */
export function monogram(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '·'
  const letters = parts.slice(0, 2).map((part) => part[0]!.toUpperCase())
  return letters.join('')
}

export function UserControl() {
  const { provider, displayName } = useIdentity()
  // Where a provider is configured and carries no label, the name falls back
  // to the issuer's host — something the desk read out of the file. It does
  // **not** fall back to "signed out": that is a verdict about a session, and
  // phase A performs no discovery, holds no token and computes no expiry, so
  // it is a state this desk has not established and must not assert.
  //
  // Branched on nullness, not on a tag. There is no `mode` to read here
  // because there is no `mode` in the state, which is the same absence the
  // configuration schema keeps one layer down.
  const name = provider === null ? displayName : (provider.label ?? provider.issuerHost)

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger className="desk-user" aria-label="Account and desk settings">
        <Avatar.Root className="desk-avatar">
          <Avatar.Fallback delayMs={0}>{monogram(name)}</Avatar.Fallback>
        </Avatar.Root>
        <span className="desk-user-name">{name}</span>
        {provider === null ? (
          <span className="desk-tag">local</span>
        ) : (
          <span className="desk-tag">{provider.issuerHost}</span>
        )}
        <IconChevronDown />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content className="desk-menu" align="end" sideOffset={6}>
          <DropdownMenu.Label className="desk-menu-note">
            {provider === null ? NONE_MENU_SENTENCE : PROVIDER_PHASE_NOTE}
          </DropdownMenu.Label>
          <DropdownMenu.Label className="desk-menu-note">{TOKEN_SENTENCE}</DropdownMenu.Label>
          <DropdownMenu.Separator className="desk-rule-h" />
          <AppearanceItems />
          <DropdownMenu.Separator className="desk-rule-h" />
          <ResetPanesItem />
          <DropdownMenu.Item asChild className="desk-menu-item">
            <Link to="/help#shortcuts">Keyboard shortcuts</Link>
          </DropdownMenu.Item>
          <DropdownMenu.Item asChild className="desk-menu-item">
            <Link to="/admin">Admin</Link>
          </DropdownMenu.Item>
          <DropdownMenu.Item asChild className="desk-menu-item">
            <Link to="/help">About</Link>
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
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
      >
        Reset panes
      </DropdownMenu.Item>
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
  const appearance = useAppearance()
  const [restored, setRestored] = useState<ResetOutcome | undefined>(undefined)
  return (
    <>
      <DropdownMenu.Label className="desk-menu-heading">Theme</DropdownMenu.Label>
      <DropdownMenu.RadioGroup
        aria-label="Theme"
        value={appearance.theme}
        onValueChange={(value) => appearance.setTheme(value as ThemeChoice)}
      >
        {THEME_CHOICES.map((choice) => (
          <AppearanceChoice key={choice} value={choice} />
        ))}
      </DropdownMenu.RadioGroup>
      <DropdownMenu.Label className="desk-menu-note">{THEME_SAYS}</DropdownMenu.Label>
      <DropdownMenu.Label className="desk-menu-heading">Density</DropdownMenu.Label>
      <DropdownMenu.RadioGroup
        aria-label="Density"
        value={appearance.density}
        onValueChange={(value) => appearance.setDensity(value as Density)}
      >
        {DENSITIES.map((choice) => (
          <AppearanceChoice key={choice} value={choice} />
        ))}
      </DropdownMenu.RadioGroup>
      <DropdownMenu.Label className="desk-menu-note">{DENSITY_SAYS}</DropdownMenu.Label>
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
      >
        Use the project’s default
      </DropdownMenu.Item>
      {restored !== undefined && (
        <DropdownMenu.Label className="desk-menu-note">{RESTORED_SAYS[restored]}</DropdownMenu.Label>
      )}
    </>
  )
}

/** One choice, spelled as the file spells it. */
function AppearanceChoice({ value }: { value: string }) {
  return (
    <DropdownMenu.RadioItem
      className="desk-menu-item desk-menu-choice"
      value={value}
      onSelect={(event) => event.preventDefault()}
    >
      <DropdownMenu.ItemIndicator className="desk-menu-tick">
        <IconCheck />
      </DropdownMenu.ItemIndicator>
      {value}
    </DropdownMenu.RadioItem>
  )
}
