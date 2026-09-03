/**
 * Reading, amending and re-serialising `jpack.json` — **without
 * re-implementing the runtime's schema.**
 *
 * The runtime owns what a project configuration means. This module decodes
 * nothing beyond the shape it has to touch to add one key, validates nothing
 * at all, and carries every other member through unchanged in value and in key
 * order. A desk that re-derived the schema here would be a second answer to a
 * question the runtime already answers, and the two would drift on the first
 * version bump.
 *
 * **Not byte for byte, and the difference is worth stating.** The round trip
 * is through `JSON.parse`, so a number is carried as a double: a 20-digit
 * integer comes back rounded, `1.0` comes back `1`, `1e2` comes back `100`,
 * and duplicate keys collapse to the last. Every member `jpack.json` carries
 * today is a string or an object, so nothing in the file is subject to it —
 * but a future numeric member would be, and that is the sentence that has to
 * be here rather than the reassuring one.
 *
 * # What a pack entry may carry, and why this writes so little
 *
 * From `internal/project/jpack.schema.json` in the runtime (v0.19.0), whose
 * `pack` def is `additionalProperties: false` with `required: ["path"]`, and
 * from `internal/project/project.go`'s `Pack` struct where `Path` is bare and
 * `Matrix`, `Description`, `ExpectedVersion`, `Facts` and `Evidence` are all
 * `omitempty`:
 *
 * - **`path`** is the only required member, and the only one that is not a
 *   claim about the document's content.
 * - **`description`** carries `minLength: 1`, so a blank description omits the
 *   member rather than writing `""` — which the schema refuses.
 * - **`expectedVersion`** matches the semver pattern and is a *validated
 *   reference, never an independent truth*: `validate.go` reports a difference
 *   from the document's own `version` as a FAILED check. So the desk writes it
 *   only because it also writes `version` into the document, and both come
 *   from `NEW_PACK_VERSION` below. Two constants would be one bump away from
 *   failing `packs validate` on every pack the desk ever created.
 * - **`matrix` is deliberately not written.** There is no matrix, and a
 *   `matrix` naming a file that does not exist is a validation failure the
 *   desk would have authored.
 * - **`facts` and `evidence` are not written.** They are agent hints keyed by
 *   pointers and ids the document does not yet have, and a hint keyed to
 *   nothing is a failed check too.
 *
 * The entry's key is a `decisionId`, `^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$` — which
 * is where the slug rule comes from and why it is what it is.
 *
 * **`configVersion` is never touched.** `packs` exists in all three shapes
 * (`SupportedConfigVersions()` is `{"1","2","3"}`), so amending a `"1"` or
 * `"2"` project must leave it at `"1"` or `"2"`: bumping it would break a
 * runtime that reads only the earlier shapes, for a member that needed no
 * bump. `graphs` and `audit` are carried through untouched for the same
 * reason.
 */

/**
 * The version a new pack's document declares — and therefore the version its
 * entry pins. One constant, read in both places. See `expectedVersion` above.
 */
export const NEW_PACK_VERSION = '0.1.0'

/** One entry under `packs`, as this desk writes one. */
export interface PackEntry {
  path: string
  description?: string
  expectedVersion: string
}

/** The configuration as a generic object. Nothing here is decoded further. */
export type ProjectConfig = Record<string, unknown>

export class ProjectConfigProblem extends Error {}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/**
 * Parse `jpack.json` far enough to add one key to it, and no further.
 *
 * Two refusals, both about the shape this module must touch: the file has to
 * be a JSON object, and `packs` — if it is there at all — has to be one too.
 * Everything else is the runtime's business, including whether the result is a
 * configuration the runtime would accept.
 */
export function parseProjectConfig(text: string): ProjectConfig {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch (cause) {
    throw new ProjectConfigProblem(`jpack.json is not valid JSON (${String(cause)})`)
  }
  if (!isPlainObject(parsed)) {
    throw new ProjectConfigProblem('jpack.json is not a JSON object')
  }
  if ('packs' in parsed && !isPlainObject(parsed.packs)) {
    throw new ProjectConfigProblem('jpack.json has a packs member that is not an object')
  }
  return parsed
}

/** The pack keys this project already uses. */
export function existingPackKeys(config: ProjectConfig): string[] {
  const packs = config.packs
  return isPlainObject(packs) ? Object.keys(packs) : []
}

/** The paths this project's entries already name. */
export function existingPackPaths(config: ProjectConfig): string[] {
  const packs = config.packs
  if (!isPlainObject(packs)) return []
  return Object.values(packs)
    .map((entry) => (isPlainObject(entry) && typeof entry.path === 'string' ? entry.path : undefined))
    .filter((path): path is string => path !== undefined)
}

/**
 * The configuration with one entry added, everything else in place.
 *
 * Key order survives: spreading assigns in insertion order, and an existing
 * `packs` keeps its original position because the explicit member replaces its
 * value rather than re-inserting the key. The new slug lands last inside
 * `packs`, and it can never be reordered as an array index because a slug must
 * begin with a lowercase letter.
 */
export function withPack(
  config: ProjectConfig,
  slug: string,
  entry: PackEntry
): ProjectConfig {
  const packs = isPlainObject(config.packs) ? config.packs : {}
  return { ...config, packs: { ...packs, [slug]: entry } }
}

/** One entry for a pack the desk is creating now. */
export function packEntryFor(path: string, description: string): PackEntry {
  const trimmed = description.trim()
  return {
    path,
    // `minLength: 1`: a blank description omits the member rather than writing
    // an empty string the schema refuses.
    ...(trimmed === '' ? {} : { description: trimmed }),
    expectedVersion: NEW_PACK_VERSION
  }
}

/**
 * Re-encode, in the shape the file was already written in.
 *
 * The indent, the line ending and the trailing newline are taken from the
 * source text. Without that, the first pack a project creates silently
 * reformats a hand-authored file wholesale, and the diff its maintainer
 * reviews is every line rather than the four that were added.
 *
 * The line ending is not a detail: `JSON.stringify` emits `\n` only, so a
 * CRLF `jpack.json` — ordinary on a Windows checkout, which the chassis
 * supports — would come back with every line changed. That is precisely the
 * whole-file rewrite this function exists to avoid, arriving through the one
 * axis the indent does not cover.
 */
export function serialiseProjectConfig(source: string, config: ProjectConfig): string {
  const eol = source.includes('\r\n') ? '\r\n' : '\n'
  const encoded = JSON.stringify(config, null, indentOf(source)).replace(/\n/g, eol)
  return source.endsWith('\n') ? `${encoded}${eol}` : encoded
}

/**
 * The indent the file already uses: whatever sits between the first newline
 * and the first member on that line. Two spaces where there is nothing to
 * measure — a one-line file has no indent to preserve, and two is what the
 * runtime's own fixtures use.
 *
 * The *first* indented line, which is a top-level member in any file whose
 * members are each on their own line. A file that opens with everything
 * collapsed onto line one and indents only some nested object later would have
 * that nesting's indent applied to its top level; it is measured rather than
 * parsed because a file already written the ordinary way is the case worth
 * getting right, and a collapsed file has no formatting to preserve anyway.
 */
function indentOf(source: string): string | number {
  const match = source.match(/\n([ \t]+)"/)
  return match ? match[1]! : 2
}
