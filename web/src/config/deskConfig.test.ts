/**
 * The decoder, and the one rule that governs all of it: **any problem refuses
 * the whole file, and every refusal names its key.**
 *
 * A decoder that accepted the good half of a file would let a typo'd key sit
 * there doing nothing while its siblings applied — which reads, to whoever
 * wrote it, as a setting that does not work rather than a spelling that is
 * wrong. And it would half-honour a file with a pasted secret in it.
 */
import { describe, expect, it } from 'vitest'
import {
  DESK_DEFAULTS,
  decodeDeskConfig,
  effectiveConfig,
  type ConfigProblem
} from './deskConfig'

/** The keys named in a decode's problems, so a case reads as one line. */
function keys(problems: ConfigProblem[]): string[] {
  return problems.map((problem) => problem.key)
}

const VALID = {
  deskConfigVersion: 1,
  organization: { name: 'Acme Co.', mark: null },
  user: { displayName: 'local user' },
  appearance: { theme: 'dark', density: 'compact' },
  panes: {
    left: { mode: 'icons', width: 200 },
    inspector: { open: true, width: 400 },
    console: { open: true, height: 300 }
  }
}

describe('decodeDeskConfig', () => {
  it('accepts a whole, well-formed project file', () => {
    const decoded = decodeDeskConfig(JSON.stringify(VALID), 'project')
    expect(decoded.problems).toEqual([])
    expect(decoded.values?.organization?.name).toBe('Acme Co.')
    expect(decoded.values?.appearance).toEqual({ theme: 'dark', density: 'compact' })
    expect(decoded.values?.panes?.left).toEqual({ mode: 'icons', width: 200 })
  })

  it('refuses a file that is not JSON, and carries the parser’s own message', () => {
    const decoded = decodeDeskConfig('{ not json', 'project')
    expect(decoded.values).toBeUndefined()
    expect(keys(decoded.problems)).toEqual([''])
    expect(decoded.problems[0]!.reason).toContain('not JSON')
  })

  it('refuses a file that is not a JSON object, naming what it found', () => {
    for (const text of ['[]', '"a string"', '4', 'null']) {
      const decoded = decodeDeskConfig(text, 'project')
      expect(decoded.values).toBeUndefined()
      expect(decoded.problems[0]!.reason).toContain('must be a JSON object')
    }
  })

  it('requires deskConfigVersion, and requires it to be 1', () => {
    expect(keys(decodeDeskConfig('{}', 'project').problems)).toEqual(['deskConfigVersion'])
    const wrong = decodeDeskConfig('{"deskConfigVersion": 2}', 'project')
    expect(keys(wrong.problems)).toEqual(['deskConfigVersion'])
    expect(wrong.problems[0]!.reason).toContain('found number 2')
  })

  it('refuses an unknown top-level key BY NAME, one problem per key', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({ ...VALID, colour: 'blue', logo: 'x' }),
      'project'
    )
    expect(decoded.values).toBeUndefined()
    expect(keys(decoded.problems).sort()).toEqual(['colour', 'logo'])
  })

  it('refuses an unknown nested key with its path', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({ ...VALID, organization: { name: 'Acme Co.', logo: 'x' } }),
      'project'
    )
    expect(keys(decoded.problems)).toEqual(['organization.logo'])
  })

  it('refuses identity at the project location, and says why in the reason', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({ ...VALID, identity: { provider: null } }),
      'project'
    )
    expect(decoded.values).toBeUndefined()
    expect(keys(decoded.problems)).toEqual(['identity'])
    expect(decoded.problems[0]!.reason).toContain('desk-level desk.json')
    expect(decoded.problems[0]!.reason).toContain('shared')
  })

  it('accepts identity at the desk location', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({
        deskConfigVersion: 1,
        identity: {
          provider: {
            label: 'Company sign-in',
            issuer: 'https://issuer.example/',
            clientId: 'jpack-desk',
            scopes: ['openid', 'profile'],
            audience: null,
            claims: { name: 'name', picture: 'picture', subject: 'sub' },
            showRemoteAvatar: false,
            signOut: 'local'
          }
        }
      }),
      'desk'
    )
    expect(decoded.problems).toEqual([])
    expect(decoded.values?.identity?.provider?.clientId).toBe('jpack-desk')
  })

  it('refuses a pasted clientSecret BY NAME — no such key exists', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({
        deskConfigVersion: 1,
        identity: {
          provider: {
            issuer: 'https://issuer.example/',
            clientId: 'jpack-desk',
            clientSecret: 'hunter2'
          }
        }
      }),
      'desk'
    )
    expect(decoded.values).toBeUndefined()
    expect(keys(decoded.problems)).toContain('identity.provider.clientSecret')
  })

  it('refuses a discriminator pasted into the provider object BY NAME', () => {
    // The load-bearing behavioural half of enforcement test (1): the file has
    // two shapes, not three, and there is nowhere for a third to be recorded.
    for (const member of ['kind', 'mode', 'operator', 'vendor']) {
      const decoded = decodeDeskConfig(
        JSON.stringify({
          deskConfigVersion: 1,
          identity: {
            provider: { issuer: 'https://issuer.example/', clientId: 'x', [member]: 'supplied' }
          }
        }),
        'desk'
      )
      expect(decoded.values).toBeUndefined()
      expect(keys(decoded.problems)).toContain(`identity.provider.${member}`)
    }
  })

  it('refuses an issuer that is not https, or http on loopback', () => {
    const refused = (issuer: string) =>
      decodeDeskConfig(
        JSON.stringify({
          deskConfigVersion: 1,
          identity: { provider: { issuer, clientId: 'x' } }
        }),
        'desk'
      )
    expect(keys(refused('http://issuer.example/').problems)).toContain('identity.provider.issuer')
    expect(keys(refused('not a url').problems)).toContain('identity.provider.issuer')
    // A locally-run test issuer is a transport question, not a question about
    // who the issuer is.
    expect(refused('http://localhost:9000/').problems).toEqual([])
    expect(refused('http://127.0.0.1:9000/').problems).toEqual([])
  })

  it('refuses an organization mark that is a path, or over the size bound', () => {
    const mark = (value: string) =>
      decodeDeskConfig(
        JSON.stringify({ deskConfigVersion: 1, organization: { name: null, mark: value } }),
        'project'
      )
    expect(keys(mark('assets/logo.png').problems)).toEqual(['organization.mark'])
    expect(mark('<svg viewBox="0 0 1 1"></svg>').problems).toEqual([])
    expect(mark('data:image/svg+xml,%3Csvg%3E%3C/svg%3E').problems).toEqual([])
    expect(keys(mark(`<svg>${'x'.repeat(70000)}</svg>`).problems)).toEqual(['organization.mark'])
  })

  it('refuses a mistyped value, naming the key and what it found', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({
        deskConfigVersion: 1,
        appearance: { theme: 'midnight', density: 'comfortable' },
        panes: { left: { mode: 'expanded', width: -1 } }
      }),
      'project'
    )
    expect(keys(decoded.problems).sort()).toEqual(['appearance.theme', 'panes.left.width'])
    expect(decoded.problems.find((p) => p.key === 'appearance.theme')!.reason).toContain('"midnight"')
  })

  it('refuses an empty organization name, exactly as it refuses an empty display name', () => {
    // `null` is how a file says "use the desk's own name"; `""` is not that
    // sentence. Accepted, it rendered a blank brand link and a `·` monogram —
    // the one case `DESK_FALLBACK_NAME` exists for, and the one it missed.
    for (const name of ['', '   ']) {
      const decoded = decodeDeskConfig(
        JSON.stringify({ deskConfigVersion: 1, organization: { name, mark: null } }),
        'project'
      )
      expect(decoded.values, `${JSON.stringify(name)} refuses the file`).toBeUndefined()
      expect(keys(decoded.problems)).toEqual(['organization.name'])
      expect(decoded.problems[0]!.reason).toContain('non-empty string or null')
    }
  })

  it('accepts null as the way a file asks for the desk’s own name', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, organization: { name: null, mark: null } }),
      'project'
    )
    expect(decoded.problems).toEqual([])
    expect(decoded.values?.organization?.name).toBeNull()
  })

  it('takes a display name from the project file — it is local and gates nothing', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, user: { displayName: 'desk operator' } }),
      'project'
    )
    expect(decoded.problems).toEqual([])
    expect(decoded.values?.user?.displayName).toBe('desk operator')
  })
})

describe('effectiveConfig', () => {
  it('is the built-in defaults when nothing was read, with no problems', () => {
    const effective = effectiveConfig(undefined)
    expect(effective.config).toEqual(DESK_DEFAULTS)
    expect(effective.problems).toEqual([])
    expect(effective.sources.organization).toBe('default')
    expect(effective.path).toBe('jpack-desk.json')
  })

  it('is the built-in defaults when the file was refused, carrying the problems', () => {
    const decoded = decodeDeskConfig('{"deskConfigVersion":1,"nope":true}', 'project')
    const effective = effectiveConfig(decoded)
    expect(effective.config).toEqual(DESK_DEFAULTS)
    expect(keys(effective.problems)).toEqual(['nope'])
    // Nothing was honoured, so nothing claims the file as its source.
    expect(Object.values(effective.sources).every((source) => source === 'default')).toBe(true)
  })

  it('badges the sections a project file supplied, and only those', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, organization: { name: 'Acme Co.' } }),
      'project'
    )
    const effective = effectiveConfig(decoded)
    expect(effective.config.organization.name).toBe('Acme Co.')
    expect(effective.sources.organization).toBe('project file')
    expect(effective.sources.panes).toBe('default')
    // Identity can never come from a project file, so its badge never moves.
    expect(effective.sources.identity).toBe('default')
    expect(effective.config.identity.provider).toBeNull()
  })
})
