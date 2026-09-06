import { withAbort } from '../../contract'

/**
 * A hand-rolled SSE reader. No dependency: `fetch` plus `TextDecoderStream` is
 * enough, and both are in every browser this desk targets.
 *
 * This is one of the pieces a framework would otherwise own — thirty lines
 * here, and each provider's event grammar on top of it. Ported unchanged from
 * the bake-off's `none` prototype.
 */
export async function* sseEvents(
  response: Response,
  /** The run's signal: a stream that stalls must not outlive its run. */
  signal: AbortSignal
): AsyncGenerator<string> {
  if (!response.body) throw new Error('the model answer carried no body')
  const reader = response.body.pipeThrough(new TextDecoderStream()).getReader()
  let buffer = ''
  for (;;) {
    const { done, value } = await withAbort(() => reader.read(), signal)
    if (done) break
    buffer += value
    let newline: number
    while ((newline = buffer.indexOf('\n')) >= 0) {
      const line = buffer.slice(0, newline).replace(/\r$/, '')
      buffer = buffer.slice(newline + 1)
      if (line.startsWith('data:')) yield line.slice(5).trim()
    }
  }
  const tail = buffer.trim()
  if (tail.startsWith('data:')) yield tail.slice(5).trim()
}

// **Reading the answer rather than the request is the contract's rule, not this
// provider's**, and both engines keep it, so it lives in `engines/contract.ts`
// and is re-exported here for the two providers that read a body.
export { isEventStream } from '../../contract'
