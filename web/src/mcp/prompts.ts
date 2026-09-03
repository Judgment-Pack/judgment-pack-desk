/**
 * The runtime's authoring prompts, as text.
 *
 * **The desk holds no model key, calls no model, and executes no prompt.**
 * `prompts.go` is explicit that the client's model runs these with the
 * client's key, and this desk is not that client — it renders the runtime's
 * own words for a person to carry to whatever agent they run, and stops there.
 *
 * A new file under `src/mcp/`, so every existing file in that directory stays
 * byte-identical through this line of work.
 */
import type { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { useMcp } from './McpProvider'

/** The prompt whose method guidance the authoring surfaces link to. */
export const AUTHOR_PACK_PROMPT = 'author_pack'

interface PromptSummary {
  name: string
  description?: string
}

interface PromptLister {
  listPrompts?: () => Promise<{ prompts?: PromptSummary[] }>
  getPrompt?: (params: { name: string }) => Promise<{
    description?: string
    messages?: { role?: string; content?: { type?: string; text?: string } }[]
  }>
}

/**
 * Which prompts this runtime advertises, or an empty list.
 *
 * A runtime that answers no `prompts/list` at all is one without prompts, as
 * far as anything the desk may claim goes — so the query resolves empty rather
 * than rejecting, and the page renders one honest line instead of an error
 * about a capability nobody asked for.
 */
export function usePromptNames(): UseQueryResult<string[], Error> {
  const { client, status } = useMcp()
  return useQuery({
    queryKey: ['prompts/list'],
    enabled: status === 'ready' && client !== null,
    queryFn: async () => {
      const lister = client as unknown as PromptLister
      if (typeof lister.listPrompts !== 'function') return []
      try {
        const answer = await lister.listPrompts()
        return (answer.prompts ?? []).map((prompt) => prompt.name)
      } catch {
        return []
      }
    }
  })
}

export interface PromptText {
  description?: string
  /** Every message's text, joined in the runtime's own order. */
  text: string
}

/** One prompt's message text, verbatim. */
export function usePromptText(name: string, advertised: boolean): UseQueryResult<PromptText, Error> {
  const { client, status } = useMcp()
  return useQuery({
    queryKey: ['prompts/get', name],
    enabled: advertised && status === 'ready' && client !== null,
    queryFn: async () => {
      const lister = client as unknown as PromptLister
      if (typeof lister.getPrompt !== 'function') {
        throw new Error('this connection cannot fetch a prompt')
      }
      const answer = await lister.getPrompt({ name })
      const text = (answer.messages ?? [])
        .map((message) => message.content?.text ?? '')
        .filter((part) => part !== '')
        .join('\n\n')
      return { description: answer.description, text }
    }
  })
}

/** Kept beside the hooks so the type of the client this needs is one place. */
export type PromptCapableClient = Client & PromptLister
