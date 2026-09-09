/**
 * **No credential on any URL, checked over the bundle that ships.**
 *
 * Every other test in this suite reads a function's answer. This reads the
 * emitted JavaScript, because the claim is about what the *page* does and a
 * page is the bundle: a helper could be perfect and one call site could still
 * spell an address by hand. The three spellings here are the three arrangements
 * this desk has actually shipped and removed — `?token=`, a `secret` on a
 * query, and a credential on the `/ws` address.
 *
 * **The positive control matters more than usual here.** A glob that matched
 * nothing would pass every assertion below, so the bundle is required to exist,
 * to be substantial, and to contain the subprotocol string the page *does* use.
 * CI builds before it tests for exactly this reason.
 */
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const ASSETS = join(import.meta.dirname, '..', '..', 'dist', 'assets')

function bundle(): { name: string; text: string }[] {
  let names: string[]
  try {
    names = readdirSync(ASSETS).filter((n) => n.endsWith('.js'))
  } catch {
    return []
  }
  return names.map((name) => ({ name, text: readFileSync(join(ASSETS, name), 'utf8') }))
}

describe('the built bundle', () => {
  const built = bundle()

  it('exists, and is the application rather than an empty directory', () => {
    expect(
      built.length,
      'no bundle to read: run `npm run build` first — CI builds before it tests'
    ).toBeGreaterThan(0)
    expect(built.reduce((n, f) => n + f.text.length, 0)).toBeGreaterThan(100_000)
  })

  it('carries the subprotocol offer, which is where the id actually goes', () => {
    // The positive control: this string is what the page uses instead of a URL
    // credential, so its absence would mean the assertions below are vacuous.
    expect(built.some((f) => f.text.includes('jpack-desk-session.'))).toBe(true)
  })

  it('spells no credential onto any address', () => {
    for (const { name, text } of built) {
      // `?token=` was the first arrangement: the secret on every URL.
      expect(text, `${name} builds a ?token= address`).not.toContain('?token=')
      // `secret=` was the launch URL, which the page never constructs.
      expect(text, `${name} builds a secret= address`).not.toContain('secret=')
      // And the upgrade carries nothing on its query.
      expect(text, `${name} puts a query on the upgrade`).not.toContain('/ws?')
    }
  })
})
