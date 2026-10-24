// Actual server/adapter observations are temporary inputs, never golden vectors.
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { readFrames, RunObservations } from '../web/src/assistant/serverRuns/stream.ts'

const directory = process.argv[2]
assert(directory, 'expected temporary producer observation directory')
for (const engine of ['api', 'native']) for (const mode of ['off', 'redact']) {
  const raw = fs.readFileSync(path.join(directory, `progress-${engine}-${mode}.sse`), 'utf8')
  const bytes = new TextEncoder().encode(raw)
  let at = 0, observations, progress = 0, clears = 0, messages = 0, snapshots = 0
  const body = new ReadableStream({ pull(controller) {
    if (at === bytes.length) controller.close()
    else { controller.enqueue(bytes.slice(at, at + 7)); at += Math.min(7, bytes.length - at) }
  } })
  for await (const frame of readFrames(body)) {
    if (!observations) {
      assert.equal(frame.kind, 'snapshot', 'durable reconciliation must arrive first')
      const { id, owner, requestId } = frame.data
      observations = new RunObservations({ id, owner, requestId })
      observations.beginStream()
    }
    const cursor = observations.cursor
    const observation = observations.accept(frame)
    if (observation?.kind === 'progress') {
      assert.equal(observations.cursor, cursor, 'ephemeral text cannot move durable replay')
      assert.equal(messages, 0, 'prefix must precede the authoritative final message')
      assert.equal(observation.progress.taskId, undefined, 'root answer cannot be a child')
      if (observation.progress.text === '') {
        assert.equal(observations.answer, '', 'empty whole prefix clears live text')
        clears++
      } else {
        assert.match(observations.answer, /^Fixture/)
        progress++
      }
    } else if (observation?.kind === 'event' && observation.event.event.type === 'message') {
      messages++
      assert.equal(observations.answer, '', 'authoritative message reconciles live text')
    } else if (observation?.kind === 'snapshot') snapshots++
  }
  assert(observations)
  assert(snapshots >= 2, 'running and completed checkpoint observations must survive')
  assert(messages >= 1, 'completed prose must remain durable under either policy')
  assert.equal(observations.snapshot.checkpoint.status, 'complete')
  assert.equal(observations.answer, '')
  assert.equal(observations.snapshot.displayPreview, undefined, 'completed prose is not duplicated')
  if (mode === 'off') assert(progress >= 1, 'actual admitted adapter must stream before completion')
  else assert.equal(progress + clears, 0, 'default guarded mode must withhold raw prefixes')
  observations.endStream()
}
console.log('Run progress readers: actual API/native SSE passed; guarded/default policy preserved')
