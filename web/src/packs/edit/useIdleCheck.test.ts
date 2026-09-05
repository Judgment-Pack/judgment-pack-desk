/**
 * The gap between the buffer and the bytes the check is about.
 */
import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useIdleCheck } from './useIdleCheck'

beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

describe('the check on idle', () => {
  it('sends the first bytes at once, because a load is not typing', () => {
    const { result } = renderHook(({ text }: { text: string }) => useIdleCheck(text), {
      initialProps: { text: '{"a": 1}' }
    })
    expect(result.current.checkedText).toBe('{"a": 1}')
    expect(result.current.behind).toBe(false)
  })

  it('waits for the buffer to sit still, and says it is behind meanwhile', () => {
    const { result, rerender } = renderHook(({ text }: { text: string }) => useIdleCheck(text), {
      initialProps: { text: '0' }
    })
    rerender({ text: '01' })
    expect(result.current.checkedText).toBe('0')
    expect(result.current.behind).toBe(true)
    act(() => void vi.advanceTimersByTime(300))
    rerender({ text: '012' })
    // Still typing: the pause restarts rather than the first keystroke going.
    act(() => void vi.advanceTimersByTime(400))
    expect(result.current.checkedText).toBe('0')
    act(() => void vi.advanceTimersByTime(300))
    expect(result.current.checkedText).toBe('012')
    expect(result.current.behind).toBe(false)
  })

  it('sends the buffer now when it is asked to', () => {
    const { result, rerender } = renderHook(({ text }: { text: string }) => useIdleCheck(text), {
      initialProps: { text: '0' }
    })
    rerender({ text: '01' })
    expect(result.current.checkedText).toBe('0')
    act(() => result.current.checkNow())
    expect(result.current.checkedText).toBe('01')
    expect(result.current.behind).toBe(false)
  })

  it('sends the bytes as they are now, not as they were when the pause began', () => {
    const { result, rerender } = renderHook(({ text }: { text: string }) => useIdleCheck(text), {
      initialProps: { text: '0' }
    })
    rerender({ text: '01' })
    rerender({ text: '012' })
    act(() => void vi.advanceTimersByTime(700))
    expect(result.current.checkedText).toBe('012')
  })

  it('is not behind before there are any bytes at all', () => {
    const { result } = renderHook(() => useIdleCheck(undefined))
    expect(result.current.checkedText).toBeUndefined()
    expect(result.current.behind).toBe(false)
  })
})
