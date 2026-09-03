/**
 * The alias forms, one test each.
 *
 * Every case here is a spelling a `jpack.json` can legally carry that the
 * runtime resolves to the same file as the desk's own `<dir>/<slug>.pack.json`.
 * Before this, the collision check was `===` on raw spellings, so each of them
 * let a create through and left two ids resolving to one document.
 */
import { describe, expect, it } from 'vitest'
import { claimedBy, cleanRelative, equalFold, samePath } from './packPath'

describe('cleanRelative', () => {
  it('cleans the interior forms the runtime cleans', () => {
    expect(cleanRelative('packs/./new.pack.json')).toBe('packs/new.pack.json')
    expect(cleanRelative('packs//new.pack.json')).toBe('packs/new.pack.json')
    expect(cleanRelative('packs/old/../new.pack.json')).toBe('packs/new.pack.json')
    expect(cleanRelative('./packs/new.pack.json')).toBe('packs/new.pack.json')
    expect(cleanRelative('packs/.//../packs/new.pack.json')).toBe('packs/new.pack.json')
    expect(cleanRelative('packs/new.pack.json')).toBe('packs/new.pack.json')
  })

  it('refuses what the runtime refuses outright', () => {
    for (const raw of [
      '',
      '/packs/new.pack.json',
      'C:/packs/new.pack.json',
      '\\packs\\new.pack.json',
      'https://example.invalid/x.json',
      '../outside.json',
      'packs/../../outside.json',
      '.',
      'packs/x\0.json'
    ]) {
      expect(cleanRelative(raw), raw).toBeUndefined()
    }
  })
})

describe('samePath', () => {
  it('matches every alias of one file', () => {
    const target = 'packs/new.pack.json'
    for (const alias of [
      'packs/new.pack.json',
      'packs/./new.pack.json',
      'packs//new.pack.json',
      './packs/new.pack.json',
      'packs/old/../new.pack.json',
      'PACKS/NEW.PACK.JSON',
      'Packs/New.Pack.Json'
    ]) {
      expect(samePath(alias, target), alias).toBe(true)
    }
  })

  it('keeps two different files apart', () => {
    expect(samePath('packs/a.pack.json', 'packs/b.pack.json')).toBe(false)
    expect(samePath('packs/a.pack.json', 'other/a.pack.json')).toBe(false)
    // A nested directory that merely shares a prefix is not the same file.
    expect(samePath('packs/a.pack.json', 'packs/a.pack.json/x')).toBe(false)
  })

  it('says nothing is the same as a path the runtime would refuse', () => {
    // Two invalid paths are not thereby one file, and an invalid one never
    // "claims" a valid one — a refusal is not a match.
    expect(samePath('../outside.json', '../outside.json')).toBe(false)
    expect(samePath('/abs.json', 'abs.json')).toBe(false)
  })
})

describe('claimedBy', () => {
  it('finds the alias among the paths a project already declares', () => {
    const declared = ['packs/intake.pack.json', 'packs/./new.pack.json']
    expect(claimedBy(declared, 'packs/new.pack.json')).toBe(true)
    expect(claimedBy(declared, 'packs/other.pack.json')).toBe(false)
    expect(claimedBy([], 'packs/new.pack.json')).toBe(false)
  })
})

describe('equalFold, against strings.EqualFold itself', () => {
  /**
   * The table is `go run` output, not a reasoned list.
   *
   * Each row was produced by handing the pair to `strings.EqualFold` in a
   * throwaway Go program on this machine and recording the answer. Reasoning
   * about Unicode folding is how the `toLowerCase()` version got written in
   * the first place: it looks obviously right, and `ſ` and `ς` are the two
   * letters that make it obviously wrong.
   */
  it.each([
    ['packs', 'packs', true],
    ['packs', 'PACKS', true],
    ['packs', 'Packs', true],
    // The two the review reproduced. `ſ` U+017F is what an older document's
    // `packſ/` carries, and it folds with `s` — `'ſ'.toLowerCase()` does not.
    ['packſ', 'packs', true],
    ['packſ', 'PACKS', true],
    ['packſ/A', 'PACKS/a', true],
    ['packs/ſ.pack.json', 'packs/s.pack.json', true],
    // Sigma: three code points, one orbit. A final sigma never met a medial
    // one under lowercasing, because `'ς'.toLowerCase()` is `ς`.
    ['Σ', 'ς', true],
    ['Σ', 'σ', true],
    ['ς', 'σ', true],
    // The letterlike forms a filesystem hands back on macOS.
    ['K', 'k', true],
    ['Å', 'å', true],
    ['Ω', 'ω', true],
    ['µ', 'μ', true],
    ['ẛ', 'ṡ', true],
    // The Greek variant letterforms `SimpleFold` carries.
    ['θ', 'ϑ', true],
    ['ϴ', 'θ', true],
    ['β', 'ϐ', true],
    ['π', 'ϖ', true],
    ['φ', 'ϕ', true],
    ['κ', 'ϰ', true],
    ['ρ', 'ϱ', true],
    ['ε', 'ϵ', true],
    ['ι', 'ͅ', true],
    // Eszett folds with its capital and with nothing else — `ss` is a *full*
    // fold, which `EqualFold` does not perform.
    ['ß', 'ẞ', true],
    ['ß', 'ss', false],
    ['ß', 's', false],
    // Turkish. `ı` raises to `I` in JavaScript and folds with nothing but
    // itself in Go, which is why uppercase agreement is not a second chance.
    ['ı', 'I', false],
    ['ı', 'i', false],
    ['İ', 'i', false],
    // Digraphs and their title case, which do fold.
    ['Ǆ', 'ǆ', true],
    ['ǅ', 'ǆ', true],
    ['Ǆ', 'ǅ', true],
    // Ligatures are compatibility forms, not case forms.
    ['ﬀ', 'ff', false],
    ['ﬄ', 'ffl', false],
    ['ﬁ', 'fi', false],
    ['ﬅ', 'st', false],
    // Ordinary letters, and scripts with no case at all.
    ['a', 'a', true],
    ['a', 'b', false],
    ['packs/a.pack.json', 'PACKS/A.PACK.JSON', true],
    ['日本', '日本', true],
    ['日本', '日', false],
    ['𝒜', '𝒜', true],
    ['ϲ', 'Ϲ', true],
    ['ϲ', 'c', false],
    ['ᵹ', 'Ᵹ', true],
    ['ⱥ', 'Ⱥ', true],
    ['ⅷ', 'Ⅷ', true],
    ['ᾀ', 'ᾈ', true],
    ['ᾀ', 'ἀι', false],
    ['ǰ', 'J̌', false],
    ['ſ', 'S', true]
  ])('folds %s against %s as %s', (left, right, folds) => {
    expect(equalFold(left as string, right as string)).toBe(folds)
  })

  it('compares code points, not UTF-16 units', () => {
    // **Deseret, because it is the case that can tell the two apart.** An
    // astral letter with a case mapping is one rune and two UTF-16 units:
    // `𐐀` U+10400 lowercases to `𐐨` U+10428, and Go folds them. Split into
    // units, the lead surrogates match and the trail surrogates are compared
    // as lone surrogates — which have no case — so a unit-wise pass answers
    // false for a pair that folds.
    //
    // `𝒜` has no case mapping, so it answers the same either way: the first
    // fixture written here proved nothing, and the mutation row that breaks
    // this survived it.
    expect(equalFold('\u{10400}', '\u{10428}')).toBe(true)
    expect(equalFold('\u{10400}x', '\u{10428}X')).toBe(true)
    // And a rune count that differs from the unit count is still a length
    // mismatch: one astral letter is not two ASCII ones.
    expect(equalFold('\u{10400}', 'ab')).toBe(false)
  })
})

describe('samePath, folded the way the runtime folds', () => {
  it('sees one file through a long s and a sigma', () => {
    // The two aliases the review reproduced. Both are legal spellings a
    // `jpack.json` can carry, and both were two files to this desk.
    expect(samePath('packſ/new.pack.json', 'packs/new.pack.json')).toBe(true)
    expect(samePath('Σ/x.pack.json', 'ς/x.pack.json')).toBe(true)
  })

  it('still keeps the letters Go keeps apart apart', () => {
    expect(samePath('ı/x.pack.json', 'i/x.pack.json')).toBe(false)
    expect(samePath('ß/x.pack.json', 'ss/x.pack.json')).toBe(false)
  })

  it('finds a long-s alias among a project’s declared paths', () => {
    expect(claimedBy(['packſ/new.pack.json'], 'packs/new.pack.json')).toBe(true)
  })
})
