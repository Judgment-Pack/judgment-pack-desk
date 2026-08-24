import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import type { Notification } from '@modelcontextprotocol/sdk/types.js'
import { useQueryClient } from '@tanstack/react-query'
import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { DeskWebSocketTransport } from './transport'

export type ConnectionStatus = 'connecting' | 'ready' | 'failed'

export interface McpConnection {
  client: Client | null
  status: ConnectionStatus
  error: Error | null
  /** The runtime that answered initialize, for the status bar. */
  server: { name: string; version: string } | null
}

const McpContext = createContext<McpConnection>({
  client: null,
  status: 'connecting',
  error: null,
  server: null
})

export function useMcp(): McpConnection {
  return useContext(McpContext)
}

/**
 * The session token arrives in the URL the chassis prints. Keeping it in
 * sessionStorage lets client-side navigation drop it from the address bar
 * without losing the connection, and scopes it to this tab.
 */
const TOKEN_KEY = 'jpack-desk-token'

function sessionToken(): string {
  const fromUrl = new URLSearchParams(window.location.search).get('token')
  if (fromUrl) {
    window.sessionStorage.setItem(TOKEN_KEY, fromUrl)
    return fromUrl
  }
  return window.sessionStorage.getItem(TOKEN_KEY) ?? ''
}

function socketURL(token: string): string {
  const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${scheme}//${window.location.host}/ws?token=${encodeURIComponent(token)}`
}

/**
 * McpProvider makes the browser the MCP client: it connects the official SDK
 * Client over the chassis relay, runs initialize once, and hands the connected
 * client to the views. There is no desk-specific API in between.
 */
export function McpProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [connection, setConnection] = useState<McpConnection>({
    client: null,
    status: 'connecting',
    error: null,
    server: null
  })

  useEffect(() => {
    let disposed = false
    const client = new Client({ name: 'judgment-pack-desk', version: '0.1.0' }, { capabilities: {} })

    // desk/fileChanged is the chassis' own notification and has no SDK schema.
    // The fallback handler is the SDK's supported way to receive a method it
    // does not know, so no schema has to be invented for it here.
    client.fallbackNotificationHandler = async (notification: Notification) => {
      if (notification.method !== 'desk/fileChanged') return
      // The runtime reads the project tree on every call, so any change under
      // it can make any cached answer stale. Invalidating the whole desk cache
      // is both correct and cheap: these are local calls to a local process.
      await queryClient.invalidateQueries()
    }

    const token = sessionToken()
    if (!token) {
      setConnection({
        client: null,
        status: 'failed',
        error: new Error(
          'No session token. Open the URL that jpack-desk printed at startup — it carries ?token=…'
        ),
        server: null
      })
      return
    }

    const transport = new DeskWebSocketTransport(socketURL(token))

    client
      .connect(transport)
      .then(() => {
        if (disposed) return
        const info = client.getServerVersion()
        setConnection({
          client,
          status: 'ready',
          error: null,
          server: info ? { name: info.name, version: info.version } : null
        })
      })
      .catch((cause: unknown) => {
        if (disposed) return
        setConnection({
          client: null,
          status: 'failed',
          error: cause instanceof Error ? cause : new Error(String(cause)),
          server: null
        })
      })

    return () => {
      disposed = true
      void client.close()
    }
  }, [queryClient])

  return <McpContext.Provider value={connection}>{children}</McpContext.Provider>
}
