/**
 * The decoder, and the one rule that governs all of it: **any problem refuses
 * the whole file, and every refusal names its key.**
 *
 * A decoder that accepted the good half of a file would let a typo'd key sit
 * there doing nothing while its siblings applied — which reads, to whoever
 * wrote it, as a setting that does not work rather than a spelling that is
 * wrong. And it would half-honour a file with a pasted secret in it.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  DESK_DEFAULTS,
  EXCLUDED_DIRECTORIES,
  MAX_PACK_DIR_DEPTH,
  PANE_BOUNDS,
  STAGING_PREFIX,
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
    // Measured in **bytes of UTF-8**, not UTF-16 code units. A mark whose
    // text is not ASCII weighs up to three times what `String.length` reports
    // — three and not four, because the four-byte astral characters cost two
    // UTF-16 units each and are therefore only 2:1 —
    // so a limit documented as 64KB and counted in code units is a limit
    // nobody can check against the file on disk. 30,000 three-byte characters
    // is 90,000 bytes and 30,013 code units: refused one way, accepted the
    // other.
    const wide = `<svg>${'あ'.repeat(30000)}</svg>`
    expect(wide.length).toBeLessThan(65536)
    expect(keys(mark(wide).problems)).toEqual(['organization.mark'])
    expect(mark(wide).problems[0]!.reason).toContain('bytes of UTF-8')
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

  it('refuses a packs directory deeper than the listing walks', () => {
    // The chassis refuses the write at this bound; refusing the *configuration*
    // is what lets Admin name the key that is wrong instead of the dialog
    // failing later on a path nobody chose to look at.
    const dirOf = (depth: number) =>
      decodeDeskConfig(
        JSON.stringify({
          deskConfigVersion: 1,
          storage: { packs: { dir: Array.from({ length: depth }, (_, i) => `d${i}`).join('/') } }
        }),
        'project'
      )
    expect(dirOf(MAX_PACK_DIR_DEPTH).problems).toEqual([])
    expect(keys(dirOf(MAX_PACK_DIR_DEPTH + 1).problems)).toEqual(['storage.packs.dir'])
    expect(dirOf(MAX_PACK_DIR_DEPTH + 1).problems[0]!.reason).toContain('directories deep')
  })

  it('refuses a pane dimension of zero, and one past its documented maximum', () => {
    // **Zero is the dangerous value**, not a harmless one: a pane the file
    // declares `open` at zero pixels renders as an open pane nobody can see,
    // with a toggle that appears to do nothing. And an enormous one is the
    // same defect from the other end — the frame is clipped and does not
    // scroll, so a 20,000px console pushes the status strip out of it.
    const dimension = (path: 'left' | 'inspector' | 'console', field: string, value: number) =>
      decodeDeskConfig(
        JSON.stringify({ deskConfigVersion: 1, panes: { [path]: { [field]: value } } }),
        'project'
      )
    for (const [path, field, key] of [
      ['left', 'width', 'panes.left.width'],
      ['inspector', 'width', 'panes.inspector.width'],
      ['console', 'height', 'panes.console.height']
    ] as const) {
      const bounds = PANE_BOUNDS[key]!
      expect(keys(dimension(path, field, 0).problems), `${key} at zero`).toEqual([key])
      expect(keys(dimension(path, field, bounds.min - 1).problems), `${key} below`).toEqual([key])
      expect(keys(dimension(path, field, bounds.max + 1).problems), `${key} above`).toEqual([key])
      // The endpoints themselves are legal: the bound is inclusive, and a
      // refusal that moved by one would be a different rule than the one
      // Admin and the README print.
      expect(dimension(path, field, bounds.min).problems, `${key} at min`).toEqual([])
      expect(dimension(path, field, bounds.max).problems, `${key} at max`).toEqual([])
      // And the refusal names the range rather than saying only "invalid".
      expect(dimension(path, field, 0).problems[0]!.reason).toContain(
        `between ${bounds.min} and ${bounds.max}`
      )
    }
  })

  it('records which pane dimensions the file stated, and which it inherited', () => {
    // `PanesConfig` cannot answer this — every field is filled in from the
    // defaults, so "the file said 360" and "the file said nothing" are the
    // same value. The Inspector's drawer form is 320px by its own baseline and
    // must not move to the column's 360px on a desk that configures nothing.
    const decoded = decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, panes: { inspector: { open: true } } }),
      'project'
    )
    expect(decoded.problems).toEqual([])
    expect(decoded.declaredPanes).toEqual({
      leftWidth: false,
      inspectorWidth: false,
      consoleHeight: false
    })
    expect(decodeDeskConfig(JSON.stringify(VALID), 'project').declaredPanes).toEqual({
      leftWidth: true,
      inspectorWidth: true,
      consoleHeight: true
    })
    // And a file that was refused declared nothing, because it contributed
    // nothing: the desk is on the built-in defaults, dimensions included.
    expect(effectiveConfig(decodeDeskConfig('{ not json', 'project')).declaredPanes).toEqual({
      leftWidth: false,
      inspectorWidth: false,
      consoleHeight: false
    })
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

describe('the storage member', () => {
  const withStorage = (packs: unknown) =>
    decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1, storage: { packs } }), 'project')

  it('is the defaults when the member is absent entirely', () => {
    const decoded = decodeDeskConfig('{"deskConfigVersion":1}', 'project')
    expect(decoded.problems).toEqual([])
    expect(decoded.values?.storage).toBeUndefined()
    expect(effectiveConfig(decoded).config.storage).toEqual({
      packs: { kind: 'filesystem', dir: 'packs', idBase: 'https://example.invalid/judgment-packs/' }
    })
  })

  it('takes the defaults for the members a partial storage.packs leaves out', () => {
    const decoded = withStorage({ dir: 'decisions' })
    expect(decoded.problems).toEqual([])
    expect(decoded.values?.storage).toEqual({
      packs: {
        kind: 'filesystem',
        dir: 'decisions',
        idBase: 'https://example.invalid/judgment-packs/'
      }
    })
  })

  it('refuses an unknown key by its path', () => {
    const decoded = withStorage({ dir: 'packs', bucket: 'acme-packs' })
    expect(decoded.values).toBeUndefined()
    expect(decoded.problems).toEqual([{ key: 'storage.packs.bucket', reason: 'unknown key' }])
  })

  it('names both future kinds when a kind other than filesystem is asked for', () => {
    // `oneOf` would say `must be one of "filesystem"`, which tells whoever
    // typed "database" nothing about why.
    const decoded = withStorage({ kind: 'database' })
    expect(decoded.values).toBeUndefined()
    expect(keys(decoded.problems)).toEqual(['storage.packs.kind'])
    const reason = decoded.problems[0]!.reason
    expect(reason).toContain('must be "filesystem"')
    expect(reason).toContain('"database" and "cloud storage" are not available yet')
  })

  it('refuses a directory that escapes, is absolute, or is not a directory name', () => {
    for (const dir of ['../elsewhere', '/etc/packs', 'packs/../..', 'a\\b', 'C:/packs', '', '   ']) {
      const decoded = withStorage({ dir })
      expect(decoded.values, `dir ${JSON.stringify(dir)} was accepted`).toBeUndefined()
      expect(keys(decoded.problems)).toEqual(['storage.packs.dir'])
    }
  })

  it('refuses a directory the desk never reads or writes', () => {
    // `"dir": "dist"` used to decode clean: Admin advertised it as the pack
    // location and every create then failed at the write, because the chassis
    // excludes those names from its endpoints altogether. A configuration that
    // is accepted and cannot work is worse than one refused where it was
    // written.
    for (const dir of ['dist', 'node_modules', '.git', 'a/vendor/b', 'DIST', '.venv/x']) {
      const decoded = withStorage({ dir })
      expect(decoded.values, `dir ${JSON.stringify(dir)} was accepted`).toBeUndefined()
      expect(keys(decoded.problems)).toEqual(['storage.packs.dir'])
      expect(decoded.problems[0]!.reason).toContain('never reads or writes')
    }
    // And a staging name, which `wireRelativePath` refuses for the same reason.
    expect(withStorage({ dir: '.jpack-desk-abc' }).values).toBeUndefined()
    // A directory that merely contains one of those words is fine: the test is
    // per whole path segment, as the chassis' own is.
    expect(withStorage({ dir: 'distribution' }).values?.storage?.packs.dir).toBe('distribution')
    expect(withStorage({ dir: 'my-vendor-packs' }).values?.storage?.packs.dir).toBe('my-vendor-packs')
  })

  it('names the same directories the chassis excludes, and cannot drift from them', () => {
    // The list is mirrored rather than fetched, so this reads the Go source
    // that owns it. Two answers about what the desk edits is the failure mode
    // worth preventing; one answer plus this assertion is not.
    const watch = readFileSync(join(import.meta.dirname, '../../../internal/desk/watch.go'), 'utf8')
    const block = watch.match(/var skipDirs = map\[string\]bool\{([^}]*)\}/)
    expect(block, 'skipDirs is no longer a map literal in watch.go').toBeTruthy()
    const named = [...block![1]!.matchAll(/"([^"]+)":/g)].map((match) => match[1]!)
    expect(named.sort()).toEqual([...EXCLUDED_DIRECTORIES].sort())

    const files = readFileSync(join(import.meta.dirname, '../../../internal/desk/files.go'), 'utf8')
    expect(files).toContain(`const stagingPrefix = "${STAGING_PREFIX}"`)
  })

  it('trims a trailing separator rather than refusing it', () => {
    expect(withStorage({ dir: 'packs/' }).values?.storage?.packs.dir).toBe('packs')
    expect(withStorage({ dir: 'a/b//' }).values?.storage?.packs.dir).toBe('a/b')
  })

  it('requires idBase to parse as a URI, because a pack’s id member is one', () => {
    const decoded = withStorage({ idBase: 'not a uri' })
    expect(decoded.values).toBeUndefined()
    expect(keys(decoded.problems)).toEqual(['storage.packs.idBase'])
    expect(decoded.problems[0]!.reason).toContain('must be a URI')
  })

  it('normalises idBase to end in a separator, once and here', () => {
    // Every use is then a bare `idBase + slug`, and Admin shows the prefix
    // that will actually be written rather than the one that was typed.
    expect(withStorage({ idBase: 'https://acme.example/packs' }).values?.storage?.packs.idBase).toBe(
      'https://acme.example/packs/'
    )
    expect(withStorage({ idBase: 'https://acme.example/packs/' }).values?.storage?.packs.idBase).toBe(
      'https://acme.example/packs/'
    )
    // A fragment base is already a separator and is left exactly as written.
    expect(withStorage({ idBase: 'https://acme.example/p#' }).values?.storage?.packs.idBase).toBe(
      'https://acme.example/p#'
    )
  })

  it('refuses the whole file for one storage problem, like every other section', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({
        deskConfigVersion: 1,
        organization: { name: 'Acme Co.' },
        storage: { packs: { kind: 'cloud storage' } }
      }),
      'project'
    )
    expect(decoded.values).toBeUndefined()
    expect(effectiveConfig(decoded).config.organization.name).toBeNull()
  })

  it('refuses storage that is not an object, and packs that is not one', () => {
    expect(
      keys(decodeDeskConfig('{"deskConfigVersion":1,"storage":[]}', 'project').problems)
    ).toEqual(['storage'])
    expect(keys(withStorage('packs').problems)).toEqual(['storage.packs'])
  })

  it('badges storage as coming from the project file when it supplied one', () => {
    const effective = effectiveConfig(withStorage({ dir: 'decisions' }))
    expect(effective.sources.storage).toBe('project file')
    expect(effective.config.storage.packs.dir).toBe('decisions')
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

describe('the desk-level file, and the precedence between the two', () => {
  const GOOD = {
    url: 'https://api.example.invalid/v1',
    kind: 'openai-compatible',
    model: 'a-model',
    tools: ['validate']
  }

  /** One desk-level read, as `queries.ts` assembles it from the endpoint. */
  function deskRead(file: unknown) {
    return {
      path: '/home/someone/.config/jpack-desk/desk.json',
      present: true,
      decoded: decodeDeskConfig(JSON.stringify(file), 'desk')
    }
  }

  it('takes the assistant from the desk-level file, and badges it as such', () => {
    const effective = effectiveConfig(
      undefined,
      undefined,
      undefined,
      deskRead({ deskConfigVersion: 1, assistant: { endpoint: GOOD } })
    )
    expect(effective.config.assistant.endpoint).toEqual(GOOD)
    expect(effective.sources.assistant).toBe('desk file')
    expect(effective.desk?.path).toBe('/home/someone/.config/jpack-desk/desk.json')
    expect(effective.desk?.problems).toEqual([])
  })

  it('lets a project file outrank the desk-level one, section by section', () => {
    // The more specific statement wins: the desk-level file is this machine's
    // answer for every project it opens, and a project saying something
    // different is saying it about itself.
    const effective = effectiveConfig(
      decodeDeskConfig(
        JSON.stringify({ deskConfigVersion: 1, organization: { name: 'This project' } }),
        'project'
      ),
      undefined,
      undefined,
      deskRead({
        deskConfigVersion: 1,
        organization: { name: 'Every project' },
        user: { displayName: 'the operator' }
      })
    )
    expect(effective.config.organization.name).toBe('This project')
    expect(effective.sources.organization).toBe('project file')
    // And the section the project said nothing about still comes from the
    // desk-level file rather than falling all the way to the defaults.
    expect(effective.config.user.displayName).toBe('the operator')
    expect(effective.sources.user).toBe('desk file')
  })

  it('refuses each file on its own, and reports each as its own', () => {
    const effective = effectiveConfig(
      decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1 }), 'project'),
      undefined,
      undefined,
      deskRead({ deskConfigVersion: 1, assistant: { endpoint: { ...GOOD, vendor: 'acme' } } })
    )
    // The project file is fine and stays fine.
    expect(effective.problems).toEqual([])
    // The desk-level file is refused whole, so it supplies nothing at all.
    expect(keys(effective.desk?.problems ?? [])).toEqual(['assistant.endpoint.vendor'])
    expect(effective.config.assistant.endpoint).toBeNull()
    expect(effective.sources.assistant).toBe('default')
  })

  it('reads the declared pane dimensions off whichever file supplied them', () => {
    // Reading them off the project's decode while the desk-level file
    // supplied the section would report the Inspector's drawer against a
    // width that file never stated.
    const effective = effectiveConfig(
      undefined,
      undefined,
      undefined,
      deskRead({ deskConfigVersion: 1, panes: { inspector: { width: 420 } } })
    )
    expect(effective.config.panes.inspector.width).toBe(420)
    expect(effective.declaredPanes.inspectorWidth).toBe(true)
    expect(effective.declaredPanes.leftWidth).toBe(false)
  })

  it('tells a file that is absent from a read that never happened', () => {
    // Undefined means nothing asked — the shell's own defaults value. A read
    // that found no file is present: false with a path, and Admin says which
    // of the two it is rather than collapsing them.
    expect(effectiveConfig(undefined).desk).toBeUndefined()
    const looked = effectiveConfig(undefined, undefined, undefined, {
      path: '/home/someone/.config/jpack-desk/desk.json',
      present: false,
      note: 'no desk-level configuration file at /home/someone/.config/jpack-desk/desk.json'
    })
    expect(looked.desk?.present).toBe(false)
    expect(looked.desk?.note).toContain('no desk-level configuration file')
    expect(looked.config.assistant.endpoint).toBeNull()
  })

  it('accepts an http endpoint on loopback and refuses one anywhere else', () => {
    // A transport rule, not an identity one: a bearer credential in clear
    // text over a network is a credential given away.
    for (const url of ['http://localhost:11434/v1', 'http://127.0.0.1:8080/v1']) {
      const decoded = decodeDeskConfig(
        JSON.stringify({ deskConfigVersion: 1, assistant: { endpoint: { ...GOOD, url } } }),
        'desk'
      )
      expect(decoded.problems, url).toEqual([])
    }
    for (const url of ['http://models.example/v1', 'ftp://models.example', 'not a url']) {
      const decoded = decodeDeskConfig(
        JSON.stringify({ deskConfigVersion: 1, assistant: { endpoint: { ...GOOD, url } } }),
        'desk'
      )
      expect(keys(decoded.problems), url).toEqual(['assistant.endpoint.url'])
    }
  })

  it('admits the two protocols and refuses any other by name', () => {
    for (const kind of ['openai-compatible', 'anthropic']) {
      const decoded = decodeDeskConfig(
        JSON.stringify({ deskConfigVersion: 1, assistant: { endpoint: { ...GOOD, kind } } }),
        'desk'
      )
      expect(decoded.problems, kind).toEqual([])
    }
    const decoded = decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, assistant: { endpoint: { ...GOOD, kind: 'gemini' } } }),
      'desk'
    )
    expect(keys(decoded.problems)).toEqual(['assistant.endpoint.kind'])
  })
})
