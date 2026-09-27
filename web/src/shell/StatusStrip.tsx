import { msg, useLocale } from '../i18n'
/** Connection health stays compact; component versions live in Help & About.
 * Configuration warnings remain visible on every route.
 */
import { Link } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { connectionSays, useMcp } from '../mcp/McpProvider'

export const CONFIG_REFUSED_CUE = 'configuration refused — see Admin'

/**
 * The other half of the same cue, and a different sentence because it is a
 * different fact: the read did not produce a file, and it was not a 404.
 * "Refused" is a verdict the decoder reached about content; this one never got
 * that far. It deliberately does **not** say the file exists — a chassis
 * refusal says something about a file, but a socket that never answered
 * establishes only that absence was not established, and one cue covers both.
 * Admin is where the two are told apart.
 */
export const CONFIG_UNREAD_CUE = 'configuration could not be read — see Admin'

/**
 * The same two cues, in the spelling a phone has room for.
 *
 * The full sentence is about 263px wide in the strip's own face and a 320px
 * viewport leaves roughly 232px beside the console button, so the link — which
 * neither shrinks nor wraps, deliberately — painted across the button and off
 * the edge of a frame that clips. The short spelling is what is *painted*
 * there; the accessible name is the full sentence at every width, because it
 * is on the link rather than in its text.
 */
export const CONFIG_REFUSED_SHORT = 'config refused'
export const CONFIG_UNREAD_SHORT = 'config unread'

export function StatusStrip() {
  useLocale()
  const { status } = useMcp()
  const { problems, readFailure, desk } = useEffectiveConfig()
  // **Either file, one cue.** The strip's job is to stop a mistyped key
  // looking exactly like having written no file at all, and that argument does
  // not care which of the two files carried the typo. Admin names which.
  const refused = problems.length > 0 || (desk?.problems.length ?? 0) > 0
  const unread = readFailure !== undefined || desk?.readFailure !== undefined
  return (
    <footer className="desk-strip">
      <span className="desk-strip-left">
        {refused && <ConfigCue full={msg(CONFIG_REFUSED_CUE)} short={msg(CONFIG_REFUSED_SHORT)} />}
        {!refused && unread && (
          <ConfigCue full={msg(CONFIG_UNREAD_CUE)} short={msg(CONFIG_UNREAD_SHORT)} />
        )}
      </span>
      <Link to="/help" className="desk-strip-connection" aria-label={`${msg('Help & About')}: ${connectionSays(status)}`}>
        {connectionSays(status)}
      </Link>
    </footer>
  )
}

/**
 * One cue, two spellings, one accessible name.
 *
 * Both spellings are in the DOM and CSS paints exactly one of them, because
 * the alternative — choosing in JavaScript off a `matchMedia` — makes the
 * strip re-render on every drag of a window edge for a string. The name is on
 * the link, so it is the full sentence whichever is painted, and both spans
 * are `aria-hidden` so the short one never reaches the accessible name.
 */
function ConfigCue({ full, short }: { full: string; short: string }) {
  useLocale()
  return (
    <Link className="desk-strip-warn" to="/admin" aria-label={full}>
      <span className="desk-strip-warn-full" aria-hidden="true">
        {full}
      </span>
      <span className="desk-strip-warn-short" aria-hidden="true">
        {short}
      </span>
    </Link>
  )
}
