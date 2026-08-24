import { Navigate, Route, Routes } from 'react-router-dom'
import { ErrorBox } from './components/primitives'
import { useMcp } from './mcp/McpProvider'
import { PackDetail } from './routes/PackDetail'
import { PackEvaluate } from './routes/PackEvaluate'
import { PackList } from './routes/PackList'

export function App() {
  const { status, error, server, everConnected, attempt, retryNow } = useMcp()
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
            <Route path="/" element={<PackList />} />
            <Route path="/packs/:packId" element={<PackDetail />} />
            <Route path="/packs/:packId/evaluate" element={<PackEvaluate />} />
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
