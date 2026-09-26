import { msg } from '../i18n'
import type { ResearchGatewayConfig } from '../config/deskConfig'
import { base64, verifyDocument } from '../documents/client'
import { parseJsonText, type JsonNode } from '../research/verify/canon'
import { sha256Hex } from '../research/verify/receipt'
import { factFields } from '../packs/test-workspace/model'
import type { PackDocument } from '../mcp/types'
import type { InputMapping, SourceInput } from './client'

export const pointerEscape = (s: string) => s.replace(/~/g, '~0').replace(/\//g, '~1')
export function sourceText(snapshot: SourceInput['snapshot']): string {
  const { original } = snapshot
  const bytes = Uint8Array.from(atob(original.bytes), c => c.charCodeAt(0))
  if (!bytes.length || bytes.length > 200_000 || !(original.mediaType === 'application/json' || original.mediaType === 'text/plain' && /\.json$/i.test(original.name))) throw Error(msg('Choose one JSON file up to 200 KB.'))
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
}
/** Keep literal numbers intact. This lists paths; the runner alone maps inputs. */
export function sourcePaths(snapshot: SourceInput['snapshot']): string[] {
  const root = parseJsonText(sourceText(snapshot))
  if (root.kind !== 'object') throw Error(msg('Choose one JSON object describing a single input record.'))
  const paths: string[] = []
  const walk = (v: JsonNode, path: string, depth: number) => {
    if (depth > 32) throw Error(msg('The JSON file is nested too deeply.'))
    paths.push(path)
    if (v.kind === 'object') v.members.forEach(m => walk(m.value, `${path}/${pointerEscape(m.name)}`, depth + 1))
    if (v.kind === 'array') v.items.forEach((item, i) => walk(item, `${path}/${i}`, depth + 1))
  }
  walk(root, '', 0)
  return paths
}
export async function localSnapshot(file: File): Promise<SourceInput['snapshot']> {
  if (!file.size || file.size > 200_000 || !/\.json$/i.test(file.name)) throw Error(msg('Choose one JSON file up to 200 KB.'))
  const bytes = new Uint8Array(await file.arrayBuffer())
  const snapshot: SourceInput['snapshot'] = { version: 1, selectedAt: new Date().toISOString(), original: { name: file.name, mediaType: 'application/json', bytes: base64(bytes), sha256: `sha256:${await sha256Hex(bytes)}` } }
  sourcePaths(snapshot)
  return snapshot
}
export function initialMapping(doc: PackDocument, provider: InputMapping['provider'], paths: string[]): InputMapping {
  const choose = (...candidates: string[]) => candidates.find(path => paths.includes(path))
  return {
    version: 1, provider,
    facts: factFields(doc).flatMap(f => { const source = choose(f.path, `/facts${f.path}`); return source === undefined ? [] : [{ target: f.path, source }] }),
    evidence: (doc.evidenceRequirements ?? []).flatMap(e => { const source = choose(`/evidence/${pointerEscape(e.id)}`, `/evidenceAvailability/${pointerEscape(e.id)}`); return source === undefined ? [] : [{ requirement: e.id, source }] }),
  }
}
export async function verifySource(source: SourceInput, pin: ResearchGatewayConfig | null) {
  sourcePaths(source.snapshot)
  if (source.mapping.provider === 'google-drive') {
    if (!pin || !source.snapshot.proof?.drive || source.snapshot.proof.source !== 'drive') throw Error(msg('Reconnect Google Drive before using this source.'))
    await verifyDocument(source.snapshot, pin)
  }
}
