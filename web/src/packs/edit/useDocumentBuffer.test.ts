/**
 * The buffer, driven as a hook rather than through a view.
 *
 * Dirty, undo and discard are three rules about bytes, and a view test would
 * hold them through whatever the view happened to render. `renderHook` drives
 * the rules themselves.
 */
import { act, render, renderHook } from '@testing-library/react'
import { createElement, useLayoutEffect } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { FileContent } from '../../files/client'
import { UNDO_DEPTH, useDocumentBuffer, type DocumentBuffer } from './useDocumentBuffer'

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
    const other = file('{"b": 9}', 'packs/other.pack.json')
    rerender({ answer: other })
    expect(result.current.text).toBe('{"b": 9}')
    expect(result.current.base).toBe(other)
    expect(result.current.dirty).toBe(false)
    // The stack was about the document that is no longer on screen; one Undo
    // would otherwise put the first pack's bytes into the second one.
    expect(result.current.canUndo).toBe(false)
    expect(result.current.waiting).toBeUndefined()
  })

  it('holds a different file rather than replacing unsaved work', () => {
    // **A path can move under an address nobody navigated**: the listing
    // re-answers, `get_pack` names another file. Seeding on that replaced an
    // author's edits with another document's bytes, with nothing on screen
    // having offered to keep them.
    const { result, rerender } = renderHook(
      ({ answer }: { answer: FileContent }) => useDocumentBuffer(answer),
      { initialProps: { answer: file('{"a": 1}') } }
    )
    act(() => result.current.commit('{"a": 2}'))
    const other = file('{"b": 9}', 'packs/other.pack.json')
    rerender({ answer: other })

    expect(result.current.text).toBe('{"a": 2}')
    expect(result.current.base?.path).toBe('packs/x.pack.json')
    expect(result.current.dirty).toBe(true)
    expect(result.current.waiting).toBe(other)

    // And it is an offer, so taking it is an act.
    act(() => result.current.takeWaiting())
    expect(result.current.text).toBe('{"b": 9}')
    expect(result.current.base).toBe(other)
    expect(result.current.waiting).toBeUndefined()
    expect(result.current.canUndo).toBe(false)
  })

  it('forgets a document the route has left, and seeds again from the file', () => {
    // What the route calls when the address moves to another pack: the edits go
    // and the buffer is whatever the file query is answering with now. Coming
    // back to a pack hands the hook the *same* `FileContent` object out of the
    // cache, so this has to seed again rather than wait for an object that will
    // never change.
    const { result, rerender } = renderHook(
      ({ answer }: { answer: FileContent }) => useDocumentBuffer(answer),
      { initialProps: { answer: file('{"a": 1}') } }
    )
    act(() => result.current.commit('{"a": 2}'))
    act(() => result.current.forget())
    expect(result.current.text).toBe('{"a": 1}')
    expect(result.current.dirty).toBe(false)
    expect(result.current.canUndo).toBe(false)

    // And a buffer whose edits were forgotten holds nothing back: this is the
    // other pack arriving after a navigation the guard already asked about.
    act(() => result.current.commit('{"a": 3}'))
    act(() => result.current.forget())
    const other = file('{"b": 9}', 'packs/other.pack.json')
    rerender({ answer: other })
    expect(result.current.text).toBe('{"b": 9}')
    expect(result.current.waiting).toBeUndefined()
  })
})

describe('an offer a committed render is still showing', () => {
  it('is refused once the address it was for is not the address any more', () => {
    // **The window between a render and the effect that follows it.** `waiting`
    // is cleared in a passive effect, so React can commit a render carrying the
    // old offer *and* the new address — and the button that takes the offer is
    // on screen, and clickable, in exactly that state. A layout effect is that
    // window: it runs after the commit and before the passive effect.
    const A = file('{"a": 1}')
    const B = file('{"b": 9}', 'packs/other.pack.json')
    const latest: { current: DocumentBuffer | undefined } = { current: undefined }
    let takeInTheWindow = false

    function Probe({ answer }: { answer: FileContent }) {
      const buffer = useDocumentBuffer(answer)
      latest.current = buffer
      useLayoutEffect(() => {
        if (takeInTheWindow) buffer.takeWaiting()
      })
      return null
    }

    const view = render(createElement(Probe, { answer: A }))
    act(() => latest.current!.commit('{"a": 2}'))
    expect(latest.current!.dirty).toBe(true)

    // A different file over unsaved work is held, not taken.
    act(() => {
      view.rerender(createElement(Probe, { answer: B }))
    })
    expect(latest.current!.waiting?.path).toBe('packs/other.pack.json')
    expect(latest.current!.text).toBe('{"a": 2}')

    // The address goes back to A, and the offer is taken **in the window**:
    // after the commit that carries A, before the effect that clears B.
    takeInTheWindow = true
    act(() => {
      view.rerender(createElement(Probe, { answer: A }))
    })
    takeInTheWindow = false

    // The work in front of the viewer is still there, and the file the page is
    // about is still the file the page is about.
    expect(latest.current!.text).toBe('{"a": 2}')
    expect(latest.current!.base?.path).toBe('packs/x.pack.json')
    expect(latest.current!.waiting).toBeUndefined()
  })
})

describe('a read that resolves before the next render', () => {
  it('is refused where the buffer was put down in the same turn', () => {
    // **The generation is a ref, not only state.** `forget()` scheduled the
    // increment, so a read resolving in the same turn still saw the old number —
    // and `seeded.current` was already cleared, which took the path check out
    // with it. The forgotten reload was adopted.
    const { result } = renderHook(() => useDocumentBuffer(file('{"a": 1}')))
    act(() => result.current.commit('{"a": 2}'))
    const identity = result.current.identity
    expect(identity).toBeDefined()

    let took: boolean | undefined
    act(() => {
      result.current.forget()
      // No render between the two: this is what a promise callback does.
      took = result.current.rebase(file('{"a": 9}'), identity)
    })
    expect(took).toBe(false)
  })

  it('says so, so the caller knows whether its own state may move', () => {
    const { result } = renderHook(() => useDocumentBuffer(file('{"a": 1}')))
    const identity = result.current.identity!
    let took: boolean | undefined
    act(() => {
      took = result.current.rebase({ ...file('{"a": 9}'), path: 'packs/other.pack.json' }, identity)
    })
    expect(took).toBe(false)
    act(() => {
      took = result.current.rebase(file('{"a": 9}'), identity)
    })
    expect(took).toBe(true)
    expect(result.current.text).toBe('{"a": 9}')
  })
})

describe('what a save lands on', () => {
  it('replaces the buffer where the save carried everything', () => {
    const { result } = renderHook(() => useDocumentBuffer(file('{"a": 1}')))
    act(() => result.current.commit('{"a": 2}'))
    const landed = { ...file('{"a": 2}'), sha256: 'b'.repeat(64) }
    act(() => result.current.landed(landed, '{"a": 2}'))
    expect(result.current.base).toBe(landed)
    expect(result.current.text).toBe('{"a": 2}')
    expect(result.current.dirty).toBe(false)
    expect(result.current.canUndo).toBe(false)
  })

  it('keeps what was typed while the write was in flight', () => {
    // **The author is free to keep typing during a PUT.** Every keystroke after
    // the request is work the save did not send, and a rebase that replaced the
    // buffer with the landed bytes deleted it — silently, at the moment the
    // page said the save had succeeded.
    const { result } = renderHook(() => useDocumentBuffer(file('{"a": 1}')))
    act(() => result.current.commit('{"a": 2}'))
    const submitted = '{"a": 2}'
    act(() => result.current.commit('{"a": 3}'))
    const landed = { ...file(submitted), sha256: 'b'.repeat(64) }
    act(() => result.current.landed(landed, submitted))
    expect(result.current.base).toBe(landed)
    expect(result.current.text).toBe('{"a": 3}')
    expect(result.current.dirty).toBe(true)
    // And the way back to it is still there.
    expect(result.current.canUndo).toBe(true)
  })

  it('keeps the submitted bytes where the read-back is not them', () => {
    // The write completed and the disk does not hold what was sent. The base is
    // what landed — that is what a further save must state — and the editor
    // still holds what was sent, dirty against it.
    const { result } = renderHook(() => useDocumentBuffer(file('{"a": 1}')))
    act(() => result.current.commit('{"a": 2}'))
    const landed = { ...file('{"a": 99}'), sha256: 'c'.repeat(64) }
    act(() => result.current.landed(landed, '{"a": 2}'))
    expect(result.current.base).toBe(landed)
    expect(result.current.text).toBe('{"a": 2}')
    expect(result.current.dirty).toBe(true)
  })
})
