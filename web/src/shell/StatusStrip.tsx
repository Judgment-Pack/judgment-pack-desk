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
 */
import { useMcp } from '../mcp/McpProvider'
import { IconPanelBottom } from './icons'

export function StatusStrip({
  consoleOpen,
  onToggleConsole
}: {
  consoleOpen: boolean
  onToggleConsole: () => void
}) {
  const { server } = useMcp()
  return (
    <footer className="desk-strip">
      {server ? (
        <span>
          connected to <code>{server.name}</code> {server.version}
        </span>
      ) : (
        <span>not connected</span>
      )}
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
