/**
 * The buffer of record is the **text**, and an edit moves the bytes it names.
 *
 * A form over a parsed object has to re-serialize the whole file to save, so a
 * one-word change to a rule's description arrives as a diff of every line —
 * indentation, member order, the trailing newline, the author's own spacing.
 * ADR-0019 makes a human read that diff. So this indexes the bytes once,
 * records where every value and every member sits, and an edit splices exactly
 * that span: every byte outside it survives, unchanged, including the ones the
 * desk has no opinion about.
 *
 * **The scanner is this desk's own read of the bytes, and it is not the
 * runtime's.** `JSON.parse` collapses a duplicated member name last-wins; the
 * runtime refuses it by name (`JPS-CARRIER-DUPLICATE-MEMBER`, at the member's
 * own pointer — the probe in the PR body shows it). So the two readings can
 * disagree about what a document says, and `agreesWithParse` is what asks. On
 * an unexplained disagreement the caller withholds form editing and offers the
 * raw bytes instead: a form that wrote through a reading the runtime does not
 * share would edit a document nobody has.
 *
 * **Deliberately not here**, so the next piece of work does not re-derive them:
 * `insertMember` — adding a member needs a place to put it and a house style
 * for the whitespace around it, which is a decision and not a splice — and
 * array reordering, because rule order is §7-significant and moves through
 * keyboard move-up/move-down rather than through a generic splice.
 */

/** Where one value, and the member that carries it, sit in the text. */
export interface Span {
  pointer: string
  /** The first byte of the value itself. */
  valueStart: number
  /** One past the last byte of the value. */
  valueEnd: number
  /**
   * The first byte of the whole member — the opening quote of its name inside
   * an object, or the value's own start for an array element and the root.
   */
  memberStart: number
  /** One past the last byte of the member, which is the value's own end. */
  memberEnd: number
}

/** One member name that appears more than once in the same object. */
export interface DuplicateMember {
  /** The pointer of the repeated member, as the runtime reports it. */
  pointer: string
  /** The repeated name on its own. */
  name: string
}

export interface DocumentIndex {
  /** Every value's span, by pointer. The document itself is `''`. */
  spans: Map<string, Span>
  duplicates: DuplicateMember[]
  /**
   * The value this scanner read, present exactly when the bytes scanned. A
   * duplicated member keeps its **first** occurrence here, which is the
   * disagreement with `JSON.parse` that `agreesWithParse` reports.
   */
  value?: unknown
  /** Why the bytes did not scan, where they did not. */
  parseError?: string
}

class ScanError extends Error {}

/**
 * Walk the bytes once, recording every span.
 *
 * A hand-written scanner rather than a dependency: the question is where a
 * value *is*, and no JSON parser in the ecosystem answers it without also
 * bringing a serializer this must never use.
 */
export function indexDocument(text: string): DocumentIndex {
  const spans = new Map<string, Span>()
  const duplicates: DuplicateMember[] = []
  let at = 0

  const fail = (why: string): never => {
    throw new ScanError(`${why} at byte ${at}`)
  }
  const skip = () => {
    while (at < text.length && (text[at] === ' ' || text[at] === '\t' || text[at] === '\n' || text[at] === '\r')) {
      at += 1
    }
  }
  const literal = (word: string, value: unknown): unknown => {
    if (text.startsWith(word, at)) {
      at += word.length
      return value
    }
    return fail('expected a value')
  }
  const readString = (): string => {
    const start = at
    if (text[at] !== '"') fail('expected a string')
    at += 1
    while (at < text.length) {
      const char = text[at]
      if (char === '\\') {
        at += 2
        continue
      }
      if (char === '"') {
        at += 1
        return JSON.parse(text.slice(start, at)) as string
      }
      at += 1
    }
    return fail('the string does not end')
  }
  const readNumber = (): number => {
    const start = at
    if (text[at] === '-') at += 1
    while (at < text.length && /[0-9eE+.\-]/.test(text[at]!)) at += 1
    const raw = text.slice(start, at)
    const parsed = Number(raw)
    if (raw === '' || Number.isNaN(parsed)) fail('expected a number')
    return parsed
  }

  const record = (path: string[], valueStart: number, valueEnd: number, memberStart: number) => {
    const key = pointerOf(path)
    // First occurrence wins, which is what makes a duplicate visible: a second
    // span under one pointer would quietly overwrite the first and the two
    // readings would agree again.
    if (!spans.has(key)) {
      spans.set(key, { pointer: key, valueStart, valueEnd, memberStart, memberEnd: valueEnd })
    }
  }

  const readValue = (path: string[], memberStart: number): unknown => {
    skip()
    const valueStart = at
    const char = text[at]
    let value: unknown
    if (char === '{') {
      at += 1
      // **Null-prototype, and that is not fastidiousness.** A member literally
      // named `__proto__` assigned into `{}` invokes the prototype setter and
      // stores nothing, so the scanner's reading silently lacked a member
      // `JSON.parse` has — and the disagreement gate below, which exists to
      // catch exactly that, compared with `in` and found the inherited one. A
      // document could then be edited through a reading that did not match it.
      const object: Record<string, unknown> = Object.create(null) as Record<string, unknown>
      const seen = new Set<string>()
      skip()
      if (text[at] === '}') {
        at += 1
      } else {
        for (;;) {
          skip()
          const nameStart = at
          const name = readString()
          skip()
          if (text[at] !== ':') fail('expected a member separator')
          at += 1
          const member = readValue([...path, name], nameStart)
          if (seen.has(name)) {
            duplicates.push({ pointer: pointerOf([...path, name]), name })
          } else {
            seen.add(name)
            object[name] = member
          }
          skip()
          if (text[at] === ',') {
            at += 1
            continue
          }
          if (text[at] === '}') {
            at += 1
            break
          }
          fail('expected a comma or the end of the object')
        }
      }
      value = object
    } else if (char === '[') {
      at += 1
      const array: unknown[] = []
      skip()
      if (text[at] === ']') {
        at += 1
      } else {
        for (;;) {
          skip()
          const elementStart = at
          array.push(readValue([...path, String(array.length)], elementStart))
          skip()
          if (text[at] === ',') {
            at += 1
            continue
          }
          if (text[at] === ']') {
            at += 1
            break
          }
          fail('expected a comma or the end of the array')
        }
      }
      value = array
    } else if (char === '"') {
      value = readString()
    } else if (char === 't') {
      value = literal('true', true)
    } else if (char === 'f') {
      value = literal('false', false)
    } else if (char === 'n') {
      value = literal('null', null)
    } else {
      value = readNumber()
    }
    record(path, valueStart, at, memberStart)
    return value
  }

  try {
    skip()
    const value = readValue([], at)
    skip()
    if (at !== text.length) fail('there are bytes after the document')
    return { spans, duplicates, value }
  } catch (cause) {
    return {
      spans,
      duplicates,
      parseError: cause instanceof ScanError ? cause.message : String(cause)
    }
  }
}

/** The escaping, kept identical to `pointers.ts` and to the runtime's own. */
function pointerOf(parts: readonly string[]): string {
  if (parts.length === 0) return ''
  let value = ''
  for (const part of parts) {
    value += '/'
    for (const char of part) {
      if (char === '~') value += '~0'
      else if (char === '/') value += '~1'
      else value += char
    }
  }
  return value
}

export function spanAt(index: DocumentIndex, pointer: string): Span | undefined {
  return index.spans.get(pointer)
}

/**
 * Replace one value's bytes and leave every other byte alone.
 *
 * `json` is written in verbatim: shaping a value is the caller's, because the
 * shape depends on what the member is — an ordered comparison takes a decimal
 * *string*, and `validate` is what says so.
 */
export function replaceValue(
  text: string,
  index: DocumentIndex,
  pointer: string,
  json: string
): string {
  const span = spanAt(index, pointer)
  if (span === undefined) return text
  return text.slice(0, span.valueStart) + json + text.slice(span.valueEnd)
}

/**
 * Take one member out, and exactly one adjacent comma with it.
 *
 * This is what blanking a `nonEmptyString` calls. Writing `""` produces a
 * document the runtime refuses by name; omitting the member produces a
 * document that is merely smaller, and where the member was required the
 * refusal that follows names the absent member at its own pointer — which is
 * an address the form already has.
 *
 * The comma is taken **after** the member where there is one and **before** it
 * otherwise, so the last member of an object leaves no dangling comma and the
 * only member of an object leaves an empty one.
 *
 * **What is removed is the deleted member's own layout, never its neighbour's.**
 * Taking the whitespace *after* the forward comma looks equivalent and is not:
 * that whitespace belongs to the member that follows. Removing `/a` from
 * `{⏎\t"a":1,⏎␣␣␣␣␣␣"b":2⏎}` used to give `{⏎\t"b":2⏎}` — `b` lost its own six
 * spaces and inherited `a`'s tab, so a document with unequal indentation was
 * silently reformatted by a delete. The span taken is instead the run of
 * whitespace *before* the member, the member, and the comma after it; the next
 * member's own indentation is never inside it.
 */
export function removeMember(text: string, index: DocumentIndex, pointer: string): string {
  const span = spanAt(index, pointer)
  if (span === undefined || pointer === '') return text
  let start = span.memberStart
  let end = span.memberEnd

  // The layout this member was written with, which goes with it.
  let back = start - 1
  while (back >= 0 && isSpace(text[back]!)) back -= 1
  const layoutStart = back + 1

  let scan = end
  while (scan < text.length && isSpace(text[scan]!)) scan += 1
  if (text[scan] === ',') {
    // A member with something after it: its own preceding layout, itself, and
    // the comma. Whatever whitespace follows that comma is the next member's
    // and is left exactly where it was.
    start = layoutStart
    end = scan + 1
  } else if (text[back] === ',') {
    // The last member: the comma before it goes instead, and the whitespace
    // between that comma and this member goes with it.
    start = back
  } else {
    // The only member: its layout goes, leaving the braces as they were.
    start = layoutStart
  }
  return text.slice(0, start) + text.slice(end)
}

function isSpace(char: string): boolean {
  return char === ' ' || char === '\t' || char === '\n' || char === '\r'
}

/** One way the desk's reading of the bytes and `JSON.parse`'s differ. */
export interface Disagreement {
  pointer: string
  reason: string
}

/**
 * Ask whether this desk's reading of the bytes is the reading `JSON.parse`
 * has, and say where it is not.
 *
 * The answer gates form editing. A duplicated member name is the case that
 * exists in the wild: `JSON.parse` keeps the last, this scanner keeps the
 * first, and the runtime keeps neither — it refuses the document. Editing
 * through a reading nobody else shares would splice into a member the runtime
 * does not believe is there.
 */
export function agreesWithParse(text: string, index: DocumentIndex): Disagreement[] {
  const problems: Disagreement[] = []
  for (const duplicate of index.duplicates) {
    problems.push({
      pointer: duplicate.pointer,
      reason: `the member ${JSON.stringify(duplicate.name)} appears more than once`
    })
  }
  if (index.parseError !== undefined) {
    // Two different facts, and the caller needs both: a document neither
    // reading can take is not JSON, and a document only *this* reading
    // refuses is a disagreement about bytes that parse. Neither can be
    // spliced, and each says why in its own words.
    let parses = true
    try {
      JSON.parse(text)
    } catch {
      parses = false
    }
    problems.push({
      pointer: '',
      reason: parses
        ? `the desk could not read these bytes: ${index.parseError}`
        : `these bytes are not JSON: ${index.parseError}`
    })
    return problems
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch (cause) {
    problems.push({ pointer: '', reason: `these bytes are not JSON: ${String(cause)}` })
    return problems
  }
  const mismatch = firstDifference(index.value, parsed, [])
  if (mismatch !== undefined) problems.push(mismatch)
  return problems
}

function firstDifference(mine: unknown, theirs: unknown, path: string[]): Disagreement | undefined {
  const here = pointerOf(path)
  if (Array.isArray(mine) || Array.isArray(theirs)) {
    if (!Array.isArray(mine) || !Array.isArray(theirs) || mine.length !== theirs.length) {
      return { pointer: here, reason: 'the two readings do not agree on this array' }
    }
    for (let index = 0; index < mine.length; index += 1) {
      const found = firstDifference(mine[index], theirs[index], [...path, String(index)])
      if (found !== undefined) return found
    }
    return undefined
  }
  if (isObject(mine) || isObject(theirs)) {
    if (!isObject(mine) || !isObject(theirs)) {
      return { pointer: here, reason: 'the two readings do not agree on this member' }
    }
    const names = new Set([...Object.keys(mine), ...Object.keys(theirs)])
    for (const name of names) {
      // Own properties only. `in` walks the prototype chain, so `__proto__`,
      // `constructor` and `toString` were "carried" by a reading that carries
      // no such member — which is how the one gate against a mismatched
      // reading reported agreement on a document it disagreed about.
      if (!Object.hasOwn(mine, name) || !Object.hasOwn(theirs, name)) {
        return {
          pointer: pointerOf([...path, name]),
          reason: `only one reading carries the member ${JSON.stringify(name)}`
        }
      }
      const found = firstDifference(mine[name], theirs[name], [...path, name])
      if (found !== undefined) return found
    }
    return undefined
  }
  if (mine !== theirs) {
    return { pointer: here, reason: 'the two readings do not agree on this value' }
  }
  return undefined
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
