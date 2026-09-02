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
 */
import { Link } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useMcp } from '../mcp/McpProvider'
import { IconPanelBottom } from './icons'

export const CONFIG_REFUSED_CUE = 'configuration refused — see Admin'

export function StatusStrip({
  consoleOpen,
  onToggleConsole
}: {
  consoleOpen: boolean
  onToggleConsole: () => void
}) {
  const { server } = useMcp()
  const { problems } = useEffectiveConfig()
  return (
    <footer className="desk-strip">
      <span className="desk-strip-left">
        {server ? (
          <span>
            connected to <code>{server.name}</code> {server.version}
          </span>
        ) : (
          <span>not connected</span>
        )}
        {problems.length > 0 && (
          <Link className="desk-strip-warn" to="/admin">
            {CONFIG_REFUSED_CUE}
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
