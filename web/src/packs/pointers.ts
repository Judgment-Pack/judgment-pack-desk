/**
 * The one address space: an RFC 6901 pointer, and the element that carries it.
 *
 * Every block the pack document renders is addressed by the pointer of the
 * member it draws, and that one string is four things at once — the block's
 * `data-pointer`, the block's element id, the fragment a deep link names, and
 * the value `?at` holds while the Inspector is looking at it. A diagnostic's
 * `instancePath` is the same string from the other end, which is the whole
 * reason there is one space rather than two.
 *
 * **Two traps, written down because both are silent.**
 *
 * An id containing `/` or `~` is perfectly legal HTML — an id may be any
 * non-empty string with no ASCII whitespace — and is *not* a valid CSS
 * selector: `#/rules/1` parses as a type selector and a class, and
 * `querySelector` throws or matches nothing. So every lookup in this desk goes
 * through `document.getElementById`, which takes a string rather than a
 * selector. Nothing here may be handed to `querySelector`.
 *
 * A fragment is percent-encoded in the address bar, so it is decoded before it
 * is compared — exactly as `shell/useHashTarget.ts` already does it, and a
 * fragment that is not valid percent-encoding names nothing rather than
 * throwing.
 */

/**
 * The pointer for a path of member names and array indices.
 *
 * The escaping mirrors the runtime's own `carrier.Pointer` byte for byte:
 * `~` becomes `~0`, `/` becomes `~1`, and an empty path is the empty string.
 * If the two ever disagree, a diagnostic anchors on nothing and the desk
 * silently stops reporting it — which is why this is one function with one
 * test rather than a template literal at each call site.
 */
export function pointer(parts: readonly (string | number)[]): string {
  if (parts.length === 0) return ''
  let value = ''
  for (const part of parts) {
    value += '/'
    for (const char of String(part)) {
      if (char === '~') value += '~0'
      else if (char === '/') value += '~1'
      else value += char
    }
  }
  return value
}

/**
 * One step down from a pointer, escaping the step.
 *
 * The reason this exists rather than a template literal: an extension's key is
 * **document data**. A namespaced `example.owner` needs no escaping and a key
 * carrying `/` or `~` does, and a call site that got it wrong would address a
 * block nothing anchors on — silently, because a diagnostic that anchors on
 * nothing is a diagnostic the desk stops reporting. A constant step is safe
 * either way and goes through here too, so there is one spelling.
 */
export function child(at: string, step: string | number): string {
  const parts = parsePointer(at)
  if (parts === undefined) return at
  return pointer([...parts, step])
}

/**
 * The inverse. Anything that is neither empty nor `/`-prefixed is refused
 * rather than repaired: a value that is not a pointer names nothing, and
 * guessing which member it meant would put a diagnostic on the wrong block.
 *
 * **An illegal escape is refused too**, and that is the half this used to
 * repair. RFC 6901 admits exactly two escapes, `~0` and `~1`; `replaceAll`
 * left every other `~` in place, so `~2` and a trailing bare `~` parsed as
 * ordinary characters and named a member nobody wrote. A pointer that is not a
 * pointer has to name nothing, or a diagnostic lands on whatever it happens to
 * resemble.
 */
export function parsePointer(text: string): string[] | undefined {
  if (text === '') return []
  if (!text.startsWith('/')) return undefined
  const tokens = text.slice(1).split('/')
  const parts: string[] = []
  for (const token of tokens) {
    // Every `~` must be followed by `0` or `1`. Scanned rather than matched by
    // a regex so the unescaping and the validation are one pass and cannot
    // disagree about what they saw.
    let decoded = ''
    for (let index = 0; index < token.length; index += 1) {
      const char = token[index]!
      if (char !== '~') {
        decoded += char
        continue
      }
      const next = token[index + 1]
      if (next === '0') decoded += '~'
      else if (next === '1') decoded += '/'
      else return undefined
      index += 1
    }
    parts.push(decoded)
  }
  return parts
}

/** Whether one token is a legal RFC 6901 array index: no `01`, no `1e0`, no `-0`. */
function arrayIndex(token: string): number | undefined {
  if (!/^(0|[1-9][0-9]*)$/.test(token)) return undefined
  return Number(token)
}

/**
 * The value one pointer names inside a document, or undefined.
 *
 * **The one evaluator.** There were three, and they disagreed in ways that
 * showed the Inspector a subtree the address did not name: `Number(part)`
 * accepted `01`, `1e0`, `-0` and the empty string as array indices, so
 * `/rules/01` and `/rules/1e0` both selected rule one; `part in value` consults
 * the prototype chain, so `/constructor` and `/toString` selected properties
 * that are in no JSON document. An address either names something in this
 * document or names nothing, and there is one place that decides which.
 */
export function valueAt(document: unknown, at: string): unknown {
  const parts = parsePointer(at)
  if (parts === undefined) return undefined
  let value: unknown = document
  for (const part of parts) {
    if (Array.isArray(value)) {
      const index = arrayIndex(part)
      // Bounds-checked: `/rules/9` in a document with two rules names nothing,
      // and `undefined` from a read past the end is not the same answer.
      if (index === undefined || index >= value.length) return undefined
      value = value[index]
      continue
    }
    if (typeof value === 'object' && value !== null && Object.hasOwn(value, part)) {
      value = (value as Record<string, unknown>)[part]
      continue
    }
    return undefined
  }
  return value
}

/** Whether a pointer names something this document actually declares. */
export function declaredAt(document: unknown, at: string): boolean {
  return valueAt(document, at) !== undefined
}

/**
 * The ancestor chain, longest first, not including the pointer itself.
 *
 * This is the order diagnostic anchoring walks: the nearest rendered ancestor
 * is wanted, so the walk stops at the first hit rather than at the last. The
 * root's empty pointer is the final entry, and it is a real address — the
 * document's own strip.
 */
export function parentPointers(text: string): string[] {
  const parts = parsePointer(text)
  if (parts === undefined) return []
  const chain: string[] = []
  for (let length = parts.length - 1; length >= 0; length -= 1) {
    chain.push(pointer(parts.slice(0, length)))
  }
  return chain
}

/**
 * The element id for a pointer, or undefined where there can be none.
 *
 * The id **is** the pointer, verbatim, so `data-pointer="/rules/1"` and the
 * deep link `#/rules/1` are one string and no mapping table can drift. The
 * root's pointer is the empty string, which is not a legal id, so the root
 * carries `data-pointer=""` and no id at all — and nothing deep-links to the
 * document as a whole.
 */
export function elementIdFor(text: string): string | undefined {
  return text === '' ? undefined : text
}

/**
 * The pointer a URL fragment names, or undefined.
 *
 * Decoded first, because the address bar percent-encodes one. A fragment that
 * is not valid percent-encoding is not a pointer, and neither is one that does
 * not start with `/` — `#shortcuts` is the Help page's, and it must not be
 * read as an address into a pack.
 */
export function pointerFromHash(hash: string): string | undefined {
  if (hash.length < 2) return undefined
  let decoded: string
  try {
    decoded = decodeURIComponent(hash.slice(1))
  } catch {
    return undefined
  }
  return decoded.startsWith('/') ? decoded : undefined
}
