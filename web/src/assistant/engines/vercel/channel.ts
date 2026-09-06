/**
 * One ordered channel between a framework's callbacks and the contract's
 * iterator.
 *
 * The built-in engine is an async generator all the way down, so `yield` is the
 * only ordering there is. A framework loop is not: `streamText` calls a tool's
 * `execute` from inside its own pipeline, and an adapter cannot `yield` from
 * there. What it can do is put the event on this channel and **wait until the
 * consumer has taken it**, which is the property the whole event order rests on:
 *
 *   `tool_call` delivered → `session.callTool` → *the desk's gate reports here*
 *   → `tool_result` delivered
 *
 * The guardrail event is the **desk's** and travels on the desk's own sink, not
 * on this channel (`assistant/session.ts` puts it there). So it lands between
 * the two only if the `tool_call` has already reached the consumer when the
 * gate fires — a queue that merely buffers would put the refusal *before* the
 * call it refused, which is the order a reader cannot make sense of.
 *
 * `push` therefore resolves after the consumer's `for await` body has run for
 * that event, and not before. There is no deadlock in that: the consumer pulls
 * from `drain()` while the framework's pipeline is driven from a separate task,
 * so a blocked `execute` is backpressure and not a cycle.
 *
 * **`abandon()` exists because a consumer may stop listening.** A pane that
 * unmounts, a `break`, a `return()` on the iterator: whatever is waiting on a
 * delivery that will never happen must be released, or the framework's task
 * runs for ever behind a page nobody is looking at.
 */
import type { AssistantEvent } from '../../engine'

interface Waiting {
  event: AssistantEvent
  delivered: () => void
}

export interface EventChannel {
  /** Put one event on the channel; resolves once the consumer has taken it. */
  push(event: AssistantEvent): Promise<void>
  /** No more events. `drain` returns once what is queued has been delivered. */
  close(): void
  /** Nobody is listening any more: release every pending delivery at once. */
  abandon(): void
  drain(): AsyncGenerator<AssistantEvent>
}

export function eventChannel(): EventChannel {
  const queue: Waiting[] = []
  let wake: (() => void) | null = null
  let closed = false
  let abandoned = false

  const stir = () => {
    const wakeNow = wake
    wake = null
    wakeNow?.()
  }

  return {
    push(event: AssistantEvent): Promise<void> {
      if (abandoned || closed) return Promise.resolve()
      return new Promise<void>((resolve) => {
        queue.push({ event, delivered: resolve })
        stir()
      })
    },
    close(): void {
      closed = true
      stir()
    },
    abandon(): void {
      abandoned = true
      closed = true
      // Whatever was queued is never delivered, so nothing may still be waiting
      // on a delivery: each pending push is released where it stands.
      while (queue.length > 0) queue.shift()!.delivered()
      stir()
    },
    async *drain(): AsyncGenerator<AssistantEvent> {
      for (;;) {
        while (queue.length > 0) {
          if (abandoned) return
          const next = queue.shift()!
          yield next.event
          // After the consumer's own body has run for this event, which is what
          // makes the desk's interleaved guardrail land in the right place.
          next.delivered()
        }
        if (closed) return
        await new Promise<void>((resolve) => {
          wake = resolve
        })
      }
    }
  }
}
