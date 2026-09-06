/**
 * A certification fixture: an engine that schedules its network calls for after
 * it has finished, and leaves an interval running.
 *
 * The conformance session used to restore the real globals the moment
 * `runAssistantSession` resolved, then after a fixed 200ms wait, then after
 * running each interval three times and clearing it on the engine's behalf.
 * Every one of those is a delay or a bound an engine can simply outlast: 201ms,
 * a chain of timers, or a reach on the fourth tick.
 *
 * So the session **tracks** every handle an engine creates while it is sealed,
 * runs the timeouts and microtasks to exhaustion, and treats two things as
 * certification failures in their own right: a handle still pending when the
 * bound is reached, and **an interval the engine never cleared**. This fixture
 * is what that is measured against — four reaches at four shapes of schedule,
 * plus an interval left ticking.
 *
 * Every reach swallows its own exception, because a hostile engine would not
 * leave one where anybody could see it. What catches them is the sentinel's own
 * record; what catches the interval is that it is still live.
 *
 * It is not in `CERTIFIED_ENGINES` and never will be.
 */
import type { AssistantEvent, AssistantSession, Engine } from '../../engine'

/**
 * One reach, marked so the suite can say **which** schedule it came from: a
 * barrier that catches three of four is a barrier that catches none of the one
 * that matters.
 */
function reach(from: string): void {
  try {
    void globalThis.fetch(`/anything?from=${from}`)
  } catch {
    /* nobody was going to see this anyway */
  }
}

export const touchesAfterRun: Engine = {
  id: 'builtin',
  async *start(_session: AssistantSession): AsyncGenerator<AssistantEvent> {
    // Soon — the shape a fixed 200ms wait did catch.
    setTimeout(() => reach('soon'), 10)
    // Long after any barrier a test would care to wait out.
    setTimeout(() => reach('far'), 5 * 60 * 1000)
    // Chained: the outer timer schedules the reach, so a drain that ran once
    // and stopped would see the outer one and miss this.
    setTimeout(() => {
      setTimeout(() => reach('chained'), 1000)
    }, 20)
    // A promise chain with no timer in it at all, which is why the drain
    // flushes microtasks between rounds rather than only firing handles.
    void Promise.resolve()
      .then(() => Promise.resolve())
      .then(() => reach('promise'))
    // And an interval nobody clears. The reach on its fourth tick is what a
    // run-three-and-clear drain missed; the interval being **live** is what
    // fails the leg now, whatever tick it would have reached on.
    let ticks = 0
    setInterval(() => {
      ticks += 1
      if (ticks >= 4) reach('interval')
    }, 50)
    yield { type: 'end' }
  }
}

export default touchesAfterRun
