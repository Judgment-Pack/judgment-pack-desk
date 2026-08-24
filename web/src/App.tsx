import { Navigate, Route, Routes } from 'react-router-dom'
import { ErrorBox } from './components/primitives'
import { useMcp } from './mcp/McpProvider'
import { PackDetail } from './routes/PackDetail'
import { PackEvaluate } from './routes/PackEvaluate'
import { PackList } from './routes/PackList'

export function App() {
  const { status, error, server } = useMcp()

  return (
    <div className="app">
      <header className="app-head">
        <a className="brand" href="/">
          judgment&#8209;pack desk
        </a>
        <ConnectionBadge />
      </header>

      <main className="app-body">
        {status === 'failed' && error ? (
          <ErrorBox title="Not connected to the runtime" error={error} />
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
  const label = status === 'ready' ? 'connected' : status === 'connecting' ? 'connecting' : 'offline'
  return (
    <span className={`badge badge-${status}`} title={`MCP connection: ${label}`}>
      {label}
    </span>
  )
}
