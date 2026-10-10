export const GRAPH_TOOLS = new Set(['list_decisions', 'read_decision', 'get_graph_authoring_instructions', 'graph_validate', 'graph_explain', 'propose_graph', 'graph_rehearse'])
export interface GraphWork { arguments: string; result?: string; truncated?: boolean }
export function graphWork(args: unknown): GraphWork {
  const text = JSON.stringify(args) ?? '{}'
  return {arguments: text.slice(0, 16000), ...(text.length > 16000 ? {truncated: true} : {})}
}
export function graphWorkResult(previous: GraphWork | undefined, text: string): GraphWork {
  return {...previous ?? {arguments: '{}'}, result: text.slice(0, 65536), ...(text.length > 65536 || previous?.truncated ? {truncated: true} : {})}
}
export function validGraphWork(value: unknown): value is GraphWork {
  if (!value || typeof value !== 'object') return false
  const item = value as GraphWork
  return typeof item.arguments === 'string' && item.arguments.length <= 16000 && (item.result === undefined || typeof item.result === 'string' && item.result.length <= 65536) && (item.truncated === undefined || typeof item.truncated === 'boolean')
}
