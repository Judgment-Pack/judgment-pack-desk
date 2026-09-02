/**
 * The identity slot's boundary, enforced rather than described.
 *
 * Three tests, and they are **not of equal weight**. (1) is load-bearing: the
 * config type admits no discriminator, the decoder refuses one pasted in by
 * name, and the exposed session union has exactly two members. (2) and (3) are
 * string enumerations over the source tree; they cannot catch a novel spelling
 * and are labelled here as the weak guards they are, because a guard whose
 * limits are not written down gets read as a proof.
 *
 * **The slot is the whole of `identity`, not only `identity.provider`.** The
 * rule the spec states is about the slot — "there is no kind, no mode, no
 * operator, no vendor, and no third shape" — and a discriminator one level up
 * at `identity.kind` would branch the desk just as effectively as one inside
 * the provider object. So (1) reads both declarations and pastes at both
 * depths. Scanning only the inner object left the outer one open, and a
 * supplied-versus-bring-your-own branch on Admin passed every test.
 *
 * **(2) and (3) read `routes/` as well.** The identity table pins Admin ›
 * Identity provider by name — no preset list, no discovery default, no issuer
 * literal anywhere in the source tree — and that page is `routes/AdminView.tsx`.
 * Guards that stopped at `shell/`, `identity/` and `config/` did not look at
 * the identity UI's own page at all, which is a sharper limitation than the
 * "cannot catch a novel spelling" they were labelled with.
 */
import { readFileSync } from 'node:fs'
import { readdirSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { decodeDeskConfig } from '../config/deskConfig'

const SRC = join(import.meta.dirname, '..')

function read(relative: string): string {
  return readFileSync(join(SRC, relative), 'utf8')
}

/** Every .ts/.tsx file under one of the shell's own directories. */
function sourcesUnder(...directories: string[]): { path: string; text: string }[] {
  const found: { path: string; text: string }[] = []
  for (const directory of directories) {
    for (const entry of readdirSync(join(SRC, directory), { withFileTypes: true })) {
      if (!entry.isFile()) continue
      if (!entry.name.endsWith('.ts') && !entry.name.endsWith('.tsx')) continue
      found.push({
        path: `${directory}/${entry.name}`,
        text: read(`${directory}/${entry.name}`)
      })
    }
  }
  return found
}

/** One interface's body, by name — not the doc comment above it. */
function interfaceBody(text: string, name: string): string {
  const opening = text.indexOf(`export interface ${name} {`)
  expect(opening, `${name} is declared`).toBeGreaterThan(-1)
  const start = text.indexOf('{', opening)
  let depth = 0
  for (let index = start; index < text.length; index += 1) {
    if (text[index] === '{') depth += 1
    if (text[index] === '}') {
      depth -= 1
      if (depth === 0) return text.slice(start + 1, index)
    }
  }
  throw new Error(`${name}'s body did not close`)
}

const DISCRIMINATORS = ['kind', 'mode', 'operator', 'vendor', 'clientSecret']

describe('(1) the identity type admits no discriminator — the load-bearing guard', () => {
  it('declares none of them as a member of the identity types', () => {
    // Scoped to these two interface bodies, because `PanesConfig.left.mode`
    // and `IdentitySession.mode` legitimately exist elsewhere in this tree: a
    // whole-file grep for "mode" would either fail on innocent code or be
    // loosened until it caught nothing. Both are read, because the slot is
    // `identity` and not only the object inside it — `identity.kind` would
    // branch the desk exactly as well. `\\??` covers the optional spelling,
    // which is how such a member would actually be added.
    const source = read('config/deskConfig.ts')
    for (const declaration of ['IdentityProviderConfig', 'IdentityConfig']) {
      const body = interfaceBody(source, declaration)
      for (const name of DISCRIMINATORS) {
        expect(body, `${declaration} declares ${name}`).not.toMatch(
          new RegExp(`^\\s*${name}\\??\\s*:`, 'm')
        )
      }
    }
  })

  it('refuses one pasted into the provider object, by name', () => {
    for (const name of DISCRIMINATORS) {
      const decoded = decodeDeskConfig(
        JSON.stringify({
          deskConfigVersion: 1,
          identity: { provider: { issuer: 'https://issuer.example/', clientId: 'x', [name]: 'v' } }
        }),
        'desk'
      )
      expect(decoded.values, `${name} refuses the whole file`).toBeUndefined()
      expect(decoded.problems.map((problem) => problem.key)).toContain(`identity.provider.${name}`)
    }
  })

  it('refuses one pasted beside the provider, by name', () => {
    // The other depth, and the one nothing asserted: the decoder's identity
    // allow-list is exactly ['provider'], so a `kind` here is an unknown key
    // and the file is refused whole. Unasserted, that allow-list could have
    // grown a second member without a single test noticing.
    for (const name of DISCRIMINATORS) {
      const decoded = decodeDeskConfig(
        JSON.stringify({ deskConfigVersion: 1, identity: { provider: null, [name]: 'v' } }),
        'desk'
      )
      expect(decoded.values, `identity.${name} refuses the whole file`).toBeUndefined()
      expect(decoded.problems.map((problem) => problem.key)).toContain(`identity.${name}`)
    }
  })

  it('exposes a session union of exactly two members', () => {
    const source = read('identity/IdentityProvider.tsx')
    const declaration = source.slice(
      source.indexOf('export type IdentitySession'),
      source.indexOf('const LOCAL')
    )
    const modes = [...declaration.matchAll(/mode:\s*'([a-z]+)'/g)].map((match) => match[1])
    expect(modes).toEqual(['local', 'provider'])
  })
})

describe('(2) no component branches on the issuer — a WEAK, enumerated guard', () => {
  it('finds no comparison against an issuer anywhere in shell/, identity/ or routes/', () => {
    // Weak by construction: it looks for the shapes a comparison is usually
    // written in, and a novel spelling walks straight past it. (1) is what
    // actually holds the design in place.
    const comparisons = [/issuer\s*===/, /issuer\s*!==/, /issuerHost\s*===/, /\.includes\(\s*['"]https/]
    for (const source of sourcesUnder('shell', 'identity', 'routes')) {
      if (source.path === 'identity/enforcement.test.ts') continue
      for (const pattern of comparisons) {
        expect(source.text, `${source.path} compares an issuer`).not.toMatch(pattern)
      }
    }
  })
})

describe('(3) no issuer literal in the source — a WEAK, enumerated guard', () => {
  it('finds no https:// issuer literal outside fixtures, tests and docs', () => {
    // Also weak: it cannot see a hostname assembled at runtime, and it
    // deliberately allows the repository links Help & About renders.
    const allowed = [
      'https://github.com/Judgment-Pack/judgment-pack-desk',
      'https://github.com/Judgment-Pack/judgment-pack-runtime'
    ]
    for (const source of sourcesUnder('shell', 'identity', 'config', 'routes')) {
      if (source.path.includes('.test.')) continue
      const literals = [...source.text.matchAll(/https:\/\/[^\s'"`)]+/g)].map((match) => match[0])
      for (const literal of literals) {
        expect(
          allowed.some((prefix) => literal.startsWith(prefix)),
          `${source.path} carries the literal ${literal}`
        ).toBe(true)
      }
    }
  })
})
