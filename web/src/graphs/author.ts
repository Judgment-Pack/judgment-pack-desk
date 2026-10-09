import type { HostTool } from '../assistant/engine'
import { deskFetch } from '../files/client'
import { msg } from '../i18n'

export interface GraphProposal {
  id: string
  path: string
  content: string
  description?: string
  baseSha256?: string
}
export interface GraphOffer extends GraphProposal {
  before: string
  configContent: string
  configSha256: string
  findings: string
  plan: string
  hasLock: boolean
  nonce: string
  token: string
}
export async function graphPost<T>(route: string, body: unknown, signal?: AbortSignal): Promise<T> {
  const response = await deskFetch(`/api/graphs/${route}`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal
  })
  const answer = await response.json() as T & { error?: string }
  if (!response.ok) throw new Error(answer.error ?? msg('The graph request could not complete.'))
  return answer
}

/** These are the only host capabilities offered to the graph author. */
export function graphHostTools(): HostTool[] {
  return (['validate', 'explain'] as const).map(command => ({
    name: `graph_${command}`,
    description: command === 'validate'
      ? 'Run experimental graph validate - over the exact content on standard input. Writes nothing.'
      : 'Run experimental graph explain - over the exact content on standard input. Writes nothing.',
    inputSchema: { type: 'object', properties: { content: { type: 'string' } }, required: ['content'], additionalProperties: false },
    execute: async (args, signal) => {
      if (typeof args.content !== 'string') throw new Error(msg('A graph document is required.'))
      const response = await deskFetch(`/api/graphs/${command}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content: args.content }), signal
      })
      // Keep the runtime's bytes rather than parsing and re-encoding its answer.
      const text = await response.text()
      return { isError: !response.ok, content: [{ type: 'text', text }] }
    }
  }))
}
