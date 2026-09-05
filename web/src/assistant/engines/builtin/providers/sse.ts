/**
 * A hand-rolled SSE reader. No dependency: `fetch` plus `TextDecoderStream` is
 * enough, and both are in every browser this desk targets.
 *
 * This is one of the pieces a framework would otherwise own — thirty lines
 * here, and each provider's event grammar on top of it. Ported unchanged from
 * the bake-off's `none` prototype.
 */
export async function* sseEvents(response: Response): AsyncGenerator<string> {
  if (!response.body) throw new Error('the model answer carried no body')
  const reader = response.body.pipeThrough(new TextDecoderStream()).getReader()
  let buffer = ''
  for (;;) {
    const { done, value } = await reader.read()
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

/**
 * Whether an answer is a stream, read off the answer rather than off the
 * request.
 *
 * A request that asked to stream may be answered whole — a gateway that
 * buffers, an endpoint that ignores the member, an error envelope from the
 * desk's own relay — and an engine that parsed by what it *asked for* would
 * read a JSON object as an event stream and report an empty turn. So the
 * content type decides, and the non-stream path is the fallback.
 */
export function isEventStream(response: Response): boolean {
  return (response.headers.get('content-type') ?? '').toLowerCase().includes('text/event-stream')
}
