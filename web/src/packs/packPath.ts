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
 * The fold orbits Go walks and JavaScript's case mapping does not.
 *
 * `strings.EqualFold` folds through `unicode.SimpleFold`, which walks an
 * *orbit* — the cycle of code points that all case-fold together — rather than
 * lowercasing. For nearly every letter that cycle is just {upper, lower} and
 * `toLowerCase()` picks the same representative. For a handful it is not, and
 * those are exactly the ones a filesystem can hand back:
 *
 * - `ſ` U+017F LATIN SMALL LETTER LONG S folds with `s`/`S`. It is what an
 *   older document's `packſ/` is, and `'ſ'.toLowerCase()` is `ſ`.
 * - `Σ σ ς` are one orbit. `'ς'.toLowerCase()` is `ς`, so a final sigma and a
 *   medial one never met.
 * - `K` U+212A KELVIN SIGN and `Å` U+212B ANGSTROM SIGN fold with the ordinary
 *   letters they look like — macOS normalises filenames and these survive.
 * - The Greek and Latin variant letterforms below are the rest of the orbits
 *   `SimpleFold` carries that a lowercase pass does not close.
 *
 * Each entry is one orbit; the first member is its canonical representative.
 * This is the "small table" the rule needs, not a Unicode implementation: what
 * is *not* here is handled by the general rule in `foldRune`, which is what Go
 * reduces to for every ordinary letter.
 */
const FOLD_ORBITS: readonly (readonly string[])[] = [
  ['s', 'S', '\u017F'],
  ['k', 'K', '\u212A'],
  ['\u00E5', '\u00C5', '\u212B'],
  ['\u03C3', '\u03A3', '\u03C2'],
  ['\u03B2', '\u0392', '\u03D0'],
  ['\u03B5', '\u0395', '\u03F5'],
  ['\u03B8', '\u0398', '\u03D1', '\u03F4'],
  ['\u03B9', '\u0399', '\u0345', '\u1FBE'],
  ['\u03BA', '\u039A', '\u03F0'],
  ['\u03BC', '\u039C', '\u00B5'],
  ['\u03C0', '\u03A0', '\u03D6'],
  ['\u03C1', '\u03A1', '\u03F1'],
  ['\u03C6', '\u03A6', '\u03D5'],
  ['\u03C9', '\u03A9', '\u2126'],
  ['\u1E61', '\u1E60', '\u1E9B']
]

const ORBIT_OF: ReadonlyMap<string, string> = new Map(
  FOLD_ORBITS.flatMap((orbit) => orbit.map((member) => [member, orbit[0]!] as const))
)

/** One code point's canonical fold representative. */
function foldRune(rune: string): string {
  return ORBIT_OF.get(rune) ?? rune.toLowerCase()
}

/**
 * Whether two code points fold together, on Go's terms.
 *
 * The orbit table first, then the general rule `SimpleFold` reduces to for an
 * ordinary letter: **lowercase agreement**, and only that.
 *
 * Uppercase agreement is deliberately *not* a second chance, though it is the
 * obvious thing to add. `ı` U+0131 DOTLESS I raises to `I` in JavaScript and
 * folds with nothing but itself in Go — Turkish keeps the dotted and dotless
 * letters apart, and simple folding respects that. An uppercase clause makes
 * `ı` and `i` one file, which is a collision the runtime would not report and
 * a create this desk would then refuse for no reason. The pair that clause was
 * for, `ß`/`ẞ`, is already handled: `ẞ` lowercases to `ß`.
 *
 * Checked against `strings.EqualFold` itself over a battery of pairs, not
 * reasoned about — see `packPath.test.ts`.
 */
function runeFolds(a: string, b: string): boolean {
  return a === b || foldRune(a) === foldRune(b)
}

/**
 * `strings.EqualFold`, on the paths this desk compares.
 *
 * Iterated by **code point** and not by UTF-16 unit, because Go iterates
 * runes: a path carrying an astral character must not fold half a surrogate
 * pair against half of another. Lengths are compared in code points for the
 * same reason.
 */
export function equalFold(a: string, b: string): boolean {
  if (a === b) return true
  const left = [...a]
  const right = [...b]
  if (left.length !== right.length) return false
  return left.every((rune, index) => runeFolds(rune, right[index]!))
}

/**
 * Whether two declared paths name one file, on the runtime's terms.
 *
 * Cleaned first, then case-folded — the runtime's own second clause, and the
 * conservative answer for a file that is not there yet. A path the runtime
 * would refuse matches nothing, including another refused one: two paths that
 * are both invalid are not thereby the same file.
 *
 * **Folded the way `strings.EqualFold` folds**, not lowercased. The runtime
 * accepts Unicode paths, so a declared `packſ/` and a written `packs/` are one
 * file to it and were two here — and `Σ` against `ς` likewise. Constraining
 * both sides to ASCII would have been the broader refusal; folding properly is
 * the one that answers the question the runtime answers.
 */
export function samePath(a: string, b: string): boolean {
  const left = cleanRelative(a)
  const right = cleanRelative(b)
  if (left === undefined || right === undefined) return false
  return left === right || equalFold(left, right)
}

/** Whether any of `paths` names the same file as `path`. */
export function claimedBy(paths: readonly string[], path: string): boolean {
  return paths.some((declared) => samePath(declared, path))
}
