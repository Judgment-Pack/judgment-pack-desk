/**
 * Where a new pack's first bytes come from: the runtime, or nowhere.
 *
 * **The desk ships no template.** A desk-authored skeleton would be the desk
 * asserting what a pack is, which is exactly the opinion `files.go` disclaims
 * and the runtime is the only thing entitled to hold. So the choices are the
 * runtime's own examples, the runtime's own schema, or an empty file — and
 * where the runtime advertises neither tool the dialog says so in one line
 * rather than filling the gap.
 *
 * Every hook is disabled unless its capability flag is true, so against a
 * runtime without the tools nothing is asked and nothing is claimed.
 */
import type { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { useMcp } from './McpProvider'

/** One example, as `list_examples` reports it. */
export interface ExampleSummary {
  name: string
  focus?: string
  specSection?: string
}

export interface ExampleListing {
  status?: string
  specVersion?: string
  examples?: ExampleSummary[]
}

/**
 * A tool call the runtime refused.
 *
 * `reported` is the whole point: an in-band refusal may or may not carry a
 * sentence, and the two are not the same fact. Where it carries one, that
 * sentence is the runtime's and is shown. Where it does not, this class has to
 * say *something* — and a caller must be able to tell that the something is
 * this file's filler rather than an answer, so it can say less instead of
 * putting a tool name in front of somebody creating a pack.
 */
export class RuntimeRefusal extends Error {
  readonly reported: boolean

  constructor(tool: string, text: string) {
    super(text || `the ${tool} call was refused, with no reason given`)
    this.name = 'RuntimeRefusal'
    this.reported = text !== ''
  }
}

/**
 * One tool call's text half.
 *
 * A local reader rather than a shared one: `queries.ts` keeps its own, and
 * every other file under `src/mcp/` stays byte-identical through this line of
 * work. A refusal the runtime reported in band is raised with the runtime's
 * own message, never a sentence invented here.
 */
async function callText(
  client: Client,
  name: string,
  args: Record<string, unknown>,
  signal?: AbortSignal
): Promise<string> {
  const result = await client.callTool({ name, arguments: args }, undefined, { signal })
  const content = Array.isArray(result.content) ? result.content : []
  const text = content
    .filter(
      (block): block is { type: 'text'; text: string } =>
        (block as { type?: string })?.type === 'text'
    )
    .map((block) => block.text)
    .join('')
  if (result.isError) throw new RuntimeRefusal(name, text)
  return text
}

/** The examples this runtime carries, in the runtime's own order. */
export function useExampleListing(): UseQueryResult<ExampleListing, Error> {
  const { client, status, exampleSupported } = useMcp()
  return useQuery({
    queryKey: ['list_examples'],
    enabled: status === 'ready' && client !== null && exampleSupported,
    queryFn: async ({ signal }) =>
      JSON.parse(await callText(client!, 'list_examples', {}, signal)) as ExampleListing
  })
}

/** One example's bytes, by the name `list_examples` reported. */
export function useExample(name: string | undefined): UseQueryResult<string, Error> {
  const { client, status, exampleSupported } = useMcp()
  return useQuery({
    queryKey: ['get_example', name ?? null],
    enabled: status === 'ready' && client !== null && exampleSupported && Boolean(name),
    queryFn: ({ signal }) => callText(client!, 'get_example', { name }, signal)
  })
}

/** The runtime's JPS schema. A reference to author against, not a pack. */
export function useSchema(enabled: boolean): UseQueryResult<string, Error> {
  const { client, status, schemaSupported } = useMcp()
  return useQuery({
    queryKey: ['get_schema'],
    enabled: enabled && status === 'ready' && client !== null && schemaSupported,
    queryFn: ({ signal }) => callText(client!, 'get_schema', {}, signal)
  })
}
