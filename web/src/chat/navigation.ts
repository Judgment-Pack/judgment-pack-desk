import { msg } from '../i18n'
import type { NavigateFunction } from 'react-router-dom'
import type { Chat } from './store'

export function homeChatId(state: unknown): string | undefined {
  if (!state || typeof state !== 'object' || !('homeChatId' in state)) return undefined
  return typeof state.homeChatId === 'string' ? state.homeChatId : undefined
}

export function chatHref(chat: Chat, location?: { pathname: string; search: string }): string {
  if (!chat.pack) return `/chats/${chat.id}`
  const path = `/packs/${encodeURIComponent(chat.pack.id)}`
  const params = new URLSearchParams(location?.pathname === path ? location.search : '')
  params.set('chat', chat.id)
  return `${path}?${params}`
}

/** The history entry keeps the home conversation without putting its id in the URL. */
export function openNewChat(navigate: NavigateFunction, chat: Chat, location?: { pathname: string; search: string }) {
  if (chat.pack) navigate(chatHref(chat, location))
  else navigate('/', { state: { homeChatId: chat.id } })
}

export function hasChatContent(chat: Chat): boolean {
  return Boolean(chat.composer.trim() || chat.attachments?.length || chat.checkpoint?.state.turns.length || chat.checkpoint?.state.candidates.length || chat.pinned || chat.title !== 'New chat')
}

export function chatTitle(chat: Chat): string {
  return chat.title === 'New chat' && chat.composer.trim() ? chat.composer.trim().split('\n')[0]!.slice(0, 120) : chat.title === 'New chat' ? msg('New chat') : chat.title
}
