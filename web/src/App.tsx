import { JobsView } from './routes/JobsView'
import { DraftPackView, NewPackView } from './routes/DraftPackView'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { ChatWorkspace } from './routes/ChatWorkspace'
import { ChatHistoryPage } from './routes/ChatHistoryPage'

import { AdminView } from './routes/AdminView'
import { AuthorView } from './routes/AuthorView'
import { GraphView } from './routes/GraphView'
import { HelpAbout } from './routes/HelpAbout'
import { PackTests } from './routes/PackTests'
import { PackView } from './routes/PackView'
import { PacksIndex } from './routes/PacksIndex'
import { PacksLayout } from './routes/PacksLayout'
import { AppShell } from './shell/AppShell'
import { BlockedNotice, ConnectionNotices, useBlockingError } from './shell/ConnectionNotices'

export function App() {
  // The three connection notices and the blocked reason moved into
  // `ConnectionNotices.tsx`, comments and wording intact. They render inside
  // `<main>` and never inside a collapsible pane: nothing whose absence
  // changes what is on screen may live somewhere a viewer has closed.
  const blocking = useBlockingError()
  const location = useLocation()

  return (
    <AppShell>
      <ConnectionNotices />
      {blocking && !location.pathname.startsWith('/jobs') ? (
        <BlockedNotice error={blocking} />
      ) : (
        <Routes>
          <Route path="/" element={<ChatWorkspace />} />
          <Route path="/matrix" element={<Navigate to="/packs" replace />} />
          <Route path="/author" element={<AuthorView />} />
          <Route path="/jobs" element={<JobsView />} />
          <Route path="/jobs/new" element={<JobsView />} />
          <Route path="/jobs/:jobId" element={<JobsView />} />
          <Route path="/jobs/:jobId/runs/:runId" element={<JobsView />} />
          <Route path="/graphs" element={<GraphView />} />
          <Route path="/graphs/:graphId" element={<GraphView />} />
          {/* All pack views share folder location and browser state. Changing
              tabs changes the document view, not its place in the collection. */}
          <Route path="/create-pack" element={<Navigate to={`/packs/new${location.search}`} replace />} />
          <Route path="/chats" element={<ChatHistoryPage />} />
          <Route path="/chats/:chatId" element={<ChatWorkspace />} />
          <Route path="/create-pack/research" element={<Navigate to="/create-pack?mode=research" replace />} />
          <Route path="/packs" element={<PacksLayout />}>
            <Route index element={<PacksIndex />} />
            <Route path="new" element={<NewPackView />} />
            <Route path="drafts/:draftId" element={<DraftPackView />} />
            <Route path=":packId" element={<PackView />} />
            <Route path=":packId/evaluate" element={<PackTests />} />
            <Route path=":packId/matrix" element={<PackTests />} />
          </Route>
          <Route path="/admin" element={<AdminView />} />
          <Route path="/help" element={<HelpAbout />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      )}
    </AppShell>
  )
}
