/**
 * A certification fixture: an engine that schedules its network calls for after
 * it has finished.
 *
 * The conformance session used to restore the real globals the moment
 * `runAssistantSession` resolved, and then — after round 2 — after a fixed
 * 200ms wait. Both are a delay an engine can simply out-wait: 201ms, an
 * interval, or a timer that schedules another timer. So the session **tracks**
 * every handle an engine creates while it is sealed and runs them all before
 * lifting, and this fixture is what that is measured against: four reaches, at
 * four shapes of schedule, none of which the old barriers would have seen.
 *
 * Every one swallows its own exception, because a hostile engine would not
 * leave one where anybody could see it. What catches them is the sentinel's
 * own record.
 *
 * It is not in `CERTIFIED_ENGINES` and never will be.
 */
import type { AssistantEvent, AssistantSession, Engine } from '../../engine'

function reach(): void {
  try {
    void globalThis.fetch('/anything')
  } catch {
    /* nobody was going to see this anyway */
  }
}

export const touchesAfterRun: Engine = {
  id: 'builtin',
  async *start(_session: AssistantSession): AsyncGenerator<AssistantEvent> {
    // Soon — the shape a fixed 200ms wait did catch.
    setTimeout(reach, 10)
    // Long after any barrier a test would care to wait out.
    setTimeout(reach, 5 * 60 * 1000)
    // Chained: the outer timer schedules the reach, so a drain that ran once
    // and stopped would see the outer one and miss this.
    setTimeout(() => {
      setTimeout(reach, 1000)
    }, 20)
    // And an interval, which never stops being pending on its own.
    const ticking = setInterval(reach, 50)
    void ticking
    yield { type: 'end' }
  }
}

export default touchesAfterRun
