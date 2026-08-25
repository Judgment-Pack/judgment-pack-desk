import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider, createBrowserRouter } from 'react-router-dom'
import { App } from './App'
import { McpProvider } from './mcp/McpProvider'
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
      <McpProvider>
        <App />
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
