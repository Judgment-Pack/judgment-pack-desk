/**
 * The desk's configuration: one schema, and a decoder that refuses by name.
 *
 * **Nothing here is ever written.** Admin renders effective values, their
 * source and the exact JSON to paste; there is no PUT to a config file
 * anywhere in the desk. That is what keeps `jpackBin` out of the schema (a
 * config-supplied binary path is a local-code-execution surface — `relay.go`
 * runs it), and what means there is no admin-lockout question to answer.
 *
 * **Any problem refuses the whole file.** Partial acceptance would let a
 * typo'd key silently do nothing while its siblings applied — the reader would
 * see three of their four settings honoured and conclude the fourth was
 * ignored on purpose — and it would half-honour a file with a pasted secret in
 * it. A refused file is the built-in defaults plus a notice on Admin naming
 * every problem.
 */

export const DESK_CONFIG_VERSION = 1

export type ThemeChoice = 'system' | 'light' | 'dark'
export type Density = 'comfortable' | 'compact'

export interface OrganizationConfig {
  name: string | null
  /**
   * An inline SVG string or a `data:` URI, carried in the JSON itself and
   * bounded. Not a path: `contentOf` refuses non-UTF-8, so `GET /api/file`
   * cannot carry a raster mark, and no image endpoint is being added for a
   * logo.
   */
  mark: string | null
}

export interface UserConfig {
  displayName: string
}

/**
 * An OIDC issuer, as the desk records it.
 *
 * **There is no `kind`, no `mode`, no `operator`, no `vendor` and no
 * `clientSecret` here, and that absence is the design proof.** An issuer
 * someone else operates and an issuer you run yourself are the same object
 * with a different URL in it, so there is no shape for the desk to branch on
 * and no way for one of the two to acquire an affordance the other lacks. The
 * missing secret is the same argument from the other side: a key that does not
 * exist is refused by name when it is pasted in, rather than silently
 * persisted to a file in someone's repository.
 */
export interface IdentityProviderConfig {
  label: string | null
  issuer: string
  clientId: string
  scopes: string[]
  audience: string | null
  claims: { name: string; picture: string; subject: string }
  showRemoteAvatar: boolean
  signOut: 'local' | 'provider'
}

export interface IdentityConfig {
  provider: IdentityProviderConfig | null
}

export interface AppearanceConfig {
  theme: ThemeChoice
  density: Density
}

export interface PanesConfig {
  left: { mode: 'expanded' | 'icons'; width: number }
  inspector: { open: boolean; width: number }
  console: { open: boolean; height: number }
}

/**
 * Where this project's packs live, and what a new pack's id is built from.
 *
 * **`kind` admits only `"filesystem"`.** The desk creates a pack by writing a
 * file through the chassis file API, and there is no other mechanism here — no
 * database client, no cloud SDK, no credential slot to put one in. So the two
 * future kinds are named in the refusal and in Admin, and nothing in the desk
 * branches on this member: a create writes a file, always, and the create UI
 * never asks which kind is configured.
 *
 * `idBase` is normalised at decode to end in `/` (or left alone where it ends
 * in `#`), so a pack's id is a bare concatenation at every use and Admin shows
 * the prefix that will actually be written rather than the one that was typed.
 */
export interface StorageConfig {
  packs: {
    kind: 'filesystem'
    /** Project-relative, slash-separated, no trailing separator. */
    dir: string
    idBase: string
  }
}

export interface DeskConfig {
  deskConfigVersion: 1
  organization: OrganizationConfig
  user: UserConfig
  identity: IdentityConfig
  appearance: AppearanceConfig
  panes: PanesConfig
  storage: StorageConfig
}

export const DESK_DEFAULTS: DeskConfig = {
  deskConfigVersion: 1,
  organization: { name: null, mark: null },
  user: { displayName: 'local user' },
  identity: { provider: null },
  appearance: { theme: 'system', density: 'comfortable' },
  panes: {
    left: { mode: 'expanded', width: 248 },
    inspector: { open: false, width: 360 },
    console: { open: false, height: 240 }
  },
  storage: {
    packs: {
      kind: 'filesystem',
      dir: 'packs',
      idBase: 'https://example.invalid/judgment-packs/'
    }
  }
}

/**
 * What the header shows when no organization is configured.
 *
 * U+2011 NON-BREAKING HYPHEN, exactly as the old `.brand` string carried it.
 * The desk never fabricates a company name to put here, and never takes one
 * from a token claim: an issuer's label for a customer is not the customer's
 * brand, and reading it would let one issuer set the open desk's chrome.
 */
export const DESK_FALLBACK_NAME = 'judgment‑pack desk'

/** Where a file was read from. Each location admits a different key set. */
export type ConfigLocation = 'project' | 'desk'

export interface ConfigProblem {
  /** The offending key, path-qualified. Empty for a problem about the file. */
  key: string
  reason: string
}

/** Which top-level keys each location may carry. */
// `storage` is a COMMON key rather than a project-only one: where a project's
// packs live is a property of the project exactly as `panes` and `appearance`
// are, and the desk-level file is not read at all in phase A — so a
// PROJECT_KEYS-only special case would be a second `identity`-shaped exception
// bought for nothing. Stated here rather than left to be inferred.
const COMMON_KEYS = [
  'deskConfigVersion',
  'organization',
  'user',
  'appearance',
  'panes',
  'storage'
] as const
const PROJECT_KEYS: readonly string[] = COMMON_KEYS
const DESK_KEYS: readonly string[] = [...COMMON_KEYS, 'identity']

const IDENTITY_AT_PROJECT =
  'identity may only be configured in the desk-level desk.json — a project is a shared ' +
  'checkout, and committing an issuer would push one operator’s directory onto every clone'

/** The largest organization mark this will take, in UTF-16 code units. */
const MAX_MARK_BYTES = 65536

export interface DecodedConfig {
  /** Undefined where anything at all was refused. */
  values: Partial<DeskConfig> | undefined
  problems: ConfigProblem[]
}

/**
 * Decode one configuration file.
 *
 * Order matters and is fixed: is it JSON, is it an object, does it declare the
 * version this decoder understands, are its keys ones this location admits,
 * and only then are the values themselves read. Every refusal names its key.
 */
export function decodeDeskConfig(text: string, location: ConfigLocation): DecodedConfig {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch (cause) {
    return refuse({ key: '', reason: `the file is not JSON: ${String(cause)}` })
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return refuse({
      key: '',
      reason: `the file must be a JSON object; found ${describe(parsed)}`
    })
  }
  const record = parsed as Record<string, unknown>
  const problems: ConfigProblem[] = []

  if (!('deskConfigVersion' in record)) {
    problems.push({ key: 'deskConfigVersion', reason: 'required' })
  } else if (record.deskConfigVersion !== DESK_CONFIG_VERSION) {
    problems.push({
      key: 'deskConfigVersion',
      reason: `must be ${DESK_CONFIG_VERSION}; found ${describe(record.deskConfigVersion)}`
    })
  }

  const allowed = location === 'project' ? PROJECT_KEYS : DESK_KEYS
  for (const key of Object.keys(record)) {
    if (allowed.includes(key)) continue
    if (key === 'identity' && location === 'project') {
      problems.push({ key: 'identity', reason: IDENTITY_AT_PROJECT })
      continue
    }
    problems.push({ key, reason: 'unknown key' })
  }

  const values: Partial<DeskConfig> = {}

  if ('organization' in record) {
    const organization = section(record.organization, 'organization', ['name', 'mark'], problems)
    if (organization) {
      const name = organizationName(organization.name, problems)
      const mark = markValue(organization.mark, problems)
      values.organization = {
        name: name ?? DESK_DEFAULTS.organization.name,
        mark: mark ?? DESK_DEFAULTS.organization.mark
      }
    }
  }

  if ('user' in record) {
    const user = section(record.user, 'user', ['displayName'], problems)
    if (user) {
      let displayName = DESK_DEFAULTS.user.displayName
      if ('displayName' in user) {
        if (typeof user.displayName !== 'string' || user.displayName.trim() === '') {
          problems.push({
            key: 'user.displayName',
            reason: `must be a non-empty string; found ${describe(user.displayName)}`
          })
        } else {
          displayName = user.displayName
        }
      }
      values.user = { displayName }
    }
  }

  if ('identity' in record && location === 'desk') {
    const identity = section(record.identity, 'identity', ['provider'], problems)
    if (identity) {
      values.identity = { provider: providerValue(identity.provider, problems) }
    }
  }

  if ('appearance' in record) {
    const appearance = section(record.appearance, 'appearance', ['theme', 'density'], problems)
    if (appearance) {
      values.appearance = {
        theme:
          oneOf(appearance.theme, 'appearance.theme', ['system', 'light', 'dark'], problems) ??
          DESK_DEFAULTS.appearance.theme,
        density:
          oneOf(appearance.density, 'appearance.density', ['comfortable', 'compact'], problems) ??
          DESK_DEFAULTS.appearance.density
      }
    }
  }

  if ('panes' in record) {
    const panes = section(record.panes, 'panes', ['left', 'inspector', 'console'], problems)
    if (panes) {
      // Each sub-section is optional: a file that sets only the rail is a
      // file that means the other two to stay as they are, and reporting them
      // absent would refuse it for saying nothing.
      const left = 'left' in panes ? section(panes.left, 'panes.left', ['mode', 'width'], problems) : undefined
      const inspector =
        'inspector' in panes
          ? section(panes.inspector, 'panes.inspector', ['open', 'width'], problems)
          : undefined
      const consoleSection =
        'console' in panes
          ? section(panes.console, 'panes.console', ['open', 'height'], problems)
          : undefined
      values.panes = {
        left: {
          mode:
            (left &&
              oneOf(left.mode, 'panes.left.mode', ['expanded', 'icons'], problems)) ??
            DESK_DEFAULTS.panes.left.mode,
          width:
            (left && dimension(left.width, 'panes.left.width', problems)) ??
            DESK_DEFAULTS.panes.left.width
        },
        inspector: {
          open:
            (inspector && boolean(inspector.open, 'panes.inspector.open', problems)) ??
            DESK_DEFAULTS.panes.inspector.open,
          width:
            (inspector && dimension(inspector.width, 'panes.inspector.width', problems)) ??
            DESK_DEFAULTS.panes.inspector.width
        },
        console: {
          open:
            (consoleSection && boolean(consoleSection.open, 'panes.console.open', problems)) ??
            DESK_DEFAULTS.panes.console.open,
          height:
            (consoleSection &&
              dimension(consoleSection.height, 'panes.console.height', problems)) ??
            DESK_DEFAULTS.panes.console.height
        }
      }
    }
  }

  if ('storage' in record) {
    const storage = section(record.storage, 'storage', ['packs'], problems)
    if (storage) {
      const packs =
        'packs' in storage
          ? section(storage.packs, 'storage.packs', ['kind', 'dir', 'idBase'], problems)
          : undefined
      values.storage = {
        packs: {
          kind: (packs && storageKind(packs.kind, problems)) ?? DESK_DEFAULTS.storage.packs.kind,
          dir: (packs && packDir(packs.dir, problems)) ?? DESK_DEFAULTS.storage.packs.dir,
          idBase: (packs && idBase(packs.idBase, problems)) ?? DESK_DEFAULTS.storage.packs.idBase
        }
      }
    }
  }

  if (problems.length > 0) return { values: undefined, problems }
  return { values, problems: [] }
}

/**
 * The one storage kind there is.
 *
 * Its own function rather than `oneOf`, so the refusal can name the two kinds
 * that are not available yet. `oneOf` would report `must be one of
 * "filesystem"`, which is true and tells a reader who typed `"database"`
 * nothing about why.
 */
function storageKind(
  value: unknown,
  problems: ConfigProblem[]
): 'filesystem' | undefined {
  if (value === undefined) return undefined
  if (value !== 'filesystem') {
    problems.push({
      key: 'storage.packs.kind',
      reason:
        `must be "filesystem"; "database" and "cloud storage" are not available yet, ` +
        `found ${describe(value)}`
    })
    return undefined
  }
  return 'filesystem'
}

/**
 * The directory new packs are written into, project-relative.
 *
 * The lexical shape the chassis will refuse anyway (`wireRelativePath`):
 * refusing it here means Admin names the key that is wrong instead of the
 * dialog failing on the write with a path nobody chose to look at. A trailing
 * separator is trimmed rather than refused — `"packs/"` is unambiguous and
 * means what it says — so `dir` never doubles a slash at use.
 */
function packDir(value: unknown, problems: ConfigProblem[]): string | undefined {
  if (value === undefined) return undefined
  const bad = (reason: string): undefined => {
    problems.push({ key: 'storage.packs.dir', reason })
    return undefined
  }
  if (typeof value !== 'string') return bad(`must be a string; found ${describe(value)}`)
  const trimmed = value.trim().replace(/\/+$/, '')
  if (trimmed === '') return bad('must name a directory inside the project')
  if (trimmed.startsWith('/')) return bad('must be relative to the project, not absolute')
  if (trimmed.includes('\\') || trimmed.includes(':')) {
    return bad('must be slash-separated and carry no backslash or colon')
  }
  if (trimmed.split('/').some((part) => part === '..' || part === '.' || part === '')) {
    return bad('must not contain an empty, "." or ".." path segment')
  }
  return trimmed
}

/**
 * The prefix a new pack's `id` is built from.
 *
 * The JPS `id` member is `format: uri`, so this must parse as one. It is
 * normalised to end in a separator here and nowhere else: every use is then a
 * bare `idBase + slug`, and Admin displays the prefix that will actually be
 * written rather than the one that happened to be typed.
 */
function idBase(value: unknown, problems: ConfigProblem[]): string | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'string' || value.trim() === '') {
    problems.push({
      key: 'storage.packs.idBase',
      reason: `must be a non-empty string; found ${describe(value)}`
    })
    return undefined
  }
  const trimmed = value.trim()
  try {
    new URL(trimmed)
  } catch {
    problems.push({
      key: 'storage.packs.idBase',
      reason: `must be a URI, because a pack's id member is one; found ${describe(value)}`
    })
    return undefined
  }
  return trimmed.endsWith('/') || trimmed.endsWith('#') ? trimmed : `${trimmed}/`
}

function refuse(problem: ConfigProblem): DecodedConfig {
  return { values: undefined, problems: [problem] }
}

function describe(value: unknown): string {
  if (value === null) return 'null'
  if (Array.isArray(value)) return 'an array'
  return typeof value === 'string' ? JSON.stringify(value) : `${typeof value} ${String(value)}`
}

/** One nested object, with its own unknown keys refused by path. */
function section(
  value: unknown,
  key: string,
  allowed: readonly string[],
  problems: ConfigProblem[]
): Record<string, unknown> | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    problems.push({ key, reason: `must be an object; found ${describe(value)}` })
    return undefined
  }
  const record = value as Record<string, unknown>
  for (const member of Object.keys(record)) {
    if (!allowed.includes(member)) {
      problems.push({ key: `${key}.${member}`, reason: 'unknown key' })
    }
  }
  return record
}

function optionalString(
  value: unknown,
  key: string,
  problems: ConfigProblem[]
): string | null | undefined {
  if (value === undefined) return undefined
  if (value === null) return null
  if (typeof value !== 'string') {
    problems.push({ key, reason: `must be a string or null; found ${describe(value)}` })
    return undefined
  }
  return value
}

/**
 * The organization name: a non-empty string, or `null` for the desk's own.
 *
 * The same rule `user.displayName` gets, and for the same reason. An empty
 * string is not a name: it renders as a blank brand link and a `·` monogram,
 * and it reaches neither the `?? DESK_FALLBACK_NAME` fallback nor a refusal,
 * so the one case the fallback exists for would be the one case it missed.
 */
function organizationName(
  value: unknown,
  problems: ConfigProblem[]
): string | null | undefined {
  const name = optionalString(value, 'organization.name', problems)
  if (typeof name === 'string' && name.trim() === '') {
    problems.push({
      key: 'organization.name',
      reason: `must be a non-empty string or null; found ${describe(value)}`
    })
    return undefined
  }
  return name
}

function markValue(value: unknown, problems: ConfigProblem[]): string | null | undefined {
  if (value === undefined) return undefined
  if (value === null) return null
  if (typeof value !== 'string') {
    problems.push({
      key: 'organization.mark',
      reason: `must be an inline SVG string, a data: URI, or null; found ${describe(value)}`
    })
    return undefined
  }
  const trimmed = value.trim()
  if (!trimmed.startsWith('<svg') && !trimmed.startsWith('data:image/')) {
    problems.push({
      key: 'organization.mark',
      reason: 'must begin with "<svg" or "data:image/" — a file path is not accepted'
    })
    return undefined
  }
  if (value.length > MAX_MARK_BYTES) {
    problems.push({
      key: 'organization.mark',
      reason: `must be at most ${MAX_MARK_BYTES} characters; found ${value.length}`
    })
    return undefined
  }
  return value
}

function oneOf<T extends string>(
  value: unknown,
  key: string,
  choices: readonly T[],
  problems: ConfigProblem[]
): T | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'string' || !choices.includes(value as T)) {
    problems.push({
      key,
      reason: `must be one of ${choices.map((choice) => JSON.stringify(choice)).join(', ')}; found ${describe(value)}`
    })
    return undefined
  }
  return value as T
}

function boolean(value: unknown, key: string, problems: ConfigProblem[]): boolean | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'boolean') {
    problems.push({ key, reason: `must be a boolean; found ${describe(value)}` })
    return undefined
  }
  return value
}

function dimension(value: unknown, key: string, problems: ConfigProblem[]): number | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'number' || !Number.isInteger(value) || value < 0) {
    problems.push({
      key,
      reason: `must be a whole number of pixels, zero or more; found ${describe(value)}`
    })
    return undefined
  }
  return value
}

/**
 * The provider object, or null.
 *
 * The issuer must be `https:` — or `http:` on loopback, so a locally-run test
 * issuer works. That is a rule about **transport**, and deliberately not one
 * about who the issuer is: nothing here reads the host, compares it to a list,
 * or behaves differently for one issuer than another.
 */
function providerValue(
  value: unknown,
  problems: ConfigProblem[]
): IdentityProviderConfig | null {
  if (value === undefined || value === null) return null
  const provider = section(
    value,
    'identity.provider',
    ['label', 'issuer', 'clientId', 'scopes', 'audience', 'claims', 'showRemoteAvatar', 'signOut'],
    problems
  )
  if (!provider) return null

  const issuer = typeof provider.issuer === 'string' ? provider.issuer : undefined
  if (issuer === undefined) {
    problems.push({
      key: 'identity.provider.issuer',
      reason: `must be a string; found ${describe(provider.issuer)}`
    })
  } else if (!isAcceptableIssuer(issuer)) {
    problems.push({
      key: 'identity.provider.issuer',
      reason: 'must be an https: URL, or an http: URL on localhost or 127.0.0.1'
    })
  }

  const clientId = typeof provider.clientId === 'string' ? provider.clientId : undefined
  if (clientId === undefined || clientId === '') {
    problems.push({
      key: 'identity.provider.clientId',
      reason: `must be a non-empty string; found ${describe(provider.clientId)}`
    })
  }

  let scopes: string[] = ['openid', 'profile']
  if (provider.scopes !== undefined) {
    if (
      !Array.isArray(provider.scopes) ||
      provider.scopes.some((scope) => typeof scope !== 'string')
    ) {
      problems.push({
        key: 'identity.provider.scopes',
        reason: `must be an array of strings; found ${describe(provider.scopes)}`
      })
    } else {
      scopes = provider.scopes as string[]
    }
  }

  const claims = { name: 'name', picture: 'picture', subject: 'sub' }
  if (provider.claims !== undefined) {
    const declared = section(
      provider.claims,
      'identity.provider.claims',
      ['name', 'picture', 'subject'],
      problems
    )
    if (declared) {
      for (const member of ['name', 'picture', 'subject'] as const) {
        const spelled = declared[member]
        if (spelled === undefined) continue
        if (typeof spelled !== 'string' || spelled === '') {
          problems.push({
            key: `identity.provider.claims.${member}`,
            reason: `must be a non-empty string; found ${describe(spelled)}`
          })
        } else {
          claims[member] = spelled
        }
      }
    }
  }

  return {
    label: optionalString(provider.label, 'identity.provider.label', problems) ?? null,
    issuer: issuer ?? '',
    clientId: clientId ?? '',
    scopes,
    audience: optionalString(provider.audience, 'identity.provider.audience', problems) ?? null,
    claims,
    showRemoteAvatar:
      boolean(provider.showRemoteAvatar, 'identity.provider.showRemoteAvatar', problems) ?? false,
    signOut:
      oneOf(provider.signOut, 'identity.provider.signOut', ['local', 'provider'], problems) ??
      'local'
  }
}

function isAcceptableIssuer(issuer: string): boolean {
  let url: URL
  try {
    url = new URL(issuer)
  } catch {
    return false
  }
  if (url.protocol === 'https:') return true
  return url.protocol === 'http:' && (url.hostname === 'localhost' || url.hostname === '127.0.0.1')
}

/** Which file an effective value came from. */
export type ValueSource = 'project file' | 'default'

export interface EffectiveConfig {
  config: DeskConfig
  /** One badge per top-level section, so Admin can say where a value came from. */
  sources: Record<keyof Omit<DeskConfig, 'deskConfigVersion'>, ValueSource>
  problems: ConfigProblem[]
  /** The project-relative path the project file is read from. */
  path: string
  /**
   * Why no file was read, where none was. Not an error the page reports — an
   * absent config is defaults with no banner — but Admin says which it is.
   */
  note?: string
}

export const PROJECT_CONFIG_PATH = 'jpack-desk.json'

/**
 * Merge one decoded project file onto the built-in defaults, per section.
 *
 * A refused file contributes nothing at all: `values` is undefined, every
 * source badge reads `default`, and the problems travel to Admin.
 */
export function effectiveConfig(decoded: DecodedConfig | undefined, note?: string): EffectiveConfig {
  const values = decoded?.values
  const from = <K extends keyof Omit<DeskConfig, 'deskConfigVersion'>>(key: K): ValueSource =>
    values?.[key] === undefined ? 'default' : 'project file'
  return {
    config: {
      deskConfigVersion: DESK_CONFIG_VERSION,
      organization: values?.organization ?? DESK_DEFAULTS.organization,
      user: values?.user ?? DESK_DEFAULTS.user,
      // Identity is never in a project file. Phase A reads no desk-level file,
      // so this is the default in every running desk today, and Admin says so
      // in words rather than leaving the reader to infer it.
      identity: values?.identity ?? DESK_DEFAULTS.identity,
      appearance: values?.appearance ?? DESK_DEFAULTS.appearance,
      panes: values?.panes ?? DESK_DEFAULTS.panes,
      storage: values?.storage ?? DESK_DEFAULTS.storage
    },
    sources: {
      organization: from('organization'),
      user: from('user'),
      identity: 'default',
      appearance: from('appearance'),
      panes: from('panes'),
      storage: from('storage')
    },
    problems: decoded?.problems ?? [],
    path: PROJECT_CONFIG_PATH,
    note
  }
}
