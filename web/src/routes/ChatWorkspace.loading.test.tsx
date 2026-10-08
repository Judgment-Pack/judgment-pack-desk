/**
 * The chat page while chat history loads: the composer is shown at once, on a
 * placeholder chat that is presentation only — locked, and never activated,
 * started, updated or persisted in the chat store.
 */
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { Chat } from '../chat/store'
import { ChatWorkspace } from './ChatWorkspace'

interface PanelProps { chat: Chat; locked?: boolean; historyLoading?: boolean; landing?: boolean }
const panels: PanelProps[] = []
const store = { activate: vi.fn(), startChat: vi.fn(() => ({ id: 'started' })), update: vi.fn(), load: vi.fn(), canCreate: true }
const chats = vi.hoisted(() => ({ value: {} as Record<string, unknown> }))

vi.mock('../chat/ChatProvider', () => ({ useChats: () => chats.value }))
vi.mock('../chat/ChatPanel', () => ({
  ChatPanel: (props: PanelProps) => {
    panels.push(props)
    return <div data-testid="panel" data-chat={props.chat.id} data-locked={String(props.locked)} />
  }
}))

beforeEach(() => {
  panels.length = 0
  vi.clearAllMocks()
})
afterEach(cleanup)

const saved = { id: 'c1', title: 'Saved', pinned: false, archived: false, updatedAt: '2026-10-01T00:00:00Z', composer: '', model: '', mode: 'draft', view: 'chat' } as Chat

function show(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/" element={<ChatWorkspace />} />
        <Route path="/chats/:chatId" element={<ChatWorkspace />} />
      </Routes>
    </MemoryRouter>
  )
}

it('shows the composer on a locked placeholder while history loads, and writes nothing to the store', () => {
  chats.value = { store, chats: [], drafts: [], ready: false, error: undefined }
  for (const path of ['/', '/chats/c1']) {
    show(path)
    const last = panels.at(-1)!
    expect(last.chat.id, path).toBe('loading-history')
    expect(last.locked, path).toBe(true)
    expect(last.historyLoading, path).toBe(true)
    cleanup()
  }
  expect(store.activate).not.toHaveBeenCalled()
  expect(store.startChat).not.toHaveBeenCalled()
  expect(store.update).not.toHaveBeenCalled()
})

it('opens the saved chat unlocked once history has loaded', () => {
  chats.value = { store, chats: [saved], drafts: [], ready: true, error: undefined }
  show('/chats/c1')
  const last = panels.at(-1)!
  expect(last.chat.id).toBe('c1')
  expect(last.locked).toBe(false)
  expect(last.historyLoading).toBe(false)
  expect(store.activate).toHaveBeenCalledWith('c1')
  expect(panels.some((each) => each.chat.id === 'loading-history')).toBe(false)
})

it('says why history could not load, offers a retry, and shows no composer', () => {
  chats.value = { store, chats: [], drafts: [], ready: false, error: 'Chat history could not be read.' }
  show('/')
  expect(screen.getByRole('alert').textContent).toContain('Chat history could not be read.')
  expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy()
  expect(screen.queryByTestId('panel')).toBeNull()
})

it('says a linked chat is no longer in history, rather than loading for ever', () => {
  chats.value = { store, chats: [], drafts: [], ready: true, error: undefined }
  show('/chats/gone')
  expect(screen.getByRole('status').textContent).toContain('This chat is no longer in history.')
  expect(screen.queryByTestId('panel')).toBeNull()
})
