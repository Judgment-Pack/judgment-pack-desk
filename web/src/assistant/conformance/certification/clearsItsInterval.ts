/**
 * A certification fixture that keeps **one** of the two rules and breaks the
 * other.
 *
 * The interval rule is "a certified engine leaves no live interval when its
 * iterator ends", and a rule that fails everything proves as little as one that
 * fails nothing. This engine starts an interval and clears it — so the interval
 * rule has nothing to say about it — and still reaches the network from a
 * timeout, which the drain catches.
 *
 * It also cancels a timer it scheduled, which is the other half of the same
 * point: a handle the engine itself cleared must never be run on its behalf. A
 * drain that fired cancelled handles would report a reach this engine did not
 * make.
 *
 * It is not in `CERTIFIED_ENGINES` and never will be.
 */
import type { AssistantEvent, AssistantSession, Engine } from '../../engine'

function reach(from: string): void {
  try {
    void globalThis.fetch(`/anything?from=${from}`)
  } catch {
    /* nobody was going to see this anyway */
  }
}

export const clearsItsInterval: Engine = {
  id: 'builtin',
  async *start(_session: AssistantSession): AsyncGenerator<AssistantEvent> {
    // Started and cleared: nothing is left ticking.
    const ticking = setInterval(() => reach('interval'), 50)
    clearInterval(ticking)
    // Scheduled and cancelled: this reach must never be attributed to it.
    const abandoned = setTimeout(() => reach('cancelled'), 30)
    clearTimeout(abandoned)
    // And one it does mean, which is what fails the leg.
    setTimeout(() => reach('kept'), 40)
    yield { type: 'end' }
  }
}

export default clearsItsInterval
