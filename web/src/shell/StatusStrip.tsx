/**
 * The 28px strip: the console's collapsed face, and today's footer sentence.
 *
 * It is its own grid row and a direct child of the grid, so it is the
 * `contentinfo` landmark `.app-foot` was — and it carries the same two
 * sentences, character for character, read from `useMcp()` **directly**.
 *
 * It derives nothing. The connection appears in exactly two places in this
 * desk — here and the header badge — and both are deliberately dumb: a
 * "helpful" summary in either is how the two would drift into disagreeing
 * about one connection.
 *
 * The one other thing it carries is a **cue that the project's configuration
 * was refused**. A refused file is the built-in defaults, and that is correct;
 * what was missing is that it was indistinguishable from having no file at
 * all. Admin names every problem, but nothing outside `/admin` said there was
 * one, so an operator who wrote `jpack-desk.json` and mistyped a key saw the
 * fallback name and no reason. This is not a verdict the shell computed: it is
 * the decoder's own refusal, counted and linked to the page that explains it.
 *
 * There is a second cue beside it, for the case the first one missed entirely:
 * a read that **could not produce a file**. A 413, a permission error, a
 * non-UTF-8 body or a dead socket all resolved to the built-in defaults with
 * the reason recorded where nothing rendered it, so a desk that could not open
 * its own file looked exactly like a desk with no file. Absence stays silent —
 * that is the ordinary case — and this does not. The cue says only that the
 * read failed: a chassis refusal says something about a file, while a socket
 * that never answered establishes only that absence was not established, and
 * the strip is not the place to tell those apart. Admin is.
 */
import { Link } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useMcp } from '../mcp/McpProvider'
import { IconPanelBottom } from './icons'

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

export function StatusStrip({
  consoleOpen,
  onToggleConsole
}: {
  consoleOpen: boolean
  onToggleConsole: () => void
}) {
  const { server } = useMcp()
  const { problems, readFailure } = useEffectiveConfig()
  return (
    <footer className="desk-strip">
      <span className="desk-strip-left">
        {server ? (
          <span className="desk-strip-connection">
            connected to <code>{server.name}</code> {server.version}
          </span>
        ) : (
          <span className="desk-strip-connection">not connected</span>
        )}
        {problems.length > 0 && (
          <ConfigCue full={CONFIG_REFUSED_CUE} short={CONFIG_REFUSED_SHORT} />
        )}
        {problems.length === 0 && readFailure !== undefined && (
          <ConfigCue full={CONFIG_UNREAD_CUE} short={CONFIG_UNREAD_SHORT} />
        )}
      </span>
      <button
        type="button"
        className="desk-icon-button"
        aria-label={consoleOpen ? 'Collapse console' : 'Expand console'}
        aria-expanded={consoleOpen}
        aria-controls="desk-console"
        onClick={onToggleConsole}
      >
        <IconPanelBottom />
      </button>
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
