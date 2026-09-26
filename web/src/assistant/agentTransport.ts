import { chassisUrl } from '../files/client'
import { socketProtocols } from '../mcp/McpProvider'
import { sessionBearer, whenSessionEnds } from '../mcp/session'
import { RunCancelled } from './engines/contract'
import type { AgentEvent, AgentRun } from './agent'

const object = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
const id = (v: unknown): v is string => typeof v === 'string' && /^[a-f0-9]{48}$/.test(v)
const failure = () => new Error('The Codex connection ended before the run completed')

/** Bound outside the engine, just like bindModelCall. Captures one constructor,
 * one selected model and a fixed route; never retries a run or changes engines. */
export function bindAgentRun(model: string): AgentRun {
  const Socket = globalThis.WebSocket
  const url = new URL(chassisUrl('/api/agent/run'), globalThis.location.href)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  return async (request, callbacks) => {
    if (callbacks.signal.aborted) throw new RunCancelled()
    const token = await sessionBearer()
    if (callbacks.signal.aborted) throw new RunCancelled()
    return new Promise<void>((resolve, reject) => {
      const ws = new Socket(url, socketProtocols(token))
      const controller = new AbortController()
      let settled = false
      let runID = ''
      let queued = 0
      let received = 0
      let endQueued = false
      let tail = Promise.resolve()
      const seenCalls = new Set<string>()
      const tools = new Set(request.tools.map(tool => tool.name))
      let stopWatching = () => {}
      const finish = (error?: Error) => {
        if (settled) return
        settled = true
        controller.abort()
        callbacks.signal.removeEventListener('abort', aborted)
        stopWatching()
        clearTimeout(deadline)
        ws.close()
        if (error) reject(error)
        else resolve()
      }
      const aborted = () => finish(new RunCancelled())
      const deadline = setTimeout(() => finish(new Error('The Codex run reached its time limit')), 12 * 60_000)
      const send = (body: unknown) => {
        if (settled || controller.signal.aborted) throw new RunCancelled()
        const text = JSON.stringify(body)
        if (text.length > 2 * 1024 * 1024 || ws.bufferedAmount > 2 * 1024 * 1024) throw failure()
        ws.send(text)
      }
      const handle = async (value: unknown) => {
        if (settled) return
        if (!object(value)) throw failure()
        if (value.type === 'started') {
          if (runID || !id(value.runId)) throw failure()
          runID = value.runId
          return
        }
        if (!runID || value.runId !== runID) throw failure()
        if (value.type === 'event') {
          const event = value.event
          if (!object(event) || !id(event.id) || typeof event.text !== 'string' ||
              !['text', 'message'].includes(String(event.type)) ||
              (event.phase !== undefined && event.phase !== 'commentary' && event.phase !== 'final')) throw failure()
          await callbacks.event({ type: event.type, id: event.id, text: event.text, phase: event.phase } as AgentEvent)
        } else if (value.type === 'tool-call') {
          const call = value.call
          if (!object(call) || !id(call.id) || seenCalls.has(call.id) || seenCalls.size >= 20 ||
              typeof call.name !== 'string' || !tools.has(call.name) || !object(call.arguments)) throw failure()
          seenCalls.add(call.id)
          const answer = await callbacks.tool({ id: call.id, name: call.name, arguments: call.arguments }, controller.signal)
          if (settled) return
          send({ type: 'tool-result', runId: runID, callId: call.id, answer })
        } else if (value.type === 'end') {
          if (typeof value.error !== 'string') throw failure()
          const messages: Record<string, string> = {
            busy: 'Another Codex operation is active',
            'sign-in-required': 'Connect a ChatGPT account before running Codex',
            limit: 'The Codex run reached its limit',
            unavailable: 'Codex is unavailable',
            'run-failed': 'Codex could not complete this run'
          }
          if (value.error === '') finish()
          else finish(new Error(messages[value.error] ?? 'Codex could not complete this run'))
        } else throw failure()
      }
      callbacks.signal.addEventListener('abort', aborted, { once: true })
      stopWatching = whenSessionEnds(aborted)
      if (callbacks.signal.aborted) { aborted(); return }
      ws.onopen = () => {
        try { send({ type: 'start', request: { ...request, model } }) }
        catch { finish(failure()) }
      }
      ws.onmessage = event => {
        if (settled) return
        if (typeof event.data !== 'string' || event.data.length > 2 * 1024 * 1024 ||
            (received += event.data.length) > 8 * 1024 * 1024 || ++queued > 64) { finish(failure()); return }
        let parsed: unknown
        try { parsed = JSON.parse(event.data) } catch { finish(failure()); return }
        if (object(parsed) && parsed.type === 'end') endQueued = true
        tail = tail.then(async () => { queued--; await handle(parsed) })
          .catch(() => finish(failure()))
      }
      ws.onerror = () => finish(failure())
      // A terminal frame can already be queued when the server closes.
      ws.onclose = () => {
        controller.abort()
        if (!endQueued) finish(failure())
        else void tail.then(() => { if (!settled) finish(failure()) })
      }
    })
  }
}
