import type { AssistantSession, Engine } from '../engine'
import { RunCancelled, withAbort } from '../engines/contract'
import type { AuthoringCheckpoint, AuthorTurn } from './run'

/** Bridge the existing certified engine into one authoring iteration. */
export function engineTurn(engine: Engine, session: Omit<AssistantSession, 'signal'>) {
  return async (context: AuthoringCheckpoint, signal: AbortSignal): Promise<AuthorTurn> => {
    if (signal.aborted) throw new RunCancelled()
    const revision = context.revisions.at(-1)
    const prompt = `${session.prompt}\n\nAUTHORING ITERATION\n${JSON.stringify({
      brief: context.brief,
      candidate: revision?.document ?? null,
      feedback: revision?.check ?? null,
      expectations: context.cases,
      recentChanges: context.revisions.slice(-3).map(item => item.summary)
    })}\nPropose a corrected candidate. Preserve policy intent and established expectations.`
    let candidate: AuthorTurn | undefined
    const stream = engine.start({ ...session, prompt, signal })[Symbol.asyncIterator]()
    try {
      for (;;) {
        const next = await withAbort(() => stream.next(), signal)
        if (next.done) break
        const event = next.value
        if (event.type === 'error') throw new Error(event.message)
        if (event.type === 'proposal') {
          candidate = event.unknowns.length ? { type: 'question', text: event.unknowns.join('\n') } :
            { type: 'candidate', document: JSON.stringify(event.document, null, 2), summary: `Candidate ${context.revisions.length + 1}` }
        }
      }
    } finally {
      // Request cleanup even after cancellation, but do not let an iterator's
      // return queue behind an unfinished next() and keep Stop waiting forever.
      const closing = stream.return?.()
      if (closing) {
        if (signal.aborted) void closing.catch(() => {})
        else await withAbort(() => closing, signal)
      }
    }
    if (!candidate) throw new Error('The engine ended without a candidate or a policy question.')
    return candidate
  }
}
