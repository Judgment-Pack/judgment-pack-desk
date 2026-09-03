import type { Client } from '@modelcontextprotocol/sdk/client/index.js'
import {
  useMutation,
  useQuery,
  type UseMutationResult,
  type UseQueryResult
} from '@tanstack/react-query'
import { readServedDocument } from './graphDocument'
import { useMcp } from './McpProvider'
import { ToolRefusal } from './refusal'
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
  ServedGraph,
  ValidationReport
} from './types'

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

  const content = Array.isArray(result.content) ? result.content : []
  const textBlocks = content.filter(
    (block): block is { type: 'text'; text: string } => block?.type === 'text'
  )
  const text = textBlocks.map((block) => block.text).join('')

  if (result.isError) {
    throw new ToolRefusal(
      text || `the runtime refused ${name}`,
      result.structuredContent as RefusalEnvelope | undefined
    )
  }
  // The block count is the question, not the joined length. A tool that
  // answered with an empty text block *did* answer, and a served document that
  // happens to be an empty file is exactly that answer — reporting it as "no
  // text content" would describe the runtime as having said nothing. What an
  // empty answer means is each caller's own: the JSON door below rejects it at
  // the parse, and the served-document door reads it as the zero bytes it is.
  if (textBlocks.length === 0) {
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

/**
 * The project's declared packs, as the runtime resolves them.
 *
 * `retryOnMount: false` is the one option this query sets, and it is not a
 * retry policy — the client's default is already `retry: false`. It is about
 * *mounting*: a query in an error state with no data re-runs when another
 * observer subscribes, and the rail holds one observer for the life of the
 * desk while every route mounts a second. So a listing the runtime had refused
 * was called again on every navigation, once per route change, for as long as
 * it kept failing. A successful listing is cached and was never the problem.
 * The reconnect path is untouched: `McpProvider` invalidates every query when
 * the socket comes back, and the notices offer an explicit retry.
 */
export function usePacks(): UseQueryResult<PackInventory, Error> {
  const { client, status } = useMcp()
  return useQuery({
    queryKey: ['list_packs'],
    enabled: status === 'ready' && client !== null,
    retryOnMount: false,
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
 * One document, checked.
 *
 * **A query and not a mutation.** `validate` evaluates nothing, holds no
 * credential and writes nothing — it reads a document's text and reports what
 * the ladder found. Nothing is appended to an audit directory and no reviewed
 * set is consulted, which is the distinction `experimental_evaluate` does not
 * draw.
 *
 * **`document` and nothing else.** `through` is omitted deliberately: the
 * runtime's own default is `semantic`, so omitting it runs the whole ladder,
 * and sending a value would make the desk decide how far to check. An empty
 * document is refused by the runtime by name, so the query is disabled rather
 * than sending one.
 *
 * **The key is the bytes *and* the connection epoch.** Identical bytes answer
 * differently on a runtime bundling different specification artifacts — the
 * `unsupported` status with a `capability`-layer diagnostic at `/specVersion`
 * is exactly that answer — so a cached report that survived a reconnect would
 * describe a different binary's opinion of the same file. `useGraphDocument`
 * states the same rule one connection short of this.
 *
 * The whole document text is in the key. At pack sizes that is nothing; a
 * digest key would need `crypto.subtle`, which jsdom does not provide, so the
 * exact bytes are the honest key here.
 */
export function useValidate(
  documentText: string | undefined
): UseQueryResult<ValidationReport, Error> {
  const { client, status, validateSupported, connectionEpoch } = useMcp()
  return useQuery({
    queryKey: ['validate', connectionEpoch, documentText ?? null],
    // `documentText` must be bytes, not merely defined: the runtime refuses an
    // empty document by name, and a call made only to be refused would put a
    // refusal on screen where the honest answer is that there is nothing to
    // check yet.
    enabled:
      status === 'ready' &&
      client !== null &&
      validateSupported &&
      documentText !== undefined &&
      documentText !== '',
    queryFn: async ({ signal }) => {
      const { parsed } = await callToolJSON<ValidationReport>(
        client!,
        'validate',
        { document: documentText },
        signal
      )
      return parsed
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
  enabled = true,
  /**
   * Whether to ask for each compared node's trace (ADR-0031).
   *
   * Off is the default and off omits the key entirely, so the untraced call is
   * byte-identical to the one this desk has always made. On sends the boolean
   * the runtime advertises, and only a caller that checked the capability
   * should pass it: an older runtime refuses the unknown member rather than
   * ignoring it.
   *
   * It is part of the query key because a traced answer and an untraced one are
   * two different answers to two different questions. A shared key would let a
   * traced payload satisfy an untraced ask, and — worse — let an untraced
   * payload satisfy a traced one, which would read on screen as a runtime that
   * reported no traces rather than as a question never asked.
   */
  includeTraces = false
): UseQueryResult<GraphSuite, Error> {
  const { client, status } = useMcp()
  return useQuery({
    queryKey: ['experimental_test_graphs', graphId ?? null, includeTraces],
    enabled: enabled && status === 'ready' && client !== null,
    queryFn: async ({ signal }) => {
      const args: Record<string, unknown> = graphId === undefined ? {} : { graph_id: graphId }
      if (includeTraces) args.include_traces = true
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
 * The read is subordinate to the runtime's verdict, never a second opinion on
 * it. `readServedDocument` consults `meta.status` before it parses: the
 * runtime's decode is the stricter of the two — duplicate member names alone
 * are refused there and taken last-wins by `JSON.parse` — so a document the
 * runtime could not decode is never one this client decodes anyway. `document`
 * is undefined in that case and in every case where the text did not carry what
 * the views draw from, and `unreadable` says which.
 *
 * The connection epoch is part of the key. A served document is joined to a
 * matrix run's coverage by node name and edge index, and those two accounts
 * only describe one graph if they came from one connection to one project; a
 * key that survived a reconnect would let a document read before the socket
 * dropped be joined to a matrix run from after it.
 */
export function useGraphDocument(graphId: string | undefined): UseQueryResult<ServedGraph, Error> {
  const { client, status, graphDocumentSupported, connectionEpoch } = useMcp()
  return useQuery({
    queryKey: ['experimental_get_graph', connectionEpoch, graphId ?? null],
    enabled:
      status === 'ready' && client !== null && graphDocumentSupported && Boolean(graphId),
    queryFn: async ({ signal }) => {
      const { raw, structured } = await callToolText(
        client!,
        'experimental_get_graph',
        { graph_id: graphId },
        signal
      )
      // A runtime that answered without structured content leaves the metadata
      // empty rather than invented; every reader here treats a missing member
      // as missing — including the status, whose absence is not `valid`.
      const meta = (structured ?? {}) as unknown as GraphDocumentMeta
      const read = readServedDocument(meta, raw)
      return read.ok
        ? { meta, raw, document: read.document }
        : { meta, raw, unreadable: read.reason }
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
