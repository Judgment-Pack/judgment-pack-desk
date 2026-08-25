import { Navigate, Route, Routes } from 'react-router-dom'
import { ErrorBox } from './components/primitives'
import { AuthorView } from './routes/AuthorView'
import { useMcp } from './mcp/McpProvider'
import { GraphView } from './routes/GraphView'
import { MatrixView } from './routes/MatrixView'
import { PackDetail } from './routes/PackDetail'
import { PackEvaluate } from './routes/PackEvaluate'
import { ProjectHome } from './routes/ProjectHome'

export function App() {
  const { status, error, server, everConnected, attempt, retryNow, known, capabilitiesError } =
    useMcp()
  // A connection that has never been made has nothing to show behind a banner,
  // so the reason takes the page. Once the desk has been connected, a drop is a
  // banner over what is already on screen: the reconnect is automatic, and
  // throwing the view away would lose the user's place over a local restart.
  const blocked = status === 'failed' || (status === 'reconnecting' && !everConnected)

  return (
    <div className="app">
      <header className="app-head">
        <a className="brand" href="/">
          judgment&#8209;pack desk
        </a>
        <ConnectionBadge />
      </header>

      <main className="app-body">
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
        {blocked && error ? (
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
        ) : (
          <Routes>
            <Route path="/" element={<ProjectHome />} />
            <Route path="/matrix" element={<MatrixView />} />
            <Route path="/author" element={<AuthorView />} />
            <Route path="/graphs" element={<GraphView />} />
            <Route path="/graphs/:graphId" element={<GraphView />} />
            <Route path="/packs/:packId" element={<PackDetail />} />
            <Route path="/packs/:packId/evaluate" element={<PackEvaluate />} />
            <Route path="/packs/:packId/matrix" element={<MatrixView />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        )}
      </main>

      <footer className="app-foot">
        {server ? (
          <span>
            connected to <code>{server.name}</code> {server.version}
          </span>
        ) : (
          <span>not connected</span>
        )}
      </footer>
    </div>
  )
}

function ConnectionBadge() {
  const { status } = useMcp()
  const label =
    status === 'ready'
      ? 'connected'
      : status === 'connecting'
        ? 'connecting'
        : status === 'reconnecting'
          ? 'reconnecting'
          : 'offline'
  return (
    <span className={`badge badge-${status}`} title={`MCP connection: ${label}`}>
      {label}
    </span>
  )
}
