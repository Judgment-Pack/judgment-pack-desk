import { expect, it } from 'vitest'
import { quoteRange } from './quote'

it.each([
  ['before line one\nline two after', 'line one line two', 'line one\nline two'],
  [' \t前言 😀\t\r\n第二行 結尾 ', '😀 第二行', '😀\t\r\n第二行'],
  ['same quote then same quote', 'same quote', 'same quote'],
  ['a\u00a0b\u2003c', 'a b c', 'a\u00a0b\u2003c'],
  ['prefix exact suffix', ' exact ', 'exact']
])('maps %j to a range in unchanged source bytes', (text, quote, expected) => {
  const range = quoteRange(text, quote)!
  expect(text.slice(range.start, range.end)).toBe(expected)
})
it.each(['', ' \t\n', 'not on this page'])('rejects empty or missing quotes: %j', quote => {
  expect(quoteRange('line one\nline two', quote)).toBeUndefined()
})
it('preserves the previous match contract across whitespace combinations and substrings', () => {
  const fold = (text: string) => text.replace(/\s+/gu, ' ').trim()
  for (const separator of [' ', '\n', '\t\r\n', '\u00a0', '\u2003']) {
    const text = `  Alpha${separator}😀beta${separator}終わり  `
    const normalized = fold(text)
    for (let start = 0; start < normalized.length; start++) for (let end = start + 1; end <= normalized.length; end++) {
      const quote = normalized.slice(start, end)
      const expected = Boolean(quote.trim() && (text.includes(quote.trim()) || fold(text).includes(fold(quote))))
      const range = quoteRange(text, quote)
      expect(Boolean(range)).toBe(expected)
      if (range) expect(fold(text.slice(range.start, range.end))).toBe(fold(quote))
    }
  }
})
