import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
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

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <McpProvider>
          <App />
        </McpProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>
)
