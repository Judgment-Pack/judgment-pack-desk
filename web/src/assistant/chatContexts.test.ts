import { afterEach, expect, it } from 'vitest'
import { ContextLedger } from '@judgment-pack/harness/context/ledger'
import { CONTEXT_DEFAULTS } from '@judgment-pack/harness/context/policy'
import { chatContextFor, disposeChatContext } from './chatContexts'

afterEach(() => { disposeChatContext('chat-a'); disposeChatContext('chat-b') })

it('makes one context per chat, and none is another chat\'s', async () => {
  const a = chatContextFor('chat-a')
  expect(chatContextFor('chat-a')).toBe(a)
  expect(chatContextFor('chat-b')).not.toBe(a)
  // What one chat retained is not among the other\'s entries.
  const turn = a.openTurn()
  const kept = await new ContextLedger(CONTEXT_DEFAULTS, turn).retain({ id: 'note', kind: 'history', label: 'A note', value: 'Private to chat A.' })
  turn.release()
  expect(a.entries.has(kept.id)).toBe(true)
  expect(chatContextFor('chat-b').entries.has(kept.id)).toBe(false)
})

it('clears what a deleted chat held and refuses a run on it, and the same id starts on a fresh context', async () => {
  const a = chatContextFor('chat-a')
  const turn = a.openTurn()
  await new ContextLedger(CONTEXT_DEFAULTS, turn).retain({ id: 'note', kind: 'history', label: 'A note', value: 'Kept.' })
  turn.release()
  expect(a.entries.size).toBe(1)
  disposeChatContext('chat-a')
  expect(a.isDisposed).toBe(true)
  expect(a.entries.size).toBe(0)
  expect(() => a.openTurn()).toThrow('disposed')
  const fresh = chatContextFor('chat-a')
  expect(fresh).not.toBe(a)
  expect(fresh.entries.size).toBe(0)
})
