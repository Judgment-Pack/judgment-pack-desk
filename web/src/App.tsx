import { JobsView } from './routes/JobsView'
import { DraftPackView, NewPackView } from './routes/DraftPackView'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { ChatWorkspace } from './routes/ChatWorkspace'
import { ChatHistoryPage } from './routes/ChatHistoryPage'

import { AdminView } from './routes/AdminView'
import { StorageFilesView } from './routes/StorageFilesView'
import { AuthorView } from './routes/AuthorView'
import { GraphView } from './routes/GraphView'
import { HelpAbout } from './routes/HelpAbout'
import { PackTests } from './routes/PackTests'
import { PackView } from './routes/PackView'
import { PacksIndex } from './routes/PacksIndex'
import { PacksLayout } from './routes/PacksLayout'
import { ReviewAndLockView } from './packs/review/ReviewAndLockView'
import { UpgradeView } from './packs/upgrade/UpgradeView'
import { AppShell } from './shell/AppShell'
import { BlockedNotice, useBlockingError } from './shell/ConnectionNotices'

export function App() {
  // A temporary connection loss keeps the workspace mounted; status lives in the footer.
  const blocking = useBlockingError()
  const location = useLocation()

  return (
    <AppShell>
      {blocking && !location.pathname.startsWith('/jobs') ? (
        <BlockedNotice error={blocking} />
      ) : (
        <Routes>
          <Route path="/" element={<ChatWorkspace />} />
          <Route path="/matrix" element={<Navigate to="/packs" replace />} />
          <Route path="/connections/:provider/files" element={<StorageFilesView />} />
          <Route path="/author" element={<AuthorView />} />
          <Route path="/jobs" element={<JobsView />} />
          <Route path="/jobs/runs" element={<JobsView />} />
          <Route path="/jobs/new" element={<JobsView />} />
          <Route path="/jobs/:jobId" element={<JobsView />} />
          <Route path="/jobs/:jobId/runs/:runId" element={<JobsView />} />
          <Route path="/graphs" element={<PacksLayout />}>
            <Route index element={<GraphView />} />
            <Route path=":graphId" element={<GraphView />} />
          </Route>
          {/* All pack views share folder location and browser state. Changing
              tabs changes the document view, not its place in the collection. */}
          <Route path="/create-pack" element={<Navigate to={`/packs/new${location.search}`} replace />} />
          <Route path="/chats" element={<ChatHistoryPage />} />
          <Route path="/chats/:chatId" element={<ChatWorkspace />} />
          <Route path="/create-pack/research" element={<Navigate to="/create-pack?mode=research" replace />} />
          <Route path="/packs" element={<PacksLayout />}>
            <Route index element={<PacksIndex />} />
            <Route path="new" element={<NewPackView />} />
            <Route path="_review" element={<ReviewAndLockView />} />
            <Route path="_upgrade" element={<UpgradeView />} />
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
