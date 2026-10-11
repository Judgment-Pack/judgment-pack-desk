import { jsonLanguage } from '@codemirror/lang-json'

/** Extract a member's original JSON lexemes: numbers must not round in display. */
export function historyRecordField(raw: string, name: string): string | undefined {
  const object = jsonLanguage.parser.parse(raw).topNode.firstChild
  if (object?.name !== 'Object') return undefined
  let field: string | undefined
  for (const property of object.getChildren('Property')) {
    const key = property.getChild('PropertyName')
    const value = property.lastChild
    if (!key || !value || JSON.parse(raw.slice(key.from, key.to)) !== name) continue
    // An ambiguous record can still be inspected/exported in full, but must
    // not acquire a definitive field value by selecting one duplicate.
    if (field !== undefined) return undefined
    field = raw.slice(value.from, value.to)
  }
  return field
}
