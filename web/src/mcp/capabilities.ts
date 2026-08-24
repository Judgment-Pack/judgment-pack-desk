/**
 * Reading what the connected runtime can do, off its own `tools/list`.
 *
 * Kept apart from the provider that calls it so that the desk's own client
 * script — which runs outside a browser and must not pull React in — asks the
 * question exactly the way the page does.
 */

/**
 * What the connected runtime says it can do, read once per connection off its
 * own `tools/list`.
 *
 * A capability is what the server advertises, never what a version string
 * implies: the desk is built against an unreleased runtime and must keep
 * working against a released one, so every one of these is feature-detected
 * and every false is a working page with less on it.
 */
export interface RuntimeCapabilities {
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
}

/** Every capability off, which is what an unread or failed listing means. */
export const NO_CAPABILITIES: RuntimeCapabilities = {
  rehearsalSupported: false,
  graphDocumentSupported: false,
  graphInventorySupported: false
}

/** One row of a `tools/list` answer, narrowed to what capability reading uses. */
interface AdvertisedTool {
  name: string
  inputSchema?: unknown
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
 */
export function readCapabilities(tools: readonly AdvertisedTool[]): RuntimeCapabilities {
  const names = new Set(tools.map((tool) => tool.name))
  const evaluate = tools.find((tool) => tool.name === 'experimental_evaluate')
  const properties = (evaluate?.inputSchema as { properties?: Record<string, unknown> } | undefined)
    ?.properties
  return {
    rehearsalSupported: Boolean(properties && 'rehearsal' in properties),
    graphDocumentSupported: names.has('experimental_get_graph'),
    graphInventorySupported: names.has('experimental_list_graphs')
  }
}
