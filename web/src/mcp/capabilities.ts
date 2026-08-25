/**
 * Reading what the connected runtime can do, off its own `tools/list`.
 *
 * Kept apart from the provider that calls it so that the desk's own client
 * script — which runs outside a browser and must not pull React in — asks the
 * question exactly the way the page does, including how it pages.
 */

/**
 * What the connected runtime says it can do, read once per connection off its
 * own `tools/list`.
 *
 * A capability is what the server advertises, never what a version string
 * implies: the desk is built against an unreleased runtime and must keep
 * working against a released one, so every one of these is feature-detected
 * and every false is a working page with less on it.
 *
 * Every false is only honest once `known` is true. A listing that never
 * answered leaves the desk not knowing what the runtime can do, which is a
 * different thing from knowing it can do none of it — see `known`.
 */
export interface RuntimeCapabilities {
  /**
   * True once a `tools/list` answer has been read. False means the listing was
   * never made or did not complete, and every flag below is then *unknown*
   * rather than absent: presenting a runtime whose listing failed as one
   * without the tools would impersonate an older runtime, and quietly withdraw
   * features from a runtime that has them.
   */
  known: boolean
  /**
   * True when `experimental_evaluate` advertises the boolean `rehearsal`
   * argument (ADR-0028, jpack >= 0.18.0). Read from the tool's own declared
   * schema — the argument is a member of a schema, so the schema is where the
   * question is asked.
   */
  rehearsalSupported: boolean
  /**
   * True when `experimental_get_graph` is advertised (ADR-0029). With it the
   * desk can fetch the one artifact that states a graph's composition and draw
   * its real edges; without it the walk falls back to the coverage-derived
   * order axis, which draws none.
   */
  graphDocumentSupported: boolean
  /**
   * True when `experimental_list_graphs` is advertised (ADR-0029). With it the
   * graphs a project configures cost one cheap call to learn; without it the
   * only way to find out is to run every graph's matrix.
   */
  graphInventorySupported: boolean
  /**
   * True when `experimental_test_graphs` advertises the boolean
   * `include_traces` argument (ADR-0031, jpack >= 0.19.0). Read from that
   * tool's own declared schema, exactly as `rehearsal` is read from
   * `experimental_evaluate`'s: the argument is a member of a schema, so the
   * schema is where the question is asked, and a tool of the same name may or
   * may not take it.
   */
  graphTracesSupported: boolean
}

/**
 * Nothing read yet: no listing has answered, so nothing is claimed either way.
 * It is the state of a connection still being made and of one whose listing
 * failed, and the views treat it as the connected page with the least on it —
 * while the page says the listing is what is missing.
 */
export const UNKNOWN_CAPABILITIES: RuntimeCapabilities = {
  known: false,
  rehearsalSupported: false,
  graphDocumentSupported: false,
  graphInventorySupported: false,
  graphTracesSupported: false
}

/** One row of a `tools/list` answer, narrowed to what capability reading uses. */
export interface AdvertisedTool {
  name: string
  inputSchema?: unknown
}

/** The half of an MCP client this module needs: one paged tool listing. */
export interface ToolLister {
  listTools(params?: { cursor?: string }): Promise<{ tools: AdvertisedTool[]; nextCursor?: string }>
}

/** More pages than any real runtime advertises; a stop rather than a policy. */
const MAX_TOOL_PAGES = 64

/**
 * Every tool the runtime advertises, following `nextCursor` to the end.
 *
 * MCP pages `tools/list`, and a runtime that grows past one page would
 * otherwise have its later tools read as absent — which is exactly the silent
 * feature withdrawal capability detection exists to avoid. A cursor the server
 * repeats, or a page count past any plausible listing, stops the walk: a client
 * that looped forever on a misbehaving server would hang the connection rather
 * than report it.
 */
export async function listAllTools(client: ToolLister): Promise<AdvertisedTool[]> {
  const tools: AdvertisedTool[] = []
  const seen = new Set<string>()
  let cursor: string | undefined
  for (let page = 0; page < MAX_TOOL_PAGES; page += 1) {
    const answer = await client.listTools(cursor === undefined ? undefined : { cursor })
    tools.push(...answer.tools)
    const next = answer.nextCursor
    if (next === undefined || next === null || next === '') return tools
    if (seen.has(next)) {
      throw new Error(
        `the runtime's tools/list repeated the cursor ${JSON.stringify(next)}, so its tool ` +
          'listing does not terminate'
      )
    }
    seen.add(next)
    cursor = next
  }
  throw new Error(
    `the runtime's tools/list did not end within ${MAX_TOOL_PAGES} pages, so its tool listing ` +
      'was not read'
  )
}

/**
 * Read the capabilities off one `tools/list` answer.
 *
 * Two different questions are asked two different ways, because they are two
 * different kinds of claim. Whether a *tool* exists is asked by name, which is
 * the only thing MCP guarantees about a tool that is not there — an absent
 * name is an absent verb. Whether an *argument* is accepted is asked of that
 * tool's declared input schema, because a tool of the same name may or may not
 * take it.
 *
 * Nothing here reads a version string. The runtime that grew the graph surface
 * is unreleased while this is written, and a desk that gated on a version
 * would be wrong twice: against a development build that has the tools, and
 * against a future release that removes them, which an experimental surface is
 * allowed to do.
 *
 * The answer this reads must be the *whole* listing — see `listAllTools`. An
 * answer read off one page of several would report the tools on later pages as
 * absent, which is a false negative this cannot detect from here.
 */
export function readCapabilities(tools: readonly AdvertisedTool[]): RuntimeCapabilities {
  const names = new Set(tools.map((tool) => tool.name))
  const takes = (tool: string, argument: string) =>
    argument in (advertisedProperties(tools, tool) ?? {})
  return {
    known: true,
    rehearsalSupported: takes('experimental_evaluate', 'rehearsal'),
    graphDocumentSupported: names.has('experimental_get_graph'),
    graphInventorySupported: names.has('experimental_list_graphs'),
    graphTracesSupported: takes('experimental_test_graphs', 'include_traces')
  }
}

/**
 * The properties one advertised tool's input schema declares, or undefined.
 *
 * Undefined covers several different things and deliberately does not
 * distinguish them, because every one answers the argument question the same
 * way: no such tool, a tool with no schema, a schema with no `properties`, and
 * a `properties` that is not an object at all all mean this runtime does not
 * advertise the argument. Reading a missing schema as "takes everything" is one
 * failure this shape prevents.
 *
 * The type check is the other, and it is not defensive decoration. `inputSchema`
 * is `unknown` off the wire — a server may send anything — and `"x" in y`
 * throws a TypeError for every non-object `y`, including `null`, `false`, `0`
 * and `""`. A malformed listing would then take down capability reading
 * altogether, which leaves the connection reporting *nothing* about what the
 * runtime can do rather than reporting one argument as unadvertised.
 */
function advertisedProperties(
  tools: readonly AdvertisedTool[],
  name: string
): Record<string, unknown> | undefined {
  const tool = tools.find((candidate) => candidate.name === name)
  const schema = tool?.inputSchema
  if (typeof schema !== 'object' || schema === null) return undefined
  const properties = (schema as { properties?: unknown }).properties
  if (typeof properties !== 'object' || properties === null || Array.isArray(properties)) {
    return undefined
  }
  return properties as Record<string, unknown>
}
