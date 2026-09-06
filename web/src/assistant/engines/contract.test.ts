/**
 * The contract's own vocabulary, held where both engines read it.
 *
 * Most of what is here is the iterator every engine is wrapped in, because it
 * is the piece with a property neither engine's suite can state on its own: an
 * async generator serves `next()`, `return()` and `throw()` from **one queue**,
 * so a `return()` arriving while a `next()` is pending is not run until that
 * `next()` settles. A run waiting on a model request that ends only when it is
 * aborted could therefore never be stopped by the consumer that owned it — the
 * abort was inside the `return()` that was queued behind the very `next()` it
 * would have released. Both hung for ever, on both engines, measured.
 */
import { describe, expect, it } from 'vitest'
import { eventIterator, servedSchema } from './contract'
import type { AssistantEvent } from '../engine'

/** A promise, or a marker where it did not settle inside the bound. */
async function within(ms: number, work: Promise<unknown>): Promise<string> {
  return Promise.race([
    work.then(
      () => 'settled',
      () => 'settled'
    ),
    new Promise<string>((resolve) => setTimeout(() => resolve('STILL WAITING'), ms))
  ])
}

/**
 * A generator that waits, exactly as a run waiting on a model request does, and
 * ends only when something outside it says so.
 */
function waiting(): {
  open: () => AsyncGenerator<AssistantEvent>
  release: () => void
  opened: () => number
  finallyRan: () => number
} {
  let unblock = () => {}
  let released = false
  let opens = 0
  let ran = 0
  const open = async function* (): AsyncGenerator<AssistantEvent> {
    opens += 1
    try {
      // A latch, so a release that arrives before the run started still lets it
      // through — the release is the thing outside deciding it may finish.
      if (!released) {
        await new Promise<void>((resolve) => {
          unblock = resolve
        })
      }
      yield { type: 'end' }
    } finally {
      ran += 1
    }
  }
  return {
    open,
    release: () => {
      released = true
      unblock()
    },
    opened: () => opens,
    finallyRan: () => ran
  }
}

describe('the iterator every engine is wrapped in', () => {
  it('cancels before it waits, so a pending next settles and then the return does', async () => {
    // The exact sequence: `next()` is in flight and waiting; `return()` arrives.
    // Cancelling has to happen **before** anything is awaited, or the queued
    // `return()` never runs and neither promise ever settles.
    const source = waiting()
    let cancelled = 0
    const iterator = eventIterator({
      open: source.open,
      cancel: () => {
        cancelled += 1
        source.release()
      }
    })
    const settledInOrder: string[] = []
    const pending = iterator.next().then(() => settledInOrder.push('next'))
    // Give the generator a turn to reach its wait.
    await Promise.resolve()
    const returned = iterator.return!().then(() => settledInOrder.push('return'))

    expect(await within(1000, pending), 'the pending next').toBe('settled')
    expect(await within(1000, returned), 'the return').toBe('settled')
    // The pending `next()` settles first. It is waiting on what the cancel
    // released, and the `return()` waits on the generator closing behind it.
    expect(settledInOrder).toEqual(['next', 'return'])
    expect(cancelled, 'cancelled exactly once').toBe(1)
    expect(source.finallyRan(), 'the generator was closed exactly once').toBe(1)
  })

  it('answers a pending next with done rather than an event', async () => {
    const source = waiting()
    const iterator = eventIterator({ open: source.open, cancel: source.release })
    const pending = iterator.next()
    await Promise.resolve()
    void iterator.return!()
    expect(await pending).toEqual({ value: undefined, done: true })
  })

  it('cancels once however many times it is asked to', async () => {
    const source = waiting()
    let cancelled = 0
    const iterator = eventIterator({
      open: source.open,
      cancel: () => {
        cancelled += 1
        source.release()
      }
    })
    void iterator.next()
    await Promise.resolve()
    expect(await iterator.return!()).toEqual({ value: undefined, done: true })
    expect(await iterator.return!()).toEqual({ value: undefined, done: true })
    expect(cancelled).toBe(1)
    expect(source.finallyRan()).toBe(1)
  })

  it('is done after a return, without asking what is underneath', async () => {
    const source = waiting()
    const iterator = eventIterator({ open: source.open, cancel: source.release })
    void iterator.next()
    await Promise.resolve()
    await iterator.return!()
    const opened = source.opened()
    expect(await iterator.next()).toEqual({ value: undefined, done: true })
    expect(await iterator.next()).toEqual({ value: undefined, done: true })
    expect(source.opened(), 'nothing underneath was touched again').toBe(opened)
  })

  it('opens nothing at all where the consumer leaves before its first next', async () => {
    const source = waiting()
    let cancelled = 0
    const iterator = eventIterator({
      open: source.open,
      cancel: () => {
        cancelled += 1
      }
    })
    expect(await within(1000, iterator.return!() as Promise<unknown>)).toBe('settled')
    expect(source.opened(), 'the run was never started').toBe(0)
    expect(cancelled).toBe(1)
  })

  it('cancels and rethrows on throw, including while a next is pending', async () => {
    const source = waiting()
    let cancelled = 0
    const iterator = eventIterator({
      open: source.open,
      cancel: () => {
        cancelled += 1
        source.release()
      }
    })
    const pending = iterator.next()
    await Promise.resolve()
    const thrown = iterator.throw!(new Error('the consumer gave up'))
    expect(await within(1000, thrown.then(() => undefined, () => undefined))).toBe('settled')
    await expect(thrown).rejects.toThrow('the consumer gave up')
    expect(await pending).toEqual({ value: undefined, done: true })
    expect(cancelled).toBe(1)
    expect(source.finallyRan()).toBe(1)
  })

  it('reports done and closes once when the run ends by itself', async () => {
    const source = waiting()
    let cancelled = 0
    const iterator = eventIterator({
      open: source.open,
      cancel: () => {
        cancelled += 1
      }
    })
    const pending = iterator.next()
    source.release()
    expect(await pending).toEqual({ value: { type: 'end' }, done: false })
    expect(await iterator.next()).toEqual({ value: undefined, done: true })
    expect(cancelled, 'the run cleans up on the ordinary path too').toBe(1)
    expect(source.finallyRan()).toBe(1)
  })

  it('is its own iterable, so `for await` drives it', async () => {
    const source = waiting()
    const iterator = eventIterator({ open: source.open, cancel: source.release })
    expect(iterator[Symbol.asyncIterator]()).toBe(iterator)
    source.release()
    const seen: AssistantEvent[] = []
    for await (const event of iterator) seen.push(event)
    expect(seen).toEqual([{ type: 'end' }])
  })
})

describe('the schema an engine may show the model', () => {
  it('is the served one, and a refusal where the runtime served none', () => {
    expect(servedSchema({ name: 'validate', inputSchema: { type: 'object' } })).toEqual({
      type: 'object'
    })
    expect(() => servedSchema({ name: 'a_new_tool' })).toThrow('a_new_tool')
    expect(() => servedSchema({ name: 'a_new_tool' })).toThrow('without an input schema')
  })
})
