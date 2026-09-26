import { initializeLanguage, languageReady } from './i18n'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider, createBrowserRouter } from 'react-router-dom'
import { ChatProvider } from './chat/ChatProvider'
import { App } from './App'
import { SessionGate } from './auth/SessionGate'
import { DeskConfigProvider } from './config/DeskConfigProvider'
import { IdentityProvider } from './identity/IdentityProvider'
import { McpProvider } from './mcp/McpProvider'
// Establish element defaults before the shell's component styles.
import '@fontsource-variable/noto-sans-kr/wght.css'
import './styles.css'
import './shell.css'

// The runtime is a local subprocess reading local files, and the chassis tells
// us when those files change. Refetching on window focus or on an interval
// would only add calls that the file watcher already covers.
const releaseLanguage = initializeLanguage()
if (import.meta.hot) import.meta.hot.dispose(releaseLanguage)

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      staleTime: Infinity,
      retry: false
    }
  }
})

const container = document.getElementById('root')
if (!container) throw new Error('no #root element to mount into')

// A *data* router, and the reason is one feature: `useBlocker`. Everything
// inside this application is same-document routing, which `beforeunload` never
// sees — so without a blocker, Back out of the authoring editor, or any in-app
// link, unmounts it and takes the unsaved buffer with it. The blocker is only
// available on a data router, so this is one.
const router = createBrowserRouter([
  {
    path: '*',
    element: (
      // Confirm local access before any project/config/chat provider mounts.
      // The backend remains the authority for every protected operation.
      <SessionGate><McpProvider>
        <DeskConfigProvider>
          <IdentityProvider>
            <ChatProvider><App /></ChatProvider>
          </IdentityProvider>
        </DeskConfigProvider>
      </McpProvider></SessionGate>
    )
  }
])

void languageReady().then(() => createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>
))
