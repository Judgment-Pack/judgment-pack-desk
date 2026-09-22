import { createContext, useContext, useEffect } from 'react'
import type { ConnectionProvider } from './client'

/** Metadata only. The shell owns the temporary surface, not a chat's portal. */
export interface ConnectionPaneRequest {
 source?: 'web'
 provider?: ConnectionProvider
 chatId?: string
 opener: HTMLElement | null
}
export const ConnectionPaneContext = createContext<{ open: (request: ConnectionPaneRequest) => void; busyChatId?: string; activeChatId?: string; close?: () => void }>({ open: () => {} })
export const useConnectionsPane = () => useContext(ConnectionPaneContext)

/** A pack write can lock the retained Assistant without navigating away. */
export function useConnectionChatLock(chatId: string, locked: boolean) {
 const { activeChatId, close } = useConnectionsPane()
 useEffect(() => { if (locked && activeChatId === chatId) close?.() }, [locked, chatId, activeChatId, close])
}
