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
 * a file that **could not be read**. A 413, a permission error, a non-UTF-8
 * body or a dead socket all resolved to the built-in defaults with the reason
 * recorded where nothing rendered it, so a configured desk that could not open
 * its own file looked exactly like a desk with no file. Absence stays silent —
 * that is the ordinary case — and unreadability does not.
 */
import { Link } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useMcp } from '../mcp/McpProvider'
import { IconPanelBottom } from './icons'

export const CONFIG_REFUSED_CUE = 'configuration refused — see Admin'

/**
 * The other half of the same cue, and a different sentence because it is a
 * different fact: the file is there and the desk could not read it. "Refused"
 * is a verdict the decoder reached about content; this one never got that far.
 */
export const CONFIG_UNREAD_CUE = 'configuration could not be read — see Admin'

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
          <Link className="desk-strip-warn" to="/admin">
            {CONFIG_REFUSED_CUE}
          </Link>
        )}
        {problems.length === 0 && readFailure !== undefined && (
          <Link className="desk-strip-warn" to="/admin">
            {CONFIG_UNREAD_CUE}
          </Link>
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
