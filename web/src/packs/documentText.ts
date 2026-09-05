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
 * **Both absences this module used to name are now here.** `insertMember`
 * needed a place to put a member and a house style for the whitespace around
 * it; the house style is not invented, it is *read off the container's own
 * members* — a neighbour's own leading layout, taken verbatim — which is the
 * same rule `removeMember` already holds when it refuses to take the next
 * member's indentation. `moveElement` is array reordering, which is a splice
 * once it is stated as one: two element bodies exchanged inside the array's own
 * layout, for the §7-significant order of `rules`.
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


/** Where a member goes when it is added to an object. */
export type Placement =
  /** Immediately after this member of the same object. */
  | { after: string }
  /** The first member of the object. */
  | { first: true }
  /** The last member of the object, which is the ordinary case. */
  | { last: true }

/**
 * Add one member to an object, in the layout the object already uses.
 *
 * **The whitespace is read, never invented.** An object indented with two
 * spaces, one with tabs and one written on a single line are three documents,
 * and a writer with a house style of its own would reformat two of them on the
 * first edit — which is exactly the whole-file diff ADR-0019 makes a human
 * read. So the run of layout in front of a neighbouring member is taken
 * verbatim and reused, and where the object has no member to learn from the
 * insertion is made inline, with one space, because that is the only shape
 * that can be produced without asserting a style the document has not shown.
 *
 * `json` is the value's bytes, written in as given — the caller shapes it, for
 * the reason `replaceValue` states.
 *
 * An array takes the same call with `name` omitted: the element's own layout
 * is a neighbour's, and the pointer of the container is the array's.
 */
export function insertMember(
  text: string,
  index: DocumentIndex,
  containerPointer: string,
  name: string | undefined,
  json: string,
  placement: Placement = { last: true }
): string {
  const container = spanAt(index, containerPointer)
  if (container === undefined) return text
  const open = text[container.valueStart]
  if (open !== '{' && open !== '[') return text
  if (open === '{' && name === undefined) return text
  if (open === '[' && name !== undefined) return text

  const members = childSpans(index, containerPointer, open === '[')
  const written = name === undefined ? json : `${JSON.stringify(name)}: ${json}`

  if (members.length === 0) {
    // Nothing to learn a style from, so nothing is claimed: one space either
    // side, inline. Whatever whitespace an empty container held is not a
    // member's layout and is not evidence of one.
    return (
      text.slice(0, container.valueStart + 1) +
      ` ${written} ` +
      text.slice(container.valueEnd - 1)
    )
  }

  const at = placementIndex(members, placement)
  // The neighbour whose layout this member borrows: the one it is written
  // beside. Its own leading run, verbatim — never the next member's, which is
  // the mistake `removeMember` documents in the other direction.
  const model = members[Math.min(at, members.length - 1)]!
  const layout = leadingLayout(text, model.memberStart)

  if (at >= members.length) {
    const last = members[members.length - 1]!
    return text.slice(0, last.memberEnd) + ',' + layout + written + text.slice(last.memberEnd)
  }
  const before = members[at]!
  const start = before.memberStart - layout.length
  return text.slice(0, start) + layout + written + ',' + text.slice(start)
}

/**
 * Move one element of an array to another index. **The whitespace belongs to
 * the slot; only the bodies change places.**
 *
 * Rule order is §7-significant: `rules[1]` firing before `rules[2]` is a fact
 * about the document, so moving a rule is an edit to what the pack decides and
 * not a tidy. It is a splice like every other edit here — the element's bytes
 * are lifted out of their place and put back at another one — and the run in
 * front of a position, and the run between a position and its comma, stay
 * where they are. So an array written with unequal indentation keeps the shape
 * it was written in: the element that lands in slot 1 is written where slot 1
 * was, rather than dragging its old column across and leaving the array
 * ragged in a new place. It is the array's shape that survives a move, not
 * each element's own.
 *
 * Out-of-range indices and a move to where the element already is return the
 * text unchanged: nothing happened, and inventing a change would be a diff
 * over a gesture that did nothing.
 */
export function moveElement(
  text: string,
  index: DocumentIndex,
  arrayPointer: string,
  from: number,
  to: number
): string {
  const container = spanAt(index, arrayPointer)
  if (container === undefined || text[container.valueStart] !== '[') return text
  const elements = childSpans(index, arrayPointer, true)
  if (from === to) return text
  if (from < 0 || to < 0 || from >= elements.length || to >= elements.length) return text

  // Each element as its own bytes, and the whitespace around each position as
  // its own strings: the run in front of it, and the run between it and the
  // comma that follows. Both are the *position's* and neither travels with a
  // body — see the note above — and both are re-emitted verbatim, so a move
  // changes nothing outside the bodies it exchanged.
  const parts = elements.map((span, position) => ({
    layout: leadingLayout(text, span.memberStart),
    trail: trailingLayout(text, span.memberEnd, position === elements.length - 1),
    body: text.slice(span.memberStart, span.memberEnd)
  }))
  const bodies = parts.map((part) => part.body)
  const [moved] = bodies.splice(from, 1)
  bodies.splice(to, 0, moved!)

  const first = elements[0]!
  const last = elements[elements.length - 1]!
  const start = first.memberStart - parts[0]!.layout.length
  const rebuilt = bodies
    .map((body, position) => {
      const slot = parts[position]!
      const separator = position === bodies.length - 1 ? '' : `${slot.trail},`
      return slot.layout + body + separator
    })
    .join('')
  return text.slice(0, start) + rebuilt + text.slice(last.memberEnd)
}

/** The direct children of one container, in document order. */
function childSpans(index: DocumentIndex, container: string, array: boolean): Span[] {
  const prefix = container === '' ? '/' : `${container}/`
  const found: Span[] = []
  for (const [pointer, span] of index.spans) {
    if (!pointer.startsWith(prefix)) continue
    const rest = pointer.slice(prefix.length)
    if (rest.includes('/')) continue
    if (array && !/^(0|[1-9][0-9]*)$/.test(rest)) continue
    found.push(span)
  }
  found.sort((left, right) => left.memberStart - right.memberStart)
  return found
}

/**
 * The run of whitespace between one element and the comma after it.
 *
 * `{ "id": "one" } ,` is legal JSON, and that space is a byte the author wrote.
 * Joining the rebuilt bodies with a bare `,` dropped it — an edit changing a
 * byte it did not name, in the one module whose whole claim is that it does
 * not. The last element has no comma of its own, and everything after it is
 * outside the region this rebuilds.
 */
function trailingLayout(text: string, memberEnd: number, last: boolean): string {
  if (last) return ''
  let ahead = memberEnd
  while (ahead < text.length && isSpace(text[ahead]!)) ahead += 1
  return text[ahead] === ',' ? text.slice(memberEnd, ahead) : ''
}

/** The run of whitespace immediately in front of one member. */
function leadingLayout(text: string, memberStart: number): string {
  let back = memberStart - 1
  while (back >= 0 && isSpace(text[back]!)) back -= 1
  return text.slice(back + 1, memberStart)
}

function placementIndex(members: readonly Span[], placement: Placement): number {
  if ('first' in placement) return 0
  if ('after' in placement) {
    // The pointer of the member to follow, matched against the children by
    // their own spans rather than by re-deriving the address.
    const found = members.findIndex((span) => span.pointer === placement.after)
    return found < 0 ? members.length : found + 1
  }
  return members.length
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
