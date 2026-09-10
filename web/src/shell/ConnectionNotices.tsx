/**
 * The three connection notices, lifted out of `App.tsx` with their comments
 * and their wording intact.
 *
 * They live in `<main>` and never in a collapsible pane: **nothing whose
 * absence changes what is on screen may live somewhere a viewer has closed.**
 * The console gets the history line; the governing statement stays where it
 * governs.
 *
 * Three exports rather than one component, so `App.tsx` keeps the original
 * control flow character for character — including the part that is easy to
 * lose in a rewrite: `blocked && error ?` means a blocked connection whose
 * error is null still renders the routes.
 */
import { ErrorBox } from '../components/primitives'
import { useMcp } from '../mcp/McpProvider'

/** The two banners that sit above whatever is on screen. */
export function ConnectionNotices() {
  const { status, everConnected, attempt, retryNow, known, capabilitiesError } = useMcp()
  return (
    <>
      {status === 'reconnecting' && everConnected && (
        <p className="banner" role="status">
          Lost the connection to the chassis. Reconnecting (attempt {attempt})…{' '}
          <button type="button" className="link-button" onClick={retryNow}>
            try now
          </button>
        </p>
      )}
      {/* Connected, and this page does not know what it is connected to. Every
          feature-detected capability is off while that holds, and saying so is
          the difference between a page with less on it and a page quietly
          claiming the runtime has less on it. */}
      {status === 'ready' && !known && (
        <p className="banner" role="status">
          The runtime's tool listing could not be read
          {capabilitiesError ? ` — ${capabilitiesError.message}` : ''}. What this runtime can do
          is unknown rather than known to be little, so the optional surfaces are left off and
          nothing here should be read as the runtime lacking them.{' '}
          <button type="button" className="link-button" onClick={retryNow}>
            reconnect and ask again
          </button>
        </p>
      )}
    </>
  )
}

/**
 * The error that takes the page, or null.
 *
 * A connection that has never been made has nothing to show behind a banner,
 * so the reason takes the page. Once the desk has been connected, a drop is a
 * banner over what is already on screen: the reconnect is automatic, and
 * throwing the view away would lose the user's place over a local restart.
 *
 * Null where the connection is blocked but carries no error, because that is
 * what `blocked && error ?` did: the routes render.
 */
export function useBlockingError(): Error | null {
  const { status, error, everConnected } = useMcp()
  const blocked = status === 'failed' || (status === 'reconnecting' && !everConnected)
  return blocked && error ? error : null
}

export function BlockedNotice({ error }: { error: Error }) {
  const { status, attempt, retryNow } = useMcp()
  return (
    <>
      <ErrorBox title="Not connected to the runtime" error={error} />
      {status === 'reconnecting' && (
        <p className="note">
          Retrying automatically (attempt {attempt}).{' '}
          <button type="button" className="link-button" onClick={retryNow}>
            Try now
          </button>
        </p>
      )}
    </>
  )
}
