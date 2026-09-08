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
 * The sentence about the token is checked against the code rather than
 * inherited from the spec: `McpProvider` copies `?token=` into `sessionStorage`
 * on first load, and nothing calls `history.replaceState` — so the token
 * leaves the address bar at the first in-app navigation, not at load. Saying
 * "it leaves the URL immediately" would be a claim this desk does not keep.
 */
import { Avatar, DropdownMenu } from 'radix-ui'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { IconChevronDown } from '../shell/icons'
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
 * What a reset did, in three sentences rather than one.
 *
 * They are three different facts and a menu that reported all of them as
 * "Cleared." would be stating one it never observed: the record may be gone,
 * this browser's storage may have refused the deletion, or the chassis may not
 * yet have said which project this desk is open on — in which case the key is
 * provisional and nothing is cleared under it.
 */
export const RESET_SAYS: Record<ResetOutcome, string> = {
  cleared: 'Cleared — the panes are back on their defaults.',
  refused: 'this browser did not clear the record — the layout is unchanged',
  unresolved: 'nothing was cleared: this desk has not been told which project it is open on'
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
          <DropdownMenu.Item asChild className="desk-menu-item">
            <Link to="/admin#appearance">Appearance</Link>
          </DropdownMenu.Item>
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
