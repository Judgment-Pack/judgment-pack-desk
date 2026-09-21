import { createContext, useContext } from 'react'
import type { ConnectionProvider } from './client'

/** Metadata only. The shell owns the temporary surface, not a chat's portal. */
export interface ConnectionPaneRequest {
 provider?: ConnectionProvider
 chatId?: string
 opener: HTMLElement | null
}
export const ConnectionPaneContext = createContext<{ open: (request: ConnectionPaneRequest) => void; busyChatId?: string }>({ open: () => {} })
export const useConnectionsPane = () => useContext(ConnectionPaneContext)
