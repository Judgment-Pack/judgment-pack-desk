/**
 * One top-level member of a configuration file, **by its own bytes**.
 *
 * Admin shows what is in the file, and "what is in the file" is the bytes
 * somebody wrote rather than this page's re-serialisation of them. The two
 * differ in ways that matter to whoever has to repair the file: `1e2` is not
 * `100`, an integer past a float64's precision is not the number it round-trips
 * to, and a member's own indentation and key order are theirs. The chassis
 * decides the same way — `topLevelMembers` in `internal/desk/assistant.go`
 * carries every member it was not asked about across verbatim — so this is the
 * page's half of one rule rather than a second one.
 *
 * **A duplicate top-level member yields nothing**, exactly as it refuses a
 * write there: `JSON.parse` keeps the last value and a reader in another
 * language may keep the first, so a file two readers disagree about is one this
 * page will not quote a member out of.
 */

/**
 * The member's own bytes, or `undefined` where they cannot be established.
 *
 * `undefined` covers four different states and deliberately does not tell them
 * apart, because the caller does the same thing in all four: the text is not
 * one JSON object, it is malformed, the member is absent, or the member is
 * written twice. What the caller shows instead is the decoded value, labelled
 * as decoded.
 */
export function memberBytes(text: string, member: string): string | undefined {
  let at = skipSpace(text, 0)
  if (text[at] !== '{') return undefined
  at = skipSpace(text, at + 1)
  if (text[at] === '}') return undefined
  let found: string | undefined
  for (;;) {
    if (text[at] !== '"') return undefined
    const name = readString(text, at)
    if (name === undefined) return undefined
    at = skipSpace(text, name.end)
    if (text[at] !== ':') return undefined
    const start = skipSpace(text, at + 1)
    const end = valueEnd(text, start)
    if (end === undefined) return undefined
    if (name.value === member) {
      // Written twice: two readers of this file would not agree what the
      // member holds, so this page quotes neither value.
      if (found !== undefined) return undefined
      found = text.slice(start, end)
    }
    at = skipSpace(text, end)
    if (text[at] === ',') {
      at = skipSpace(text, at + 1)
      continue
    }
    if (text[at] === '}') return found
    return undefined
  }
}

const SPACE = new Set([' ', '\t', '\n', '\r'])

function skipSpace(text: string, from: number): number {
  let at = from
  while (at < text.length && SPACE.has(text[at]!)) at += 1
  return at
}

/** One JSON string starting at `from`, decoded, and where it ends. */
function readString(
  text: string,
  from: number
): { value: string; end: number } | undefined {
  const end = stringEnd(text, from)
  if (end === undefined) return undefined
  try {
    return { value: JSON.parse(text.slice(from, end)) as string, end }
  } catch {
    return undefined
  }
}

/** The index just past the closing quote of the string starting at `from`. */
function stringEnd(text: string, from: number): number | undefined {
  let at = from + 1
  while (at < text.length) {
    const char = text[at]!
    if (char === '\\') {
      at += 2
      continue
    }
    if (char === '"') return at + 1
    at += 1
  }
  return undefined
}

/**
 * The index just past the value starting at `from`.
 *
 * Braces and brackets are counted, and a quote inside a string is not a
 * delimiter — which is the whole reason this is a scan rather than an index of
 * the next `,`. A scalar ends at the first character that cannot be part of
 * one; the caller's own `JSON.parse` of the whole file is what establishes that
 * the scalar is well formed, so this does not re-check it.
 */
function valueEnd(text: string, from: number): number | undefined {
  const first = text[from]
  if (first === undefined) return undefined
  if (first === '"') return stringEnd(text, from)
  if (first !== '{' && first !== '[') {
    let at = from
    while (at < text.length && !SPACE.has(text[at]!) && text[at] !== ',' && text[at] !== '}') {
      at += 1
    }
    return at === from ? undefined : at
  }
  let depth = 0
  let at = from
  while (at < text.length) {
    const char = text[at]!
    if (char === '"') {
      const end = stringEnd(text, at)
      if (end === undefined) return undefined
      at = end
      continue
    }
    if (char === '{' || char === '[') depth += 1
    if (char === '}' || char === ']') {
      depth -= 1
      if (depth === 0) return at + 1
      if (depth < 0) return undefined
    }
    at += 1
  }
  return undefined
}
