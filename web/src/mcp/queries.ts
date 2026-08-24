import type { Client } from '@modelcontextprotocol/sdk/client/index.js'
import {
  useMutation,
  useQuery,
  type UseMutationResult,
  type UseQueryResult
} from '@tanstack/react-query'
import { useMcp } from './McpProvider'
import type {
  Evaluation,
  EvaluationRun,
  LoadedPack,
  PackFileMeta,
  PackInventory,
  RefusalEnvelope
} from './types'

/**
 * A refusal the runtime reported in band: `isError`, with the message as text
 * and, on the evaluation surface, the JPS §8.4 envelope as structuredContent.
 * Both are kept, so a view can show the machine-readable class and phase
 * instead of parsing them back out of the prose.
 */
export class ToolRefusal extends Error {
  readonly envelope: RefusalEnvelope | undefined

  constructor(message: string, envelope: RefusalEnvelope | undefined) {
    super(message)
    this.name = 'ToolRefusal'
    this.envelope = envelope
  }
}

/**
 * Call a runtime tool and parse the JSON it returns.
 *
 * Every jpack tool answers with one text content block holding JSON, and
 * reports refusals in band via isError rather than as a protocol error. Both
 * facts are handled here so no view has to know them.
 */
async function callToolJSON<T>(
  client: Client,
  name: string,
  args: Record<string, unknown> = {}
): Promise<{ parsed: T; raw: string; structured: Record<string, unknown> | undefined }> {
  const result = await client.callTool({ name, arguments: args })

  const blocks = Array.isArray(result.content) ? result.content : []
  const text = blocks
    .filter((block): block is { type: 'text'; text: string } => block?.type === 'text')
    .map((block) => block.text)
    .join('')

  if (result.isError) {
    throw new ToolRefusal(
      text || `the runtime refused ${name}`,
      result.structuredContent as RefusalEnvelope | undefined
    )
  }
  if (!text) {
    throw new Error(`${name} returned no text content`)
  }

  let parsed: T
  try {
    parsed = JSON.parse(text) as T
  } catch (cause) {
    throw new Error(`${name} returned text that is not JSON: ${String(cause)}`)
  }
  return {
    parsed,
    raw: text,
    structured: result.structuredContent as Record<string, unknown> | undefined
  }
}

/** The project's declared packs, as the runtime resolves them. */
export function usePacks(): UseQueryResult<PackInventory, Error> {
  const { client, status } = useMcp()
  return useQuery({
    queryKey: ['list_packs'],
    enabled: status === 'ready' && client !== null,
    queryFn: async () => {
      const { parsed } = await callToolJSON<PackInventory>(client!, 'list_packs')
      return parsed
    }
  })
}

/** One pack document, with the file metadata the runtime reports beside it. */
export function usePack(packId: string | undefined): UseQueryResult<LoadedPack, Error> {
  const { client, status } = useMcp()
  return useQuery({
    queryKey: ['get_pack', packId],
    enabled: status === 'ready' && client !== null && Boolean(packId),
    queryFn: async () => {
      const { parsed, raw, structured } = await callToolJSON<LoadedPack['document']>(
        client!,
        'get_pack',
        { pack_id: packId }
      )
      return { document: parsed, raw, meta: (structured ?? {}) as PackFileMeta }
    }
  })
}

/** The documents one evaluation is run over. */
export interface EvaluateInput {
  /** The project's decision id; the runtime resolves it through jpack.json. */
  packId: string
  /** One JSON facts document, as JSON text. */
  facts: string
  /**
   * Optional tri-state evidence availability, as JSON text. Undefined means the
   * key is omitted entirely — no document at all, which makes every declared
   * requirement unknown. An empty string is a *supplied* empty document, which
   * is not a JSON text and is refused as malformed-input, so this never sends
   * one: absence is expressed by omitting the key, exactly as the tool asks.
   */
  evidence?: string
}

/**
 * Run one evaluation over the relay.
 *
 * It is a mutation and not a query because it is not a read: in a project whose
 * configuration declares an audit directory, a completed call appends one
 * record to it. Nothing here is cached or replayed — a disposition is the
 * answer to the documents that were supplied, and re-running is the user's to
 * ask for.
 */
export function useEvaluate(): UseMutationResult<EvaluationRun, Error, EvaluateInput> {
  const { client } = useMcp()
  return useMutation({
    mutationFn: async (input: EvaluateInput) => {
      if (!client) throw new Error('the desk is not connected to the runtime')
      const args: Record<string, unknown> = { pack_id: input.packId, facts: input.facts }
      if (input.evidence !== undefined) args.evidence = input.evidence
      const { parsed, raw } = await callToolJSON<Evaluation>(
        client,
        'experimental_evaluate',
        args
      )
      return { payload: parsed, raw, facts: input.facts, evidence: input.evidence }
    }
  })
}
