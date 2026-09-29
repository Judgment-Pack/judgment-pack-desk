/** Walk each direction independently. Flooding an undirected component would
 * include sibling outcomes/rules whose paths never pass through the selection. */
export function relationshipPaths(nodes: readonly { id: string; selected?: boolean }[], edges: readonly { id: string; source: string; target: string }[]) {
  const valid = new Set(nodes.map(node => node.id))
  const selected = nodes.filter(node => node.selected).map(node => node.id)
  const nodeIds = new Set(selected), edgeIds = new Set<string>()
  for (const reverse of [false, true]) {
    const adjacency = new Map<string, { id: string; next: string }[]>()
    for (const edge of edges) {
      if (!valid.has(edge.source) || !valid.has(edge.target)) continue
      const start = reverse ? edge.target : edge.source, next = reverse ? edge.source : edge.target
      const bucket = adjacency.get(start) ?? []
      bucket.push({ id: edge.id, next }); adjacency.set(start, bucket)
    }
    const visited = new Set(selected), queue = [...selected]
    for (let i = 0; i < queue.length; i++) for (const edge of adjacency.get(queue[i]!) ?? []) {
      edgeIds.add(edge.id); nodeIds.add(edge.next)
      if (!visited.has(edge.next)) { visited.add(edge.next); queue.push(edge.next) }
    }
  }
  return { active: selected.length > 0, nodeIds, edgeIds }
}
