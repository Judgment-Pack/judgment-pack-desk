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
import { FALLBACK_EVALUATOR_SPEC_VERSION } from './evaluatorVersion'

/** One example, as `list_examples` reports it. */
export interface ExampleSummary {
  name: string
  focus?: string
  specSection?: string
}

export interface ExampleListing {
  status?: string
  /** The version the listed set declares. */
  specVersion?: string
  /** The version the evaluator admits. Absent before runtime v0.24.0. */
  evaluatorSpecVersion?: string
  examples?: ExampleSummary[]
}

/**
 * The JPS version this runtime's evaluator admits, and whether it said so.
 *
 * `reported` decides how the example tools are called. A runtime that names
 * the version also takes `spec_version` on `list_examples` and `get_example`,
 * and serves a set that already declares it. One that names nothing refuses
 * the argument as an unknown member, so it is never sent there, and `version`
 * is the desk's fallback rather than the runtime's word.
 */
export interface EvaluatorVersion {
  version: string
  reported: boolean
}

const UNREPORTED: EvaluatorVersion = { version: FALLBACK_EVALUATOR_SPEC_VERSION, reported: false }

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

/**
 * One listing: the default set where `specVersion` is null, asked with no
 * arguments as every runtime with the tool accepts, or exactly the named set.
 *
 * Keyed by the connection as well as the version, because the version is read
 * off the default listing and the two answers mean something only together.
 */
function useListing(specVersion: string | null): UseQueryResult<ExampleListing, Error> {
  const { client, status, exampleSupported, connectionEpoch } = useMcp()
  return useQuery({
    queryKey: ['list_examples', connectionEpoch, specVersion],
    enabled: status === 'ready' && client !== null && exampleSupported,
    queryFn: async ({ signal }) =>
      JSON.parse(
        await callText(client!, 'list_examples', specVersion === null ? {} : { spec_version: specVersion }, signal)
      ) as ExampleListing
  })
}

/**
 * The evaluator's version, read off the default listing.
 *
 * `undefined` while that listing is still being asked, so nothing that depends
 * on the version is asked with a guess first. A runtime without the example
 * tools, one whose listing was refused, and one whose listing names no version
 * are all read as not reporting, which is how every runtime was read before
 * any could.
 */
export function useEvaluatorVersion(): EvaluatorVersion | undefined {
  const { exampleSupported } = useMcp()
  const listing = useListing(null)
  if (!exampleSupported || listing.isError) return UNREPORTED
  if (listing.isPending) return undefined
  const reported = listing.data?.evaluatorSpecVersion
  return typeof reported === 'string' && reported !== '' ? { version: reported, reported: true } : UNREPORTED
}

/**
 * The examples to offer, in the runtime's own order: the set that declares the
 * evaluator's version where the runtime names one, and the default set, asked
 * exactly as before, where it does not.
 */
export function useExampleListing(): UseQueryResult<ExampleListing, Error> {
  const evaluator = useEvaluatorVersion()
  return useListing(evaluator?.reported ? evaluator.version : null)
}

/** One example's bytes, by the name `list_examples` reported, from the same set. */
export function useExample(name: string | undefined): UseQueryResult<string, Error> {
  const { client, status, exampleSupported, connectionEpoch } = useMcp()
  const evaluator = useEvaluatorVersion()
  const specVersion = evaluator?.reported ? evaluator.version : null
  return useQuery({
    queryKey: ['get_example', connectionEpoch, specVersion, name ?? null],
    enabled: status === 'ready' && client !== null && exampleSupported && evaluator !== undefined && Boolean(name),
    queryFn: ({ signal }) =>
      callText(client!, 'get_example', specVersion === null ? { name } : { name, spec_version: specVersion }, signal)
  })
}

/**
 * The runtime's JPS schema for the evaluator's version. A reference to author
 * against, not a pack. `get_schema` has taken `spec_version` longer than the
 * example tools have, so it is sent with the fallback version too.
 */
export function useSchema(enabled: boolean): UseQueryResult<string, Error> {
  const { client, status, schemaSupported, connectionEpoch } = useMcp()
  const evaluator = useEvaluatorVersion()
  return useQuery({
    queryKey: ['get_schema', connectionEpoch, evaluator?.version ?? null],
    enabled: enabled && status === 'ready' && client !== null && schemaSupported && evaluator !== undefined,
    queryFn: ({ signal }) => callText(client!, 'get_schema', { spec_version: evaluator!.version }, signal)
  })
}
