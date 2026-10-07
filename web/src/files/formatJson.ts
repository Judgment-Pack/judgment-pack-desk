/** Format whitespace only: do not round numbers, reorder keys, normalize escapes,
 * or collapse duplicate keys through a parse/stringify round trip. */
export function formatJson(source: string): { ok: true; content: string } | { ok: false; reason: 'invalid' | 'too-large' } {
  try { JSON.parse(source) } catch { return { ok: false, reason: 'invalid' } }
  const limit = 4 << 20 // Matches the file editor's read/write bound.
  const parts: string[] = []
  let depth = 0, length = 0, previous = ''
  const append = (part: string) => { length += part.length; parts.push(part) }
  // Syntax was checked above. Capture complete string/primitive lexemes and
  // punctuation, dropping only JSON whitespace outside strings.
  for (const match of source.matchAll(/"(?:[^"\\]|\\.)*"|[^\s{}\[\],:]+|[{}\[\],:]/g)) {
    const token = match[0]
    const closes = token === '}' || token === ']'
    const opens = previous === '{' || previous === '['
    if (closes) depth--
    if ((closes && !opens) || (!closes && (opens || previous === ','))) append('\n' + '  '.repeat(depth))
    append(token === ':' ? ': ' : token)
    if (token === '{' || token === '[') depth++
    previous = token
    if (length >= limit) return { ok: false, reason: 'too-large' }
  }
  append('\n')
  const content = parts.join('')
  if (new TextEncoder().encode(content).length > limit) return { ok: false, reason: 'too-large' }
  return { ok: true, content }
}
