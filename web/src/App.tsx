import {TemplatesPage} from './exchange/TemplatesPage'
import { DialectsView } from './routes/DialectsView'
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

export function App() {
  // A temporary connection loss keeps the workspace mounted; status lives in the footer.
  const location = useLocation()

  return (
    <AppShell>
        <Routes>
          <Route path="/" element={<ChatWorkspace />} />
          <Route path="/matrix" element={<Navigate to="/library" replace />} />
          <Route path="/connections/:provider/files" element={<StorageFilesView />} />
          <Route path="/author" element={<AuthorView />} />
          <Route path="/dialects/templates" element={<TemplatesPage />} />
          <Route path="/dialects/new" element={<DialectsView />} />
          <Route path="/dialects/drafts/:draftId" element={<DialectsView />} />
          <Route path="/dialects" element={<DialectsView />} />
          <Route path="/dialects/:preparationId" element={<DialectsView />} />
          <Route path="/jobs" element={<JobsView />} />
          <Route path="/jobs/runs" element={<JobsView />} />
          <Route path="/jobs/new" element={<JobsView />} />
          <Route path="/jobs/:jobId" element={<JobsView />} />
          <Route path="/jobs/:jobId/runs/:runId" element={<JobsView />} />
          <Route path="/create-pack" element={<Navigate to={`/packs/new${location.search}`} replace />} />
          <Route path="/chats" element={<ChatHistoryPage />} />
          <Route path="/chats/:chatId" element={<ChatWorkspace />} />
          <Route path="/create-pack/research" element={<Navigate to="/create-pack?mode=research" replace />} />
          {/* Library and its documents share folder and browser state. The
              collection URL names both kinds; document URLs retain their type. */}
          <Route element={<PacksLayout />}>
            <Route path="/library" element={<PacksIndex />} />
            <Route path="/packs/new" element={<NewPackView />} />
            <Route path="/packs/_review" element={<ReviewAndLockView />} />
            <Route path="/packs/_upgrade" element={<UpgradeView />} />
            <Route path="/packs/drafts/:draftId" element={<DraftPackView />} />
            <Route path="/packs/:packId" element={<PackView />} />
            <Route path="/packs/:packId/evaluate" element={<PackTests />} />
            <Route path="/packs/:packId/matrix" element={<PackTests />} />
            <Route path="/packs/:packId/matrix/cases/:caseId" element={<PackTests />} />
            <Route path="/graphs" element={<GraphView />} />
            <Route path="/graphs/:graphId" element={<GraphView />} />
          </Route>
          <Route path="/admin" element={<AdminView />} />
          <Route path="/help" element={<HelpAbout />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
    </AppShell>
  )
}
