/**
 * The one thing the desk does to a served schema, and the whole of it.
 *
 * **The rule everywhere else is that the runtime's `inputSchema` travels
 * untouched.** `engines/contract.ts`'s `servedSchema` refuses to invent one, no
 * engine writes one, and an enforcement test sweeps the engine sources for a
 * schema literal — because the model is shown the contract the runtime enforces
 * or it is shown nothing.
 *
 * The native Gemini wire cannot keep that promise whole. A function
 * declaration's `parameters` is an **OpenAPI subset**, not JSON Schema, and an
 * endpoint answers 400 to keywords an ordinary schema carries — the runtime's
 * own five all carry `additionalProperties: false`, so on this family the
 * choice is between removing something and not running at all.
 *
 * **So: a closed, documented removal list, on this family only, and nothing
 * else is rewritten.** The keywords below are taken off; every other keyword
 * travels exactly as the runtime served it. Nothing is added, nothing is
 * re-typed, no value is changed, and no schema is written here.
 *
 * **What that costs, stated rather than glossed.** Each removal makes the
 * contract the model is *shown* wider than the one the runtime enforces:
 * without `additionalProperties: false` a member nobody declared looks
 * acceptable, and without `const` a fixed value looks free. It changes nothing
 * about what is enforced — the ToolGate rewrites and refuses on the wire, and
 * the runtime validates every call it receives — so the worst case is a model
 * that proposes a call the runtime then refuses, which is a turn spent rather
 * than a guarantee lost. The README says this in the same words.
 *
 * **A keyword this list does not name is never removed.** An endpoint that
 * refuses one produces an `error` event naming it — see `refusedSchemaKeyword`
 * — because a desk that quietly widened the list on being refused would be a
 * desk whose removal list is whatever the last endpoint disliked.
 */

/**
 * The keywords removed from a served schema on the `gemini` family, and the
 * whole of them.
 *
 * Read against the API reference's function-declaration section on 2026-09-06
 * (see the README for the pages and the date): `parameters` is "a subset of the
 * OpenAPI schema", and these six are the JSON-Schema keywords a runtime's
 * schema realistically carries that the subset has no place for.
 *
 * `oneOf` is deliberately **absent**. It is refused by some models and not
 * others, which makes it exactly the case this list must not grow to cover: a
 * union removed is a union that reads as "anything at all", so a contract that
 * said "one of these three" would be shown as unconstrained. It travels, and an
 * endpoint that refuses it is reported by name.
 */
export const GEMINI_SCHEMA_REMOVALS: readonly string[] = [
  '$schema',
  '$id',
  'additionalProperties',
  'const',
  'examples',
  'patternProperties'
]

/**
 * Whether a value is a plain object this walk should descend into.
 *
 * Arrays are walked as arrays; everything else is a leaf and is returned by
 * identity, because a leaf is a value the runtime wrote and this module
 * rewrites no values.
 */
function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

/**
 * The keywords whose **keys are names somebody wrote**, not keywords.
 *
 * **Two of these were missing and the omission deleted a definition.** The walk
 * knew `properties` and `$defs`; JSON Schema has more places where an author's
 * word is a key, and a schema with `{"definitions": {"const": {…}}}` had its
 * definition *called* `const` removed as though it were the keyword — leaving
 * `{"$ref": "#/definitions/const"}` pointing at nothing. A dangling reference is
 * worse than the keyword this list exists to take out.
 *
 * `patternProperties` is here for the same reason and is on the removal list
 * besides: its keys are regular expressions, which are as much the author's as a
 * property name is. `dependentRequired` and `dependentSchemas` key on property
 * names. A map this list does not know is walked as a schema, which is the
 * conservative half of the mistake — a keyword inside it is still removed — and
 * the list is what stops the *keys* being read as keywords.
 */
const NAME_MAPS: readonly string[] = [
  'properties',
  '$defs',
  'definitions',
  'dependentSchemas',
  'dependentRequired',
  'patternProperties'
]

/** Whether this key's own object holds authored names rather than keywords. */
function namesUnder(key: string): boolean {
  return NAME_MAPS.includes(key)
}

/**
 * The served schema, minus exactly the keywords above, at every depth.
 *
 * **A structural walk and not a filter over one level.** A schema's keywords
 * appear inside `properties`, inside `items`, inside `$defs` and inside each
 * branch of an `anyOf`, so a removal applied only at the top would leave
 * `additionalProperties` on every nested object and the endpoint would refuse
 * the request anyway.
 *
 * **The places a key is not a keyword are the name maps** — see `NAME_MAPS` —
 * where the keys are the author's words rather than JSON Schema's: a document
 * with a member actually called `const`, or a definition of that name, must keep
 * it. So the walk carries whether it is standing in a map of names, and removes
 * nothing there — it only descends into the schemas the names point at.
 *
 * Nothing is copied that does not have to be: a subtree with no removal in it
 * is returned by identity, so the object the runtime served is the object that
 * travels wherever this changed nothing.
 */
export function withoutUnsupportedKeywords(schema: unknown, inNameMap = false): unknown {
  if (Array.isArray(schema)) {
    const walked = schema.map((item) => withoutUnsupportedKeywords(item, false))
    return walked.some((item, at) => item !== schema[at]) ? walked : schema
  }
  if (!isRecord(schema)) return schema
  const out: Record<string, unknown> = {}
  let changed = false
  for (const [key, value] of Object.entries(schema)) {
    if (!inNameMap && GEMINI_SCHEMA_REMOVALS.includes(key)) {
      changed = true
      continue
    }
    // **A key inside a name map is a name, so its value is an ordinary schema.**
    // Reading `namesUnder` on it would ask whether the *author's word* is a
    // name-map keyword — and a definition called `patternProperties` would have
    // its own children read as names, so a keyword inside it would survive.
    const walked = withoutUnsupportedKeywords(value, inNameMap ? false : namesUnder(key))
    if (walked !== value) changed = true
    out[key] = walked
  }
  return changed ? out : schema
}

/**
 * Every keyword the desk actually sent in one schema, at every depth.
 *
 * Used to decide whether a refusal is *about the schema this desk composed*, on
 * exactly the reasoning `assistant/thinking.ts` uses for the tier: the closed
 * list is not a list somebody wrote of keywords endpoints dislike, it is the
 * set of names this request actually carried. A refusal naming none of them is
 * an ordinary model error and is reported as one.
 *
 * Authored names are excluded for the reason the walk excludes them — the same
 * `NAME_MAPS`, so the two classifications cannot disagree: a member called
 * `title`, or a definition called `const`, is the author's word and not a schema
 * keyword, and matching a refusal against it would let a document's own
 * vocabulary decide what an error meant.
 */
export function keywordsSent(schema: unknown, into?: Set<string>, inNameMap = false): Set<string> {
  const found = into ?? new Set<string>()
  if (Array.isArray(schema)) {
    for (const item of schema) keywordsSent(item, found, false)
    return found
  }
  if (!isRecord(schema)) return found
  for (const [key, value] of Object.entries(schema)) {
    if (!inNameMap) found.add(key)
    keywordsSent(value, found, inNameMap ? false : namesUnder(key))
  }
  return found
}

/**
 * The schema keyword an endpoint refused, or `''`.
 *
 * **Two conditions, and both are required**, exactly as `unsupportedThinking`
 * has: a 400 — the status every provider documents for a member it will not
 * take — whose message names a keyword this desk **actually sent**. A refusal
 * that names none of them is not about the schema.
 *
 * The longest match wins, because `properties` contains `type` and a shorter
 * name matching first would report the wrong keyword to a person about to go
 * and look at it.
 *
 * **Nothing here quotes the body beyond matching it.** The keyword is the
 * desk's own word — it came out of the schema the desk composed — and the
 * sentence a reader gets is `refusedSchemaSentence`'s.
 */
export function refusedSchemaKeyword(
  status: number,
  message: string,
  schemas: readonly unknown[]
): string {
  if (status !== 400) return ''
  const sent = new Set<string>()
  for (const schema of schemas) keywordsSent(schema, sent)
  const named = [...sent].filter((keyword) => message.includes(keyword))
  named.sort((left, right) => right.length - left.length)
  return named[0] ?? ''
}

/**
 * The sentence a person reads when an endpoint refuses a keyword the desk sent.
 *
 * **An error and not a strip**, and that is the whole ruling: the removal list
 * is closed and reviewed, so a keyword outside it that an endpoint will not
 * take is reported rather than quietly taken off. The alternative — widen the
 * list at run time — is a desk whose idea of the runtime's contract is decided
 * by whichever endpoint complained last, and the model would be shown a
 * contract nobody wrote down.
 */
export function refusedSchemaSentence(keyword: string): string {
  return (
    `the endpoint refused the tool schema over ${JSON.stringify(keyword)}, which the runtime ` +
    `served and this desk does not remove. The removal list for this wire is closed and ` +
    `documented (${GEMINI_SCHEMA_REMOVALS.join(', ')}); nothing was written, and nothing was ` +
    `stripped from the contract the runtime enforces.`
  )
}
