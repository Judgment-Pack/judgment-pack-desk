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

/**
 * The runtime's repair prompt, which takes the validator's own diagnostics.
 *
 * `internal/mcp/prompts.go` names the argument `diagnostics` and describes it
 * as "the diagnostics from a failed validate call". What the desk hands it is
 * exactly that — the runtime's own report, as JSON text — because a prompt
 * about a refusal is only as good as the refusal it was given.
 */
export const FIX_PACK_PROMPT = 'fix_pack'

/**
 * The runtime's testing prompt, which the assistant's critic works from.
 *
 * `internal/mcp/prompts.go` describes it as guidance for "a logic-testing
 * session: build an instance matrix for a pack you hold and probe it with the
 * experimental evaluator". That is what the refutation pass does, so the
 * instructions it runs under are the runtime's own rather than a second opinion
 * this desk wrote.
 */
export const TEST_PACK_PROMPT = 'test_pack'

interface PromptSummary {
  name: string
  description?: string
}

interface PromptLister {
  listPrompts?: () => Promise<{ prompts?: PromptSummary[] }>
  getPrompt?: (params: { name: string; arguments?: Record<string, string> }) => Promise<{
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

/**
 * One prompt's message text, verbatim.
 *
 * `args` are the prompt's own arguments, as `prompts/get` takes them — the
 * runtime fills them into the text it returns, so a prompt read with different
 * arguments is a different answer and is keyed as one. It is optional because
 * the surfaces that only *show* a prompt pass none; the assistant passes the
 * policy a person typed, which is the whole of what it adds to the runtime's
 * own words.
 */
export function usePromptText(
  name: string,
  advertised: boolean,
  args?: Record<string, string>
): UseQueryResult<PromptText, Error> {
  const { client, status } = useMcp()
  return useQuery({
    // The arguments are part of the key, because they are part of the answer.
    queryKey: ['prompts/get', name, args ?? null],
    enabled: advertised && status === 'ready' && client !== null,
    queryFn: async () => {
      const lister = client as unknown as PromptLister
      if (typeof lister.getPrompt !== 'function') {
        throw new Error('this connection cannot fetch a prompt')
      }
      const answer = await lister.getPrompt(args === undefined ? { name } : { name, arguments: args })
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
