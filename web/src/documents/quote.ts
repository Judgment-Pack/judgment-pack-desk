/** Locate a quotation without modifying the signed source text. Offsets use JS
 * string indices, including when PDF whitespace was folded by the model. */
export function quoteRange(text: string, quote: string): { start: number; end: number } | undefined {
  const trimmed = quote.trim()
  if (!trimmed) return
  const exact = text.indexOf(trimmed)
  if (exact >= 0) return { start: exact, end: exact + trimmed.length }
  const needle = trimmed.replace(/\s+/gu, ' ')
  const index = text.replace(/\s+/gu, ' ').trim().indexOf(needle)
  if (index < 0) return
  let cursor = 0, start: number | undefined
  for (const word of text.matchAll(/\S+/gu)) {
    if (start === undefined && index >= cursor && index < cursor + word[0].length) start = word.index + index - cursor
    const end = index + needle.length
    if (start !== undefined && end <= cursor + word[0].length) return { start, end: word.index + end - cursor }
    cursor += word[0].length + 1
  }
}
