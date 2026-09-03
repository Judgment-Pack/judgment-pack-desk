/**
 * The identity slot's boundary, enforced rather than described.
 *
 * Three tests, and they are **not of equal weight**. (1) is load-bearing: the
 * serialized identity type's key set is *exactly* `provider`, the provider
 * object's member set is exactly the eight the schema declares, the decoder
 * refuses anything else by name, and the state the shell reads carries a
 * nullable provider with no tag on it. (2) and (3) are string enumerations
 * over the source tree; they cannot catch a novel spelling and are labelled
 * here as the weak guards they are, because a guard whose limits are not
 * written down gets read as a proof.
 *
 * **(1) is exactness, not a blacklist, and that is the repair.** It used to
 * enumerate five forbidden names, so the rule it enforced was "do not add one
 * of these five" — adding `identity.strategy` to the type and to the decoder's
 * allow-list passed every assertion in this file. A blacklist of
 * discriminators cannot state "one field", because the thing it is trying to
 * exclude is *any second field*, and that set has no enumeration. So the key
 * sets are asserted whole, at the type level and again against the declaration
 * text: the type-level half fails `tsc`, and the text half fails `vitest` —
 * which is the half the mutation harness can see.
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
import {
  decodeDeskConfig,
  type IdentityConfig,
  type IdentityProviderConfig
} from '../config/deskConfig'
import type { IdentityState, ProviderIdentity } from './IdentityProvider'

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

/**
 * True only where two key sets are the same set, both ways round.
 *
 * A one-directional `extends` is not enough: `keyof T extends 'provider'`
 * passes for an empty type, and `'provider' extends keyof T` passes for a type
 * with ten members. Both directions is the assertion.
 */
type Exactly<A, B> = [A] extends [B] ? ([B] extends [A] ? true : false) : false

/** The serialized identity slot: one nullable field, and no second one. */
const IDENTITY_KEYS = ['provider'] as const

/** The provider object's members, exactly as the schema declares them. */
const PROVIDER_KEYS = [
  'label',
  'issuer',
  'clientId',
  'scopes',
  'audience',
  'claims',
  'showRemoteAvatar',
  'signOut'
] as const

/** What the shell reads: a nullable provider, and the local name beside it. */
const STATE_KEYS = ['provider', 'displayName'] as const

/** What the desk shows about a configured issuer. Display, both of them. */
const PROVIDER_IDENTITY_KEYS = ['issuerHost', 'label'] as const

// The type-level half. These fail `tsc --noEmit` — which `npm run build` runs
// before Vite — the moment a member is added to or taken from either type.
const identityKeysAreExact: Exactly<keyof IdentityConfig, (typeof IDENTITY_KEYS)[number]> = true
const providerKeysAreExact: Exactly<
  keyof IdentityProviderConfig,
  (typeof PROVIDER_KEYS)[number]
> = true
const stateKeysAreExact: Exactly<keyof IdentityState, (typeof STATE_KEYS)[number]> = true
const providerIdentityKeysAreExact: Exactly<
  keyof ProviderIdentity,
  (typeof PROVIDER_IDENTITY_KEYS)[number]
> = true

/**
 * The members an interface body declares, at its own depth.
 *
 * Depth matters: `claims: { name: string; picture: string; subject: string }`
 * is one member of the provider, not four, and a line-wise grep would read it
 * as four. Comment lines are skipped so a doc comment naming a field is not
 * mistaken for one.
 */
function membersOf(body: string): string[] {
  const members: string[] = []
  let depth = 0
  for (const raw of body.split('\n')) {
    const line = raw.trim()
    if (depth === 0 && !line.startsWith('*') && !line.startsWith('/')) {
      const match = /^([A-Za-z_$][\w$]*)\??\s*:/.exec(line)
      if (match) members.push(match[1]!)
    }
    for (const character of line) {
      if (character === '{') depth += 1
      if (character === '}') depth -= 1
    }
  }
  return members
}

describe('(1) the identity slot is exactly one nullable field — the load-bearing guard', () => {
  it('declares the identity key set as exactly provider, and no other member', () => {
    // Asserted whole rather than against a list of forbidden names. The rule
    // is "one field"; a blacklist can only ever say "not these five", and
    // `identity.strategy` walked past the five without touching them.
    expect(identityKeysAreExact).toBe(true)
    const source = read('config/deskConfig.ts')
    expect(membersOf(interfaceBody(source, 'IdentityConfig'))).toEqual([...IDENTITY_KEYS])
  })

  it('declares the provider member set exactly, and nothing beside it', () => {
    expect(providerKeysAreExact).toBe(true)
    const source = read('config/deskConfig.ts')
    expect(membersOf(interfaceBody(source, 'IdentityProviderConfig'))).toEqual([...PROVIDER_KEYS])
  })

  it('refuses any member the provider object does not declare, by name', () => {
    // Every name that is not one of the eight, taken from the decoder's own
    // allow-list rather than from a list of names someone thought of.
    for (const name of ['kind', 'mode', 'operator', 'vendor', 'clientSecret', 'strategy']) {
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

  it('refuses any member beside the provider, by name', () => {
    // The other depth, and the one nothing asserted: the decoder's identity
    // allow-list is exactly ['provider'], so a `kind` here is an unknown key
    // and the file is refused whole. Unasserted, that allow-list could have
    // grown a second member without a single test noticing.
    for (const name of ['kind', 'mode', 'operator', 'vendor', 'clientSecret', 'strategy']) {
      const decoded = decodeDeskConfig(
        JSON.stringify({ deskConfigVersion: 1, identity: { provider: null, [name]: 'v' } }),
        'desk'
      )
      expect(decoded.values, `identity.${name} refuses the whole file`).toBeUndefined()
      expect(decoded.problems.map((problem) => problem.key)).toContain(`identity.${name}`)
    }
  })

  it('exposes nullable provider state, with no discriminator to branch on', () => {
    expect(stateKeysAreExact).toBe(true)
    expect(providerIdentityKeysAreExact).toBe(true)
    const source = read('identity/IdentityProvider.tsx')
    expect(membersOf(interfaceBody(source, 'IdentityState'))).toEqual([...STATE_KEYS])
    expect(membersOf(interfaceBody(source, 'ProviderIdentity'))).toEqual([
      ...PROVIDER_IDENTITY_KEYS
    ])
    // And no reader may branch on a tag, because there is none to read. The
    // union this replaced carried `mode: 'local' | 'provider'`, which is the
    // exact shape the file's own schema refuses one layer down.
    for (const file of ['identity/IdentityProvider.tsx', 'identity/UserControl.tsx']) {
      expect(read(file), `${file} branches on a mode tag`).not.toMatch(/\bmode\s*[:=]==?\s*'/)
    }
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
