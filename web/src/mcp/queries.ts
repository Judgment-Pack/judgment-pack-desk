import type { Client } from '@modelcontextprotocol/sdk/client/index.js'
import {
  useMutation,
  useQuery,
  type UseMutationResult,
  type UseQueryResult
} from '@tanstack/react-query'
import { parseGraphDocument } from './graphDocument'
import { useMcp } from './McpProvider'
import type {
  Evaluation,
  EvaluationRun,
  GraphDocumentMeta,
  GraphInventory,
  GraphSuite,
  LoadedPack,
  PackFileMeta,
  PackInventory,
  PackTest,
  RefusalEnvelope,
  ServedGraph
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
 * Call a runtime tool and return its text beside its structured content.
 *
 * Every jpack tool answers with one text content block, and reports refusals
 * in band via isError rather than as a protocol error. Both facts are handled
 * here so no view has to know them. What the text *is* is the caller's
 * question: most tools answer with JSON, and one — `experimental_get_graph` —
 * answers with the project's own document bytes, which are exactly as
 * parseable as the file on disk happens to be.
 */
async function callToolText(
  client: Client,
  name: string,
  args: Record<string, unknown> = {},
  signal?: AbortSignal
): Promise<{ raw: string; structured: Record<string, unknown> | undefined }> {
  // The signal is the query's own: a file change cancels in-flight project
  // queries before invalidating, so a stale in-flight answer can never satisfy
  // the fresh question (see McpProvider's fileChanged handler).
  const result = await client.callTool({ name, arguments: args }, undefined, { signal })

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
  return { raw: text, structured: result.structuredContent as Record<string, unknown> | undefined }
}

/**
 * Call a runtime tool whose text half is one JSON payload, and parse it.
 *
 * Text that is not JSON is a failure *here* because every tool this wraps
 * promises JSON. The one tool that promises only bytes does not come through
 * this door.
 */
async function callToolJSON<T>(
  client: Client,
  name: string,
  args: Record<string, unknown> = {},
  signal?: AbortSignal
): Promise<{ parsed: T; raw: string; structured: Record<string, unknown> | undefined }> {
  const { raw, structured } = await callToolText(client, name, args, signal)
  let parsed: T
  try {
    parsed = JSON.parse(raw) as T
  } catch (cause) {
    throw new Error(`${name} returned text that is not JSON: ${String(cause)}`)
  }
  return { parsed, raw, structured }
}

/** The project's declared packs, as the runtime resolves them. */
export function usePacks(): UseQueryResult<PackInventory, Error> {
  const { client, status } = useMcp()
  return useQuery({
    queryKey: ['list_packs'],
    enabled: status === 'ready' && client !== null,
    queryFn: async ({ signal }) => {
      const { parsed } = await callToolJSON<PackInventory>(client!, 'list_packs', {}, signal)
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
    queryFn: async ({ signal }) => {
      const { parsed, raw, structured } = await callToolJSON<LoadedPack['document']>(
        client!,
        'get_pack',
        { pack_id: packId },
        signal
      )
      return { document: parsed, raw, meta: (structured ?? {}) as PackFileMeta }
    }
  })
}

/**
 * The project's declared pack matrices, run.
 *
 * It is a query and not a mutation even though it runs the evaluator, because
 * the tool writes nothing: a matrix row is a rehearsal rather than a decision,
 * so no audit record is appended and no reviewed set is consulted (ADR-0018,
 * ADR-0019). That is the distinction `experimental_evaluate` does not draw,
 * and it is why this one may be cached and re-run on a file change.
 *
 * Passing a decision id runs that pack's matrix alone. Omitting the key runs
 * every declared pack — and the key is omitted rather than sent empty, because
 * a present-but-empty `pack_id` is refused rather than read as absent.
 */
export function usePackMatrix(packId?: string): UseQueryResult<PackTest, Error> {
  const { client, status } = useMcp()
  return useQuery({
    queryKey: ['experimental_test_packs', packId ?? null],
    enabled: status === 'ready' && client !== null,
    queryFn: async ({ signal }) => {
      const args = packId === undefined ? {} : { pack_id: packId }
      const { parsed } = await callToolJSON<PackTest>(client!, 'experimental_test_packs', args, signal)
      return parsed
    }
  })
}

/**
 * The project's configured graph matrices, run.
 *
 * The graph twin of `usePackMatrix`, and a read for the same reason. A project
 * that configures no graph is reported `skipped` with no entries rather than
 * refused, so "this project declares no graph" is an answer this query returns
 * rather than an error a view has to recognise.
 */
export function useGraphMatrix(
  graphId?: string,
  /**
   * Whether to run at all. The home page turns it off where the cheap
   * inventory answers its question instead — running every configured graph's
   * matrix to decide whether to render a link is a real cost on a large
   * project, and ADR-0029 made it unnecessary.
   */
  enabled = true
): UseQueryResult<GraphSuite, Error> {
  const { client, status } = useMcp()
  return useQuery({
    queryKey: ['experimental_test_graphs', graphId ?? null],
    enabled: enabled && status === 'ready' && client !== null,
    queryFn: async ({ signal }) => {
      const args = graphId === undefined ? {} : { graph_id: graphId }
      const { parsed } = await callToolJSON<GraphSuite>(client!, 'experimental_test_graphs', args, signal)
      return parsed
    }
  })
}

/**
 * The graphs the project configures, resolved (ADR-0029).
 *
 * The cheap half of the graph surface: one call, no evaluator, and the
 * document identities read beside the configured ids. It answers what the
 * graph matrix previously had to be run to answer, and it answers it about
 * graphs whose *rows* would not load — a graph the matrix reports only as a
 * failure is still a configured graph, and this says so.
 *
 * The query is disabled where the runtime does not advertise the tool, so
 * against jpack 0.18.0 and older it never fires and its consumers fall back.
 */
export function useGraphInventory(): UseQueryResult<GraphInventory, Error> {
  const { client, status, graphInventorySupported } = useMcp()
  return useQuery({
    queryKey: ['experimental_list_graphs'],
    enabled: status === 'ready' && client !== null && graphInventorySupported,
    queryFn: async ({ signal }) => {
      const { parsed } = await callToolJSON<GraphInventory>(
        client!,
        'experimental_list_graphs',
        {},
        signal
      )
      return parsed
    }
  })
}

/**
 * One configured graph document, served (ADR-0029).
 *
 * The text half is the project's own file, byte for byte; the structured half
 * is the metadata beside it, including the runtime's own `status` — `valid`
 * where its decode succeeded, `undecodable` where it did not. **Serving is not
 * validating**, so an undecodable document arrives as a successful call whose
 * text is not JSON. That is why this goes through `callToolText`: routing it
 * through the JSON door would turn a document the runtime deliberately served
 * into a transport error, and the mid-edit document is the one a client most
 * needs to see.
 *
 * The parse is this client's own and is kept apart from the runtime's verdict.
 * `document` is undefined whenever the text did not yield the declared shape —
 * whatever the metadata says — because a view drawing edges must draw them
 * from something it actually read.
 */
export function useGraphDocument(graphId: string | undefined): UseQueryResult<ServedGraph, Error> {
  const { client, status, graphDocumentSupported } = useMcp()
  return useQuery({
    queryKey: ['experimental_get_graph', graphId ?? null],
    enabled:
      status === 'ready' && client !== null && graphDocumentSupported && Boolean(graphId),
    queryFn: async ({ signal }) => {
      const { raw, structured } = await callToolText(
        client!,
        'experimental_get_graph',
        { graph_id: graphId },
        signal
      )
      return {
        // A runtime that answered without structured content leaves the
        // metadata empty rather than invented; every reader here treats a
        // missing member as missing.
        meta: (structured ?? {}) as unknown as GraphDocumentMeta,
        raw,
        document: parseGraphDocument(raw)
      }
    }
  })
}

/**
 * How many graphs the project configures, by the cheapest route the connected
 * runtime offers.
 *
 * With `experimental_list_graphs` (ADR-0029) that is one call that evaluates
 * nothing. Without it the only way to find out is to run every configured
 * graph's matrix — affordable, because a row is a rehearsal and writes nothing,
 * but a real cost on a large project paid to decide whether to render a link.
 * Exactly one of the two queries is enabled, so the older route is not run
 * beside the newer one.
 */
export function useConfiguredGraphs(): {
  count: number
  isPending: boolean
  error: Error | null
  /** True where the count came from the inventory rather than from a matrix run. */
  fromInventory: boolean
} {
  const { graphInventorySupported } = useMcp()
  const inventory = useGraphInventory()
  const matrix = useGraphMatrix(undefined, !graphInventorySupported)
  const source = graphInventorySupported ? inventory : matrix
  const graphs = graphInventorySupported ? inventory.data?.graphs : matrix.data?.graphs
  return {
    count: graphs?.length ?? 0,
    isPending: source.isPending,
    error: source.error,
    fromInventory: graphInventorySupported
  }
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
/**
 * The arguments one evaluation sends, built in one place so the rules are
 * testable: evidence absence is the omitted key, and the rehearsal declaration
 * is sent exactly when the connected runtime advertises the argument — never
 * to one that would refuse the unknown member, and never omitted from one that
 * accepts it, because a what-if run that could be a rehearsal and is not
 * appends a decision nobody took (ADR-0028).
 */
export function buildEvaluateArguments(
  input: EvaluateInput,
  rehearsalSupported: boolean
): Record<string, unknown> {
  const args: Record<string, unknown> = { pack_id: input.packId, facts: input.facts }
  if (input.evidence !== undefined) args.evidence = input.evidence
  if (rehearsalSupported) args.rehearsal = true
  return args
}

export function useEvaluate(): UseMutationResult<EvaluationRun, Error, EvaluateInput> {
  const { client, rehearsalSupported } = useMcp()
  return useMutation({
    mutationFn: async (input: EvaluateInput) => {
      if (!client) throw new Error('the desk is not connected to the runtime')
      const args = buildEvaluateArguments(input, rehearsalSupported)
      const { parsed, raw } = await callToolJSON<Evaluation>(
        client,
        'experimental_evaluate',
        args
      )
      return { payload: parsed, raw, facts: input.facts, evidence: input.evidence }
    }
  })
}
