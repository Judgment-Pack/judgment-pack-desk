import { msg } from '../i18n'
import { parseJsonText, type JsonNode } from '../research/verify/canon'
import { RetainedJSON } from './wire'
import type { JobInput } from './client'

/** Editing a different field must not round a number elsewhere in the document. */
export function editableJSON(text: string): unknown {
 const root = parseJsonText(text)
 function exact(node: JsonNode): boolean {
  if (node.kind === 'number') return Number.isFinite(Number(node.literal)) && JSON.stringify(Number(node.literal)) === node.literal
  if (node.kind === 'object') return node.members.every(m => exact(m.value))
  if (node.kind === 'array') return node.items.every(exact)
  return true
 }
 if (!exact(root)) throw Error(msg('Use JSON to preserve these exact numeric values.'))
 return JSON.parse(text)
}

export function parseInput(facts: string, supplied: boolean, evidence: string): JobInput {
 let value: unknown
 try {
  parseJsonText(facts)
  try { value = editableJSON(facts) } catch { value = new RetainedJSON(facts) }
 } catch { throw Error(msg('Facts must be valid JSON.')) }
 if (!supplied) return { facts: value }
 let availability: unknown
 try { parseJsonText(evidence); availability = JSON.parse(evidence) } catch { throw Error(msg('Evidence must be valid JSON.')) }
 if (!availability || Array.isArray(availability) || typeof availability !== 'object' || Object.values(availability).some(v => !['present','absent','unknown'].includes(v as string))) throw Error(msg('Evidence must map requirement IDs to present, absent or unknown.'))
 return { facts: value, evidence: availability as JobInput['evidence'] }
}
