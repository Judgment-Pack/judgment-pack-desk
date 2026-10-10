import { readFile } from '../files/client'

export interface GraphProvenance {
  graphSha256?: string
  configSha256?: string
  nodes?: {pack?: string; packSha256?: string}[]
}
const digest = (value: unknown): value is string => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value)
export function hasGraphProvenance(value: GraphProvenance): boolean {
  return digest(value.graphSha256) && digest(value.configSha256) && Array.isArray(value.nodes) && value.nodes.length > 0
    && value.nodes.every(node => typeof node.pack === 'string' && digest(node.packSha256))
}
/** Comparison is byte identity only. It does not establish review or policy correctness. */
export async function compareGraphRevision(id: string, value: GraphProvenance, signal: AbortSignal): Promise<'matching' | 'changed' | 'unavailable'> {
  if (!hasGraphProvenance(value)) return 'unavailable'
  const config = await readFile('jpack.json', signal)
  if (config.sha256 !== value.configSha256) return 'changed'
  const declared = JSON.parse(config.content) as {graphs?: Record<string, {path: string}>; packs?: Record<string, {path: string}>}
  if (!declared.graphs || !Object.hasOwn(declared.graphs, id)) return 'changed'
  const graph = await readFile(declared.graphs[id]!.path, signal)
  if (graph.sha256 !== value.graphSha256) return 'changed'
  const seen = new Map<string, string>()
  for (const node of value.nodes!) {
    const pack = node.pack!
    if (!declared.packs || !Object.hasOwn(declared.packs, pack)) return 'changed'
    if (!seen.has(pack)) seen.set(pack, (await readFile(declared.packs[pack]!.path, signal)).sha256)
    if (seen.get(pack) !== node.packSha256) return 'changed'
  }
  return 'matching'
}
