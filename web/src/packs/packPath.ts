/**
 * Two spellings of one file, as the **runtime** decides it.
 *
 * The desk compares declared paths in exactly one place that matters — "is this
 * pack's file already claimed?" — and it was comparing strings. The runtime
 * does not: `Project.DeclaresPath` cleans each side with `fssecure.Relative`
 * and then compares the cleaned forms, and again case-folded, because a name
 * that is not on disk yet may land on a filesystem that does not distinguish
 * case. So `packs/./new.pack.json` and `packs/new.pack.json` are one file to
 * the runtime and were two to the desk — and an entry spelled the first way let
 * the desk create the second, after which two ids resolve to one document.
 *
 * This mirrors that rule and nothing else. It is deliberately not a
 * general-purpose path library: it is the smallest thing that answers the same
 * question the runtime answers, in the same order, so the two cannot drift
 * without a test here failing.
 */

/**
 * The cleaned, project-relative form of one declared path, or undefined where
 * the runtime would refuse it outright.
 *
 * `fssecure.Relative` refuses an empty path, a NUL, an absolute path, a volume
 * name, a leading separator and a remote-looking path, then `filepath.Clean`s
 * what is left and refuses a result that escapes. Interior `..` that resolves
 * back inside is *allowed* — `a/../b` is `b` — which is exactly the alias that
 * slipped past an equality check.
 */
export function cleanRelative(raw: string): string | undefined {
  if (raw === '' || raw.includes('\0')) return undefined
  // Absolute, rooted, or carrying a volume/scheme. Backslash is a separator on
  // one of the platforms this runs on and a filename character on the other,
  // so it is refused rather than guessed at, exactly as the chassis refuses it.
  if (raw.startsWith('/') || raw.startsWith('\\') || raw.includes(':')) return undefined
  const cleaned = clean(raw)
  if (cleaned === '..' || cleaned.startsWith('../')) return undefined
  if (cleaned === '.') return undefined
  return cleaned
}

/**
 * `path.Clean`, on the slash-separated forms this desk deals in.
 *
 * Written out rather than approximated with a few regexes: collapsing `//`,
 * dropping `.` and resolving `..` have to happen in one pass over the segments,
 * because each can create work for the others — `a/.//../b` is `b` only if the
 * empty segment and the dot are gone before the parent is applied.
 */
function clean(raw: string): string {
  const rooted = raw.startsWith('/')
  const out: string[] = []
  for (const segment of raw.split('/')) {
    if (segment === '' || segment === '.') continue
    if (segment === '..') {
      // A parent that has something to consume consumes it; one that does not
      // is kept, so an escape stays visible to the caller above.
      if (out.length > 0 && out[out.length - 1] !== '..') {
        out.pop()
        continue
      }
      if (rooted) continue
      out.push('..')
      continue
    }
    out.push(segment)
  }
  if (out.length === 0) return rooted ? '/' : '.'
  return (rooted ? '/' : '') + out.join('/')
}

/**
 * Whether two declared paths name one file, on the runtime's terms.
 *
 * Cleaned first, then case-folded — the runtime's own second clause, and the
 * conservative answer for a file that is not there yet. A path the runtime
 * would refuse matches nothing, including another refused one: two paths that
 * are both invalid are not thereby the same file.
 */
export function samePath(a: string, b: string): boolean {
  const left = cleanRelative(a)
  const right = cleanRelative(b)
  if (left === undefined || right === undefined) return false
  return left === right || left.toLowerCase() === right.toLowerCase()
}

/** Whether any of `paths` names the same file as `path`. */
export function claimedBy(paths: readonly string[], path: string): boolean {
  return paths.some((declared) => samePath(declared, path))
}
