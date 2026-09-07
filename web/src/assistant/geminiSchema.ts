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
 * What the **SDK-backed engine** removes on top of the desk's own list, at the
 * pinned `@ai-sdk/google@4.0.64`.
 *
 * **Declared, because the alternative is a silent narrowing.** That provider
 * does not send the schema it is given: it rebuilds it through
 * `convertJSONSchemaToOpenAPISchema`, which copies an allow-list of keywords and
 * drops the rest. So `pattern`, `maximum`, `uniqueItems`, the conditionals and
 * the annotations below never reach the model on that engine, and the desk's
 * closed six-keyword ruling was true of `builtin` and false of `vercel` — two
 * engines showing the model two different contracts, which is the exact
 * cross-engine contradiction the ruling exists to prevent.
 *
 * The desk cannot stop it: the conversion is inside the provider, below the one
 * seam this adapter has. What it can do is **say so** — here, in the README, and
 * to the author on the stream (`narrowingNotice`) — and hold the statement to
 * the version that is installed.
 *
 * **What is held, and what is not.** The conformance session derives this set
 * from what that engine actually puts on the wire, over a fixture whose keyword
 * vocabulary is pinned to the **recorded runtime 0.19.0's own** — the schemas it
 * served on `tools/list` and the pack schema its `get_schema` answered. Every
 * keyword in that vocabulary is measured, and an SDK that starts or stops
 * dropping one is a red test. The remaining entries below were measured the same
 * way against a wider synthetic schema and are kept because they are true and
 * useful, but they are **outside the provenance lock**: a runtime that never
 * emits `maximum` gives this desk no way to notice if the provider stopped
 * dropping it. The README states that boundary rather than leaving it implied.
 */
export const SDK_SCHEMA_REMOVALS: readonly string[] = [
  '$comment',
  // **A rewrite seen from a keyword's point of view.** The provider inlines a
  // `$ref` and drops the `$defs` it resolved, so the *constraint* survives and
  // these two keywords do not. They are declared because what this list states
  // is which keywords reach the model, and neither of them does; the README
  // names them as the rewrite they are.
  '$defs',
  '$ref',
  'contains',
  'default',
  'dependentRequired',
  'deprecated',
  'else',
  'exclusiveMaximum',
  'exclusiveMinimum',
  'if',
  'maxLength',
  'maximum',
  'minimum',
  'multipleOf',
  'not',
  'nullable',
  'pattern',
  'prefixItems',
  'propertyNames',
  'readOnly',
  'then',
  'title',
  'uniqueItems',
  'writeOnly'
]

/**
 * **What `@ai-sdk/google@4.0.64` does with a thought part that has no text**, and
 * it is not "carry it": it drops the part and the signature on it.
 *
 * Measured, not assumed: the provider turns a `text` part into a reasoning part
 * only when the text is non-empty, and attaches the signature of an empty one to
 * whichever text block is open — of which there is none when the summary was
 * never streamed. So the part never reaches the desk, cannot be ledgered, cannot
 * be replayed, and cannot be counted as reasoning.
 *
 * **The consequences are stated rather than left to be met.** On the SDK-backed
 * engine, against an endpoint that emits an empty signed thought and enforces
 * the wire's own rule that signed parts come back, the continuation is refused
 * and the session ends with the endpoint's status — measured by a conformance
 * leg, which is written to go red the day the provider starts carrying them.
 * And *this model always thinks* cannot be inferred from an empty signed
 * thought on that engine, because the desk is never told one arrived. The
 * built-in engine has neither limit: it reads the wire itself.
 *
 * This is the same shelf as `SDK_SCHEMA_REMOVALS` — a thing the provider does
 * below the one seam this adapter has, declared here so that it is a known
 * difference between two engines rather than a surprise.
 */
export const SDK_DROPS_EMPTY_SIGNED_THOUGHTS = true

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
export function withoutKeywords(
  schema: unknown,
  removals: readonly string[],
  inNameMap = false
): unknown {
  if (Array.isArray(schema)) {
    const walked = schema.map((item) => withoutKeywords(item, removals, false))
    return walked.some((item, at) => item !== schema[at]) ? walked : schema
  }
  if (!isRecord(schema)) return schema
  const out: Record<string, unknown> = {}
  let changed = false
  for (const [key, value] of Object.entries(schema)) {
    if (!inNameMap && removals.includes(key)) {
      changed = true
      continue
    }
    // **A key inside a name map is a name, so its value is an ordinary schema.**
    // Reading `namesUnder` on it would ask whether the *author's word* is a
    // name-map keyword — and a definition called `patternProperties` would have
    // its own children read as names, so a keyword inside it would survive.
    const walked = withoutKeywords(value, removals, inNameMap ? false : namesUnder(key))
    if (walked !== value) changed = true
    out[key] = walked
  }
  return changed ? out : schema
}

/** The desk's own removal, which is the one every engine on this family makes. */
export function withoutUnsupportedKeywords(schema: unknown, inNameMap = false): unknown {
  return withoutKeywords(schema, GEMINI_SCHEMA_REMOVALS, inNameMap)
}

/**
 * The keywords one engine's model never sees, on one family.
 *
 * The desk's closed list on every engine, plus — on the SDK-backed one — what
 * its provider removes underneath. Nothing at all on the two families whose
 * wires take JSON Schema as written.
 */
export function keywordsNotShown(engine: string, family: string): readonly string[] {
  if (family !== 'gemini') return []
  return engine === 'vercel'
    ? [...GEMINI_SCHEMA_REMOVALS, ...SDK_SCHEMA_REMOVALS]
    : GEMINI_SCHEMA_REMOVALS
}

/**
 * **The second thing the SDK-backed engine's provider does, and it is not a
 * keyword.** A schema that declares an object with no properties is dropped
 * whole: the tool is declared to the model with no `parameters` member at all.
 *
 * The runtime's `list_examples` is exactly that shape, so this is a rule with a
 * subject rather than one waiting for a hypothetical — and stating it is the
 * difference between "the model is shown the contract minus these keywords" and
 * a sentence that is false for one tool in five. Mirrored from the provider's
 * own `isEmptyObjectSchema` at the pinned version, and held to it by the
 * derivation leg, which asserts deep equality against what actually arrived.
 */
function isEmptyObjectSchema(schema: unknown): boolean {
  if (!isRecord(schema)) return false
  const properties = schema.properties
  return (
    schema.type === 'object' &&
    (properties === undefined ||
      properties === null ||
      (isRecord(properties) && Object.keys(properties).length === 0)) &&
    !schema.additionalProperties
  )
}

/**
 * The schema this engine's model is actually shown, out of the one the runtime
 * served.
 *
 * **This is the claim the wire test asserts deep equality against**, which is
 * what makes "the model is shown the runtime's contract minus exactly these
 * keywords" a measurement rather than a sentence. A narrowing outside this
 * function is a red test.
 */
export function schemaShown(engine: string, family: string, served: unknown): unknown {
  const removals = keywordsNotShown(engine, family)
  if (removals.length === 0) return served
  if (engine === 'vercel' && isEmptyObjectSchema(served)) return undefined
  return withoutKeywords(served, removals)
}

/**
 * Whether this engine declares the tool with no `parameters` at all.
 *
 * A different sentence from "these keywords are missing", because it is a
 * different thing: the model is shown a tool that takes nothing, where the
 * runtime declared one that takes an object.
 */
export function shownWithoutParameters(engine: string, family: string, served: unknown): boolean {
  return keywordsNotShown(engine, family).length > 0 && engine === 'vercel' && isEmptyObjectSchema(served)
}

/** The line the author reads where a tool is declared with no parameters. */
export const NO_PARAMETERS_NOTICE =
  'declared to the model with no parameters at all: this wire takes an OpenAPI subset and ' +
  'this engine omits an object schema with no properties. The ToolGate and the runtime hold ' +
  'the contract the runtime actually enforces, whatever the model was shown.'

/**
 * The keywords one tool loses **beyond the desk's own declared ruling**, or none.
 *
 * **The difference between two schemas and not a filter over one**, and that is
 * the whole of the fix: the first version compared the runtime's raw schema
 * against a removal set that *included* the desk's own six, so every tool the
 * runtime serves produced a notice about `additionalProperties` — a warning
 * about the ruling itself rather than about anything lost on top of it, on both
 * engines, five times a run. A notice has to mean "your contract lost something
 * this desk did not already tell you about in the README".
 *
 * So: what the **desk** shows, minus what **this engine** shows. On `builtin`
 * those are the same schema and the answer is empty. On `vercel` it is exactly
 * what that provider removes underneath, and nothing else.
 */
export function keywordsLost(engine: string, family: string, served: unknown): string[] {
  if (keywordsNotShown(engine, family).length === 0) return []
  const desk = withoutUnsupportedKeywords(served)
  const shown = schemaShown(engine, family, desk)
  const before = keywordsSent(desk)
  const after = keywordsSent(shown)
  return [...before].filter((keyword) => !after.has(keyword)).sort()
}

/**
 * The line the author reads where a tool's contract was narrowed for the model.
 *
 * **Never silent, and this is the whole of the (d) half of the ruling.** A desk
 * that removed a keyword and said nothing would leave an author reading a
 * proposal without knowing the model had been shown a wider contract than the
 * runtime enforces — and, on the SDK-backed engine, a *different* contract from
 * the one the other engine shows. One line per tool that actually lost
 * something, at the start of the run, before the model is asked anything.
 */
export function narrowingNotice(keywords: readonly string[]): string {
  return (
    `shown to the model without: ${[...keywords].sort().join(', ')}. This wire takes an ` +
    `OpenAPI subset, so those keywords cannot travel; the ToolGate and the runtime hold the ` +
    `contract the runtime actually enforces, whatever the model was shown.`
  )
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
