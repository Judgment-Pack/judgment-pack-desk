/**
 * When holding unwritten operand text counts as an edit — measured on the
 * counter it moves.
 *
 * The two hooks are driven together, because the claim is about the number they
 * share: the revision has to be a count of *edits*, and the operand's own
 * gesture is release-then-write. Counting both halves would spend two revisions
 * on one action and make the bound — no ticket collides within 2^53 edits of a
 * page session — a bound on something else.
 */
import { act, renderHook } from '@testing-library/react'
import { useCallback, useRef } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { FileContent } from '../../files/client'
import { heldChanged, useHeldText } from './heldText'
import { useDocumentBuffer } from './useDocumentBuffer'

const file = (content: string): FileContent => ({
  path: 'packs/x.pack.json',
  bytes: content.length,
  sha256: 'a'.repeat(64),
  content
})

const draft = (text: string) => ({ text, from: '"5000"', owner: '{ "id": "the-rule" }' })

/** The route's own pair: the buffer, and the text held beside it. */
function useBoth() {
  const touch = useRef<() => void>(() => {})
  const held = useHeldText(useCallback(() => touch.current(), []))
  const buffer = useDocumentBuffer(file('{"a": 1}'))
  touch.current = buffer.touch
  return { held, buffer }
}

describe('the revision counts edits, and each edit once', () => {
  it('moves once for text created, once for text changed, and not for a repeat', () => {
    const { result } = renderHook(() => useBoth())
    expect(result.current.buffer.identity!.revision).toBe(0)

    act(() => result.current.held.hold('/rules/0/when/value', draft('{"sha')))
    expect(result.current.buffer.identity!.revision).toBe(1)

    act(() => result.current.held.hold('/rules/0/when/value', draft('{"shade"')))
    expect(result.current.buffer.identity!.revision).toBe(2)

    // A control re-reporting what it already holds has told the desk nothing.
    act(() => result.current.held.hold('/rules/0/when/value', draft('{"shade"')))
    expect(result.current.buffer.identity!.revision).toBe(2)
  })

  it('moves once — not twice — when the text becomes valid and is written', () => {
    // The operand's own gesture: release the draft, then write the bytes.
    const { result } = renderHook(() => useBoth())
    act(() => result.current.held.hold('/rules/0/when/value', draft('{"shade')))
    expect(result.current.buffer.identity!.revision).toBe(1)

    act(() => {
      result.current.held.hold('/rules/0/when/value', null)
      result.current.buffer.commit('{"a": 2}')
    })
    expect(result.current.buffer.identity!.revision).toBe(2)
    expect(result.current.held.drafts.size).toBe(0)
  })

  it('does not move for a release that releases nothing', () => {
    // Which is every valid operand edit made without a draft in the first
    // place: the control calls `hold(at, null)` on its way to writing.
    const { result } = renderHook(() => useBoth())
    act(() => result.current.held.hold('/rules/0/when/value', null))
    expect(result.current.buffer.identity!.revision).toBe(0)
    act(() => result.current.buffer.commit('{"a": 2}'))
    expect(result.current.buffer.identity!.revision).toBe(1)
  })

  it('keeps the map itself unchanged where nothing was released', () => {
    const { result } = renderHook(() => useHeldText(() => {}))
    const before = result.current.drafts
    act(() => result.current.hold('/rules/0/when/value', null))
    expect(result.current.drafts).toBe(before)
  })
})

describe('which holds are edits', () => {
  it('is one rule, and a release is never one', () => {
    const held = draft('{"shade"')
    expect(heldChanged(undefined, held)).toBe(true)
    expect(heldChanged(held, held)).toBe(false)
    expect(heldChanged(held, draft('{"shade":'))).toBe(true)
    expect(heldChanged(held, null)).toBe(false)
    expect(heldChanged(undefined, null)).toBe(false)
    // The bytes a draft started from and the card it sits in retire it, so a
    // draft that reads the same about different bytes is a different draft.
    expect(heldChanged(held, { ...held, from: '"6000"' })).toBe(true)
    expect(heldChanged(held, { ...held, owner: '{ "id": "another" }' })).toBe(true)
  })
})

describe('holding and letting go', () => {
  it('keeps what is held, by pointer, and forgets all of it on demand', () => {
    const touch = vi.fn()
    const { result } = renderHook(() => useHeldText(touch))
    act(() => result.current.hold('/a', draft('one')))
    act(() => result.current.hold('/b', draft('two')))
    expect([...result.current.drafts.keys()]).toEqual(['/a', '/b'])
    expect(touch).toHaveBeenCalledTimes(2)

    act(() => result.current.hold('/a', null))
    expect([...result.current.drafts.keys()]).toEqual(['/b'])
    expect(touch).toHaveBeenCalledTimes(2)

    act(() => result.current.forget())
    expect(result.current.drafts.size).toBe(0)
    expect(touch).toHaveBeenCalledTimes(2)
  })

  it('lets the retirement pass replace the map without calling it an edit', () => {
    // Retirement is the *consequence* of an edit — the bytes moved — and the
    // edit that moved them has counted itself already.
    const touch = vi.fn()
    const { result } = renderHook(() => useHeldText(touch))
    act(() => result.current.hold('/a', draft('one')))
    act(() => result.current.update(() => new Map()))
    expect(result.current.drafts.size).toBe(0)
    expect(touch).toHaveBeenCalledTimes(1)
  })
})
