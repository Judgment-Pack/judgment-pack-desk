/**
 * What a new pack is called, where it goes, and what its first bytes are.
 *
 * Pure functions: no React, no fetch, no state. Everything the create dialog
 * decides behind the scenes is decided here, so it can be read and tested
 * without rendering anything.
 *
 * **The slug rule is the runtime's, not the desk's.** A `packs` key is a
 * `decisionId` — `^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$` in the runtime's
 * `jpack.schema.json` — so a name that cannot become one is refused with a
 * plain sentence rather than repaired. Prefixing a letter to make `2024 review`
 * into `a-2024-review` would be the desk inventing half the id.
 */
import { NEW_PACK_VERSION } from './jpackConfig'

/** A slug, or the reason there is not one. Never both. */
export type SlugResult = { slug: string } | { problem: string }

/**
 * The longest id this desk will derive.
 *
 * Not a rule of the format — it is what a name can be before the file it names
 * cannot exist. A pack is written as `<slug>.pack.json`, ten characters of
 * suffix, and a single name component is capped at 255 bytes on every
 * filesystem the desk runs on. A slug is ASCII by construction, so bytes and
 * characters are the same count here. Refusing at 246 in the field is the same
 * refusal the write would answer with, said where it can be acted on.
 */
export const MAX_SLUG_LENGTH = 245

/**
 * The id derived from a name, live and under the field.
 *
 * Diacritics are folded first — `Überprüfung` is `uberprufung`, not
 * `berpr-fung` — because dropping a letter is a worse answer than transposing
 * one. Then lowercase, every run outside `[a-z0-9]` becomes one `-`, leading
 * and trailing separators trimmed. The result is held to the runtime's own
 * pattern rather than to a rule invented here.
 *
 * **A name in another script is refused, and the refusal says why.** The
 * runtime's `decisionId` is `^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$` and carries no
 * others, so `決裁レビュー` cannot become one — but telling its author that
 * their name "needs a letter" is a false statement about a name made of
 * letters. The sentence states the alphabet instead.
 */
export function slugFor(name: string): SlugResult {
  const slug = name
    .normalize('NFKD')
    .replace(/\p{Diacritic}/gu, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
  if (slug === '') {
    return { problem: 'A name needs at least one letter a–z or digit 0–9 — an id can carry no others.' }
  }
  if (!/^[a-z]/.test(slug)) return { problem: 'A name must start with a letter.' }
  if (slug.length > MAX_SLUG_LENGTH) {
    return { problem: `A name is too long: an id can be at most ${MAX_SLUG_LENGTH} characters.` }
  }
  return { slug }
}

/**
 * The file a new pack is written to.
 *
 * Deliberately outside the runtime's optional `<decision-id>-<semver>.pack.json`
 * convention. A filename outside it has its filename check reported *skipped*
 * and never failed (`validate.go`), and it cannot match by accident either: the
 * convention's version group needs dots, and a slug can carry none. Inside the
 * convention the desk would be writing the pack's version into a third place,
 * which is the drift that check exists to catch.
 */
export function packPathFor(dir: string, slug: string): string {
  return `${dir}/${slug}.pack.json`
}

/**
 * Why this name cannot be used here, if it cannot.
 *
 * Three questions, and they are three: a key this project already uses, a file
 * *another entry already claims* even where nothing is on disk under it yet,
 * and a file that is on disk. The middle one is not the third one seen from
 * another angle — an entry naming a file that has not been written is an
 * ordinary state for a project under construction, and creating a second pack
 * over it would unregister the first.
 */
export function collisionIn(
  slug: string,
  project: {
    keys: readonly string[]
    paths: readonly string[]
    files: readonly string[]
    path: string
  }
): string | undefined {
  if (project.keys.includes(slug)) {
    return `This project already has a pack called ${slug}.`
  }
  if (project.paths.includes(project.path)) {
    return `Another pack in this project already uses that file.`
  }
  if (project.files.includes(project.path)) {
    return `There is already a file where this pack would be written.`
  }
  return undefined
}

/**
 * The template's own JSON, with the four members this dialog fills.
 *
 * Everything else the template carried is left exactly as the runtime served
 * it — `specVersion` above all, which is the runtime's statement about which
 * version of the format this document is written to and never the desk's.
 */
export function shapeTemplate(
  templateJson: string,
  fields: { name: string; description: string; slug: string; idBase: string }
): string {
  let parsed: unknown
  try {
    parsed = JSON.parse(templateJson)
  } catch (cause) {
    throw new Error(`the template is not valid JSON (${String(cause)})`)
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new Error('the template is not a JSON object')
  }
  const document = parsed as Record<string, unknown>
  const described = fields.description.trim()
  const shaped: Record<string, unknown> = {
    ...document,
    title: fields.name.trim(),
    // `idBase` already ends in a separator: the decoder normalises it once, so
    // this is a plain concatenation and not a second place a slash is decided.
    id: `${fields.idBase}${fields.slug}`,
    version: NEW_PACK_VERSION
  }
  // `description` is a non-empty string in JPS, so a blank one is an absent
  // member rather than an empty one — and a template that carried its own
  // description must not keep it under a new name.
  if (described === '') delete shaped.description
  else shaped.description = described
  return `${JSON.stringify(shaped, null, 2)}\n`
}

/**
 * The skeleton, derived from the runtime's own schema.
 *
 * Only the members the schema itself lists as `required`, a `const` taken
 * verbatim, and otherwise the empty value of the declared type. It **does not**
 * invent two outcomes to satisfy `minItems: 2`, or a rule to satisfy
 * `minItems: 1`: an invented outcome id would be the desk asserting what the
 * decision is, and the runtime is the thing entitled to say the document is
 * incomplete. What comes out is a **starting point to edit, and a document the
 * runtime will report incomplete until it is finished** — which is what the
 * dialog says beside the choice, rather than letting it be discovered later.
 *
 * `undefined` where there is no schema to read, or where the schema yields no
 * `specVersion`. See below.
 */
export function emptyPackFrom(schemaText: string | undefined): string | undefined {
  const schema = readSchema(schemaText)
  if (!schema) return undefined
  const skeleton: Record<string, unknown> = {}
  for (const name of schema.required) {
    skeleton[name] = emptyFor(schema.properties[name], schema.defs)
  }
  // No `specVersion`, no document. The member is the one thing that says which
  // version of the format these members are written to, and a file without it
  // is not an incomplete pack — it is not a pack. Where the schema does not
  // supply one (or there is no schema to read), this returns nothing and the
  // dialog offers no empty template rather than writing a file that can never
  // be read as one.
  if (typeof skeleton.specVersion !== 'string' || skeleton.specVersion === '') return undefined
  return `${JSON.stringify(skeleton, null, 2)}\n`
}

interface ReadSchema {
  required: string[]
  properties: Record<string, unknown>
  defs: Record<string, unknown>
}

function readSchema(text: string | undefined): ReadSchema | undefined {
  if (text === undefined) return undefined
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return undefined
  }
  if (typeof parsed !== 'object' || parsed === null) return undefined
  const schema = parsed as Record<string, unknown>
  const required = Array.isArray(schema.required)
    ? schema.required.filter((name): name is string => typeof name === 'string')
    : []
  const properties =
    typeof schema.properties === 'object' && schema.properties !== null
      ? (schema.properties as Record<string, unknown>)
      : {}
  const defs =
    typeof schema.$defs === 'object' && schema.$defs !== null
      ? (schema.$defs as Record<string, unknown>)
      : {}
  return { required, properties, defs }
}

/**
 * The empty value of one declared member.
 *
 * A local `$ref` is followed once — the schema states most members that way,
 * and a skeleton that emitted `null` for every `$ref` would be a skeleton of
 * nothing. It is followed once and not recursively: one hop is what these
 * definitions need, and a cycle would otherwise be this function's problem to
 * solve rather than the schema's to not have.
 */
function emptyFor(property: unknown, defs: Record<string, unknown>): unknown {
  const resolved = resolve(property, defs)
  if (typeof resolved !== 'object' || resolved === null) return null
  const declared = resolved as Record<string, unknown>
  // A const is the schema stating the value outright — `specVersion` is one —
  // so it is taken verbatim rather than emptied.
  if ('const' in declared) return declared.const
  switch (declared.type) {
    case 'array':
      return []
    case 'object':
      return {}
    case 'string':
      return ''
    case 'number':
    case 'integer':
      return 0
    case 'boolean':
      return false
    default:
      return null
  }
}

function resolve(property: unknown, defs: Record<string, unknown>): unknown {
  if (typeof property !== 'object' || property === null) return property
  const ref = (property as { $ref?: unknown }).$ref
  if (typeof ref !== 'string') return property
  const match = ref.match(/^#\/\$defs\/(.+)$/)
  return match ? (defs[match[1]!] ?? property) : property
}
