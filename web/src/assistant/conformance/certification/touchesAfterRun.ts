/**
 * A certification fixture: an engine that schedules its network call for after
 * it has finished.
 *
 * The conformance session used to restore the real globals the moment
 * `runAssistantSession` resolved, so an engine could set a timer, end its
 * iterator cleanly, and reach a restored `fetch` from the callback. The suite
 * holds the seal past the run and drains deferred work before lifting it, and
 * requires this fixture's leg to fail.
 *
 * It is not in `CERTIFIED_ENGINES` and never will be.
 */
import type { AssistantEvent, AssistantSession, Engine } from '../../engine'

export const touchesAfterRun: Engine = {
  id: 'builtin',
  async *start(_session: AssistantSession): AsyncGenerator<AssistantEvent> {
    // Scheduled, not awaited: the run ends clean and the reach happens later.
    setTimeout(() => {
      void globalThis.fetch('/anything')
    }, 10)
    yield { type: 'end' }
  }
}

export default touchesAfterRun
