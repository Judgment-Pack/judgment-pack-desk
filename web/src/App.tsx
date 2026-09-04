import { Navigate, Route, Routes } from 'react-router-dom'
import { AdminView } from './routes/AdminView'
import { AuthorView } from './routes/AuthorView'
import { GraphView } from './routes/GraphView'
import { HelpAbout } from './routes/HelpAbout'
import { MatrixView } from './routes/MatrixView'
import { PackEvaluate } from './routes/PackEvaluate'
import { PackView } from './routes/PackView'
import { PacksIndex } from './routes/PacksIndex'
import { PacksLayout } from './routes/PacksLayout'
import { ProjectHome } from './routes/ProjectHome'
import { AppShell } from './shell/AppShell'
import { BlockedNotice, ConnectionNotices, useBlockingError } from './shell/ConnectionNotices'

export function App() {
  // The three connection notices and the blocked reason moved into
  // `ConnectionNotices.tsx`, comments and wording intact. They render inside
  // `<main>` and never inside a collapsible pane: nothing whose absence
  // changes what is on screen may live somewhere a viewer has closed.
  const blocking = useBlockingError()

  return (
    <AppShell>
      <ConnectionNotices />
      {blocking ? (
        <BlockedNotice error={blocking} />
      ) : (
        <Routes>
          <Route path="/" element={<ProjectHome />} />
          <Route path="/matrix" element={<MatrixView />} />
          <Route path="/author" element={<AuthorView />} />
          <Route path="/graphs" element={<GraphView />} />
          <Route path="/graphs/:graphId" element={<GraphView />} />
          {/* A layout route, so the packs pane survives every change to the
              child — a different pack, and `?edit` when it lands. Evaluate and
              Matrix stay outside it: neither was drawn beside a pane, and
              nesting them would hand them one they never asked for. */}
          <Route path="/packs" element={<PacksLayout />}>
            <Route index element={<PacksIndex />} />
            <Route path=":packId" element={<PackView />} />
          </Route>
          <Route path="/packs/:packId/evaluate" element={<PackEvaluate />} />
          <Route path="/packs/:packId/matrix" element={<MatrixView />} />
          <Route path="/admin" element={<AdminView />} />
          <Route path="/help" element={<HelpAbout />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      )}
    </AppShell>
  )
}
