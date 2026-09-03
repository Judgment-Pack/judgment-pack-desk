/**
 * The alias forms, one test each.
 *
 * Every case here is a spelling a `jpack.json` can legally carry that the
 * runtime resolves to the same file as the desk's own `<dir>/<slug>.pack.json`.
 * Before this, the collision check was `===` on raw spellings, so each of them
 * let a create through and left two ids resolving to one document.
 */
import { describe, expect, it } from 'vitest'
import { claimedBy, cleanRelative, samePath } from './packPath'

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
