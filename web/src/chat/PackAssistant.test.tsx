import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { BufferIdentity } from '../packs/edit/useDocumentBuffer'
import { PackAssistant } from './PackAssistant'

const fake = vi.hoisted(() => ({ snapshot: {} as any, editing: {} as any }))
vi.mock('./ChatProvider', () => ({ useChats: () => fake.snapshot }))
vi.mock('../shell/InspectorSlot', () => ({ useInspectorSlot: () => ({ open: false }) }))
vi.mock('../packs/edit/editingContext', () => ({ useEditing: () => fake.editing }))
vi.mock('./ChatPanel', () => ({ ChatPanel: ({ context, proposalActions }: any) => <><button onClick={context?.beforeSend}>Send request</button>{proposalActions}</> }))
afterEach(cleanup)
const original = '{"id":"original"}'
const base = { path: 'packs/p.json', generation: 1, revision: 1 } as BufferIdentity
const chat = { id: 'chat-one', pack: { id: 'p' }, updatedAt: '2026-01-01' }
const store = { activate: vi.fn() }
function snapshot(revision: number, status = 'ready') {
  return { state: { status, restored: false, candidates: [{ revision, document: { id: 'proposed' }, digest: `digest-${revision}` }] } }
}
function view(identity = base, draft = original) {
  return <MemoryRouter><PackAssistant packId="p" editing draft={draft} identity={identity} busy={() => ''} diagnostics={undefined} /></MemoryRouter>
}
beforeEach(() => {
  fake.snapshot = { store, ready: true, chats: [chat], drafts: [], bindings: new Map([['chat-one', snapshot(1)]]) }
  fake.editing = { editing: true, pending: new Set(), write: vi.fn() }
})
it('does not offer an older candidate as the answer to a new clarification', () => {
  const rendered = render(view())
  fireEvent.click(screen.getByRole('button', { name: 'Send request' }))
  fake.snapshot.bindings = new Map([['chat-one', snapshot(1, 'needs-input')]])
  rendered.rerender(view())
  expect(screen.queryByText('Review proposed changes')).toBeNull()
})
it('offers only a new candidate and refuses it when the buffer revision changes', () => {
  const rendered = render(view())
  fireEvent.click(screen.getByRole('button', { name: 'Send request' }))
  fake.snapshot.bindings = new Map([['chat-one', snapshot(2)]])
  rendered.rerender(view())
  fireEvent.click(screen.getByText('Review proposed changes'))
  expect(screen.getByRole('button', { name: 'Apply to draft' }).hasAttribute('disabled')).toBe(false)
  // Bytes can return to their original value after an intervening edit; the
  // editor's revision is still a new baseline and must reject the old answer.
  rendered.rerender(view({ ...base, revision: 3 }))
  expect(screen.getByRole('button', { name: 'Apply to draft' }).hasAttribute('disabled')).toBe(true)
  expect(fake.editing.write).not.toHaveBeenCalled()
})
