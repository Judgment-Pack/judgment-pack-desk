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
 * The id derived from a name, live and under the field.
 *
 * Lowercase, every run outside `[a-z0-9]` becomes one `-`, leading and
 * trailing separators trimmed. The result is then held to the runtime's own
 * pattern rather than to a rule invented here.
 */
export function slugFor(name: string): SlugResult {
  const slug = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
  if (slug === '') return { problem: 'A name needs at least one letter or number.' }
  if (!/^[a-z]/.test(slug)) return { problem: 'A name must start with a letter.' }
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

/** Why this name cannot be used here, if it cannot. */
export function collisionIn(
  slug: string,
  project: { keys: readonly string[]; files: readonly string[]; path: string }
): string | undefined {
  if (project.keys.includes(slug)) {
    return `This project already has a pack called ${slug}.`
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
 * incomplete. What comes out is a valid starting point to edit, not a document
 * that claims to be finished.
 */
export function emptyPackFrom(schemaText: string | undefined): string {
  const skeleton: Record<string, unknown> = {}
  const schema = readSchema(schemaText)
  if (schema) {
    for (const name of schema.required) {
      skeleton[name] = emptyFor(schema.properties[name], schema.defs)
    }
  }
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
