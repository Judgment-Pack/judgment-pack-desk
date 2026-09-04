/**
 * The buffer, driven as a hook rather than through a view.
 *
 * Dirty, undo and discard are three rules about bytes, and a view test would
 * hold them through whatever the view happened to render. `renderHook` drives
 * the rules themselves.
 */
import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { FileContent } from '../../files/client'
import { UNDO_DEPTH, useDocumentBuffer } from './useDocumentBuffer'

const file = (content: string, path = 'packs/x.pack.json'): FileContent => ({
  path,
  bytes: content.length,
  sha256: 'a'.repeat(64),
  content
})

describe('dirty', () => {
  it('is a byte comparison, so a whitespace-only edit is dirty', () => {
    const loaded = file('{"a": 1}')
    const { result } = renderHook(() => useDocumentBuffer(loaded))
    expect(result.current.dirty).toBe(false)
    // Parses identically. It is still a different file, and a save writes
    // bytes — an editor that called this clean would discard it unasked.
    act(() => result.current.commit('{"a":  1}'))
    expect(result.current.dirty).toBe(true)
  })

  it('goes clean again when the bytes come back, however they got there', () => {
    const loaded = file('{"a": 1}')
    const { result } = renderHook(() => useDocumentBuffer(loaded))
    act(() => result.current.commit('{"a": 2}'))
    act(() => result.current.commit('{"a": 1}'))
    expect(result.current.dirty).toBe(false)
  })
})

describe('undo', () => {
  it('is one entry per committed action', () => {
    const { result } = renderHook(() => useDocumentBuffer(file('0')))
    act(() => result.current.commit('1'))
    act(() => result.current.commit('2'))
    act(() => result.current.commit('3'))
    act(() => result.current.undo())
    expect(result.current.text).toBe('2')
    act(() => result.current.undo())
    expect(result.current.text).toBe('1')
    act(() => result.current.undo())
    expect(result.current.text).toBe('0')
    expect(result.current.canUndo).toBe(false)
  })

  it('coalesces typing per field, and starts again at the next one', () => {
    const { result } = renderHook(() => useDocumentBuffer(file('')))
    for (const typed of ['a', 'ab', 'abc']) {
      act(() => result.current.commit(typed, { coalesceKey: '/rules/0/description' }))
    }
    act(() => result.current.commit('abc!', { coalesceKey: '/rules/0/id' }))
    // Two actions: a sentence typed into one field, and a keystroke in
    // another. Not four.
    act(() => result.current.undo())
    expect(result.current.text).toBe('abc')
    act(() => result.current.undo())
    expect(result.current.text).toBe('')
    expect(result.current.canUndo).toBe(false)
  })

  it('never coalesces an action that is not typing', () => {
    const { result } = renderHook(() => useDocumentBuffer(file('0')))
    act(() => result.current.commit('1'))
    act(() => result.current.commit('2'))
    act(() => result.current.undo())
    expect(result.current.text).toBe('1')
  })

  it('drops the oldest at the cap and says so rather than lying', () => {
    const { result } = renderHook(() => useDocumentBuffer(file('0')))
    for (let step = 1; step <= UNDO_DEPTH + 5; step += 1) {
      act(() => result.current.commit(String(step)))
    }
    for (let step = 0; step < UNDO_DEPTH; step += 1) {
      expect(result.current.canUndo).toBe(true)
      act(() => result.current.undo())
    }
    // The cap is a cap: the five oldest actions are gone, and `canUndo` is
    // false rather than offering a step that would do nothing.
    expect(result.current.canUndo).toBe(false)
    expect(result.current.text).toBe('5')
  })
})

describe('discard and rebase', () => {
  it('restores the base and clears the last attempt’s verdict', () => {
    const cleared = vi.fn()
    const { result } = renderHook(() => useDocumentBuffer(file('{"a": 1}'), cleared))
    act(() => result.current.commit('{"a": 2}'))
    act(() => result.current.discard())
    expect(result.current.text).toBe('{"a": 1}')
    expect(result.current.dirty).toBe(false)
    expect(result.current.canUndo).toBe(false)
    // A "Saved, and verified" — or an offer to overwrite — left standing over
    // a buffer that no longer differs is about bytes nobody is proposing.
    expect(cleared).toHaveBeenCalledTimes(1)
  })

  it('moves the base only where it is told to', () => {
    const loaded = file('{"a": 1}')
    const { result, rerender } = renderHook(
      ({ answer }: { answer: FileContent }) => useDocumentBuffer(answer),
      { initialProps: { answer: loaded } }
    )
    act(() => result.current.commit('{"a": 2}'))
    // A watcher refetch: a *newer answer about the same file*, arriving with
    // an unsaved buffer. The base does not move, so the save that follows is
    // refused rather than overwriting a change nobody saw.
    rerender({ answer: file('{"a": 3}') })
    expect(result.current.base?.content).toBe('{"a": 1}')
    expect(result.current.text).toBe('{"a": 2}')

    const saved = file('{"a": 2}')
    act(() => result.current.rebase(saved))
    expect(result.current.base).toBe(saved)
    expect(result.current.dirty).toBe(false)
    expect(result.current.canUndo).toBe(false)
  })

  it('takes a different file as a different document, edits and all', () => {
    // Not a refetch: another *file*. The pack route does not remount between
    // packs, so a buffer that seeded once kept the first pack's bytes — drawn
    // under the second pack's address, and sent to the second pack's path.
    const { result, rerender } = renderHook(
      ({ answer }: { answer: FileContent }) => useDocumentBuffer(answer),
      { initialProps: { answer: file('{"a": 1}') } }
    )
    act(() => result.current.commit('{"a": 2}'))
    const other = file('{"b": 9}', 'packs/other.pack.json')
    rerender({ answer: other })
    expect(result.current.text).toBe('{"b": 9}')
    expect(result.current.base).toBe(other)
    expect(result.current.dirty).toBe(false)
    // The stack was about the document that is no longer on screen; one Undo
    // would otherwise put the first pack's bytes into the second one.
    expect(result.current.canUndo).toBe(false)
  })
})
