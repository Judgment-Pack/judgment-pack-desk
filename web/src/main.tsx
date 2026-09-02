import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider, createBrowserRouter } from 'react-router-dom'
import { App } from './App'
import { DeskConfigProvider } from './config/DeskConfigProvider'
import { IdentityProvider } from './identity/IdentityProvider'
import { McpProvider } from './mcp/McpProvider'
import './shell.css'
import './styles.css'

// The runtime is a local subprocess reading local files, and the chassis tells
// us when those files change. Refetching on window focus or on an interval
// would only add calls that the file watcher already covers.
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
      // The configuration feeds both the identity slot and the pane defaults,
      // so it is outermost. None of the three is a gate: the config query
      // fails closed to the built-in defaults, identity is display only, and
      // the pane state has a real default value of its own.
      <McpProvider>
        <DeskConfigProvider>
          <IdentityProvider>
            <App />
          </IdentityProvider>
        </DeskConfigProvider>
      </McpProvider>
    )
  }
])

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>
)
