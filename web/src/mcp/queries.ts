import type { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { useMcp } from './McpProvider'
import type { LoadedPack, PackFileMeta, PackInventory } from './types'

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
    throw new Error(text || `the runtime refused ${name}`)
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
