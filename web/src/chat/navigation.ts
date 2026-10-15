import { draftHref } from '../packs/drafts/model'
import { msg } from '../i18n'
import type { NavigateFunction } from 'react-router-dom'
import type { Chat } from './store'

export function homeChatId(state: unknown): string | undefined {
  if (!state || typeof state !== 'object' || !('homeChatId' in state)) return undefined
  return typeof state.homeChatId === 'string' ? state.homeChatId : undefined
}

export type ChatScope = { kind: 'pack' | 'draft' | 'graph'; id: string } | { kind: 'input'; id: string; originChatId?: string }
export function chatScope(chat: Chat): ChatScope | undefined {
  return chat.input ? {kind:'input',id:chat.input.id} : chat.graph ? {kind: 'graph', id: chat.graph.workspace} : chat.pack ? {kind: 'pack', id: chat.pack.id} : chat.draftId ? {kind: 'draft', id: chat.draftId} : undefined
}
export function inChatScope(chat: Chat, scope: ChatScope): boolean {
  if (scope.kind === 'input') {
    // A conversation can create several Dialects without changing its original home.
    if (scope.id === 'list') return Boolean(chat.input || chat.inputDrafts?.length || chat.inputActions?.length)
    return chat.input?.id === scope.id || chat.id === scope.originChatId
      || Boolean(chat.inputDrafts?.some(draft => draft.savedInputId === scope.id)
        || chat.inputActions?.some(action => action.inputId === scope.id))
  }
  const own = chatScope(chat)
  return own?.kind === scope.kind && own.id === scope.id
}
export function chatHref(chat: Chat, _location?: { pathname: string; search: string }): string { return `/chats/${chat.id}` }
export function dialectChatHref(inputId: string, chatId: string, location?: { pathname: string; search: string }): string {
  const path = inputId === 'list' ? '/dialects' : '/dialects/' + inputId
  const params = new URLSearchParams(location?.pathname === path ? location.search : '')
  params.set('chat', chatId)
  return path + '?' + params
}
export function assistantChatHref(chat: Chat, location?: { pathname: string; search: string }, scope?: ChatScope): string {
  if (scope?.kind === 'input') return dialectChatHref(scope.id, chat.id, location)
  if (chat.input) return dialectChatHref(chat.input.id, chat.id, location)
  if (chat.graph) {
    const params = new URLSearchParams(location?.pathname.startsWith('/graphs') ? location.search : 'view=compose')
    if (params.has('draft') && !params.has('draftChat') && params.has('chat')) params.set('draftChat', params.get('chat')!)
    params.set('chat', chat.id)
    return `${location?.pathname.startsWith('/graphs') ? location.pathname : `/graphs/${encodeURIComponent(chat.graph.id)}`}?${params}`
  }
  if (chat.draftId && !chat.pack) {
    const path = draftHref(chat.draftId)
    const params = new URLSearchParams(location?.pathname === path ? location.search : '')
    params.set('chat', chat.id)
    return `${path}?${params}`
  }
  if (!chat.pack) return `/chats/${chat.id}`
  const path = `/packs/${encodeURIComponent(chat.pack.id)}`
  const casePrefix = `${path}/matrix/cases/`
  const onCase = Boolean(location?.pathname.startsWith(casePrefix) && location.pathname.length > casePrefix.length && !location.pathname.slice(casePrefix.length).includes('/'))
  const onPack = location?.pathname === path || location?.pathname === `${path}/evaluate` || location?.pathname === `${path}/matrix` || onCase
  const params = new URLSearchParams(onPack ? location!.search : '')
  params.set('chat', chat.id)
  return `${onPack ? location!.pathname : path}?${params}`
}

/** The history entry keeps the home conversation without putting its id in the URL. */
export function openNewChat(navigate: NavigateFunction, chat: Chat, location?: { pathname: string; search: string }) {
  if(chat.input&&location?.pathname.startsWith('/dialects'))navigate(assistantChatHref(chat,location))
  else if (chat.graph && location?.pathname.startsWith('/graphs')) navigate(assistantChatHref(chat, location))
  else if ((chat.pack || chat.draftId) && location?.pathname.startsWith('/packs/')) navigate(assistantChatHref(chat, location))
  else navigate('/', { state: { homeChatId: chat.id } })
}

export function hasChatContent(chat: Chat): boolean {
  return Boolean(chat.composer.trim() || chat.attachments?.length || chat.inputDrafts?.length || chat.inputActions?.length || chat.checkpoint?.state.turns.length || chat.checkpoint?.state.candidates.length || chat.pinned || chat.title !== 'New chat')
}

export function chatTitle(chat: Chat): string {
  return chat.title === 'New chat' && chat.composer.trim() ? chat.composer.trim().split('\n')[0]!.slice(0, 120) : chat.title === 'New chat' ? msg('New chat') : chat.title
}
