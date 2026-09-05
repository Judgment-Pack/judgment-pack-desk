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

/** The wire protocols the desk can speak to a model endpoint. */
export type EndpointKind = 'openai-compatible' | 'anthropic'
export const ASSISTANT_KINDS: readonly EndpointKind[] = ['openai-compatible', 'anthropic']

/**
 * The runtime tools the assistant may be configured to call.
 *
 * A closed list, and every one of them is a **read**: three questions and a
 * rehearsal. There is no write tool on it because the runtime has none, and no
 * file tool because proposing an edit is the assistant's whole reach — a
 * proposal a person accepts and the runtime then checks. A name outside this
 * list refuses the whole file by name rather than being ignored, because a
 * configuration that grants a tool nothing honours is a configuration that
 * reads as a grant.
 *
 * Mirrored from `AssistantTools` in `internal/desk/assistant.go`, and **held to
 * it by a test that reads that file** — both sides refuse by this list, and two
 * answers about what the assistant may call is worse than either one.
 */
export const ASSISTANT_TOOLS = [
  'get_schema',
  'get_example',
  'validate',
  'experimental_evaluate'
] as const
export type AssistantTool = (typeof ASSISTANT_TOOLS)[number]

/**
 * A model endpoint, as the desk records it.
 *
 * **There is no vendor, no operator and no mode here, and that absence is the
 * same design proof the identity slot carries.** An endpoint you already have
 * and an endpoint someone operates for you are the same object with a
 * different URL in it: they take the same fields, travel the same code path,
 * and neither can acquire an affordance the other lacks.
 *
 * **`kind` is the exception that proves it, and it is not a discriminator of
 * that sort.** It names the endpoint's **wire protocol** — a real difference
 * in how a request is *shaped*, because the two protocols put the credential
 * in different headers and the call on different paths, and no single request
 * could satisfy both. It says nothing about who is at the other end: nothing
 * in the desk reads the host, compares it to a list, or behaves differently
 * for one endpoint than another.
 *
 * **There is no key member, at any depth.** The desk keeps the key on this
 * machine, outside every project and outside this file; one pasted in here is
 * refused by name rather than silently persisted into somebody's repository.
 */
export interface AssistantEndpointConfig {
  url: string
  kind: EndpointKind
  model: string
  tools: string[]
}

export interface AssistantConfig {
  endpoint: AssistantEndpointConfig | null
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

/**
 * Which pane dimensions the file actually stated.
 *
 * `PanesConfig` cannot answer this: every field is filled in from the built-in
 * defaults, so a 360px Inspector width means "the file said 360" and "the file
 * said nothing" indistinguishably. That difference is load-bearing in exactly
 * one place — the Inspector's **drawer** form, whose own baseline is 320px and
 * not the column's 360px. Supplying the effective width unconditionally
 * changed the drawer on every desk that configures nothing, which is a
 * behaviour change nobody asked for dressed as a fix.
 */
export interface DeclaredPanes {
  leftWidth: boolean
  inspectorWidth: boolean
  consoleHeight: boolean
}

export const NOTHING_DECLARED: DeclaredPanes = {
  leftWidth: false,
  inspectorWidth: false,
  consoleHeight: false
}

export interface DeskConfig {
  deskConfigVersion: 1
  organization: OrganizationConfig
  user: UserConfig
  identity: IdentityConfig
  assistant: AssistantConfig
  appearance: AppearanceConfig
  panes: PanesConfig
  storage: StorageConfig
}

export const DESK_DEFAULTS: DeskConfig = {
  deskConfigVersion: 1,
  organization: { name: null, mark: null },
  user: { displayName: 'local user' },
  identity: { provider: null },
  assistant: { endpoint: null },
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
const DESK_KEYS: readonly string[] = [...COMMON_KEYS, 'identity', 'assistant']

const IDENTITY_AT_PROJECT =
  'identity may only be configured in the desk-level desk.json — a project is a shared ' +
  'checkout, and committing an issuer would push one operator’s directory onto every clone'

const ASSISTANT_AT_PROJECT =
  'assistant may only be configured in the desk-level desk.json — a project is a shared ' +
  'checkout, and committing an endpoint would push one operator’s model endpoint onto every clone'

/**
 * The sentence a key-shaped member is refused with, wherever it appears.
 *
 * **It is refused for being named that, not for being unknown.** "Unknown key"
 * is true and useless here: whoever pasted a key into a configuration file has
 * made a mistake about *where keys live*, and a refusal that only says the
 * spelling is wrong invites them to look for the right spelling. So the
 * refusal says the thing that is actually wrong, and says where the key does
 * go instead.
 */
export const KEYS_ARE_NEVER_IN_CONFIGURATION =
  'a key is never stored in configuration — the desk keeps the assistant key on this machine, ' +
  'in a file that is in no project and is never sent to this page; store it on Admin › Assistant'

/**
 * The names this decoder treats as key-shaped, wherever they appear.
 *
 * Deliberately broad and deliberately a substring test: the point is to catch
 * a credential someone reached for a plausible name for, and a member of this
 * schema that collided with one of these words would be renamed rather than
 * exempted. It is checked before "unknown key" so that the more specific
 * sentence is the one that gets said.
 */
const KEY_LIKE = ['key', 'secret', 'token', 'password', 'credential', 'bearer', 'authorization']

function isKeyLike(name: string): boolean {
  const folded = name.toLowerCase().replace(/[^a-z]/g, '')
  return KEY_LIKE.some((word) => folded.includes(word))
}

/**
 * Every credential-shaped member in the document, at any depth, named by path.
 *
 * **This runs before schema decoding, and over the parsed document rather than
 * over the schema.** The rule the file claims to hold is "a key is refused
 * wherever it is written"; what it actually held was "a key is refused where
 * this schema already looks", because the check lived inside `section()` and
 * `section()` only visits objects the schema knows about. So
 * `{"unknown": {"apiKey": "…"}}` was refused — as `unknown: unknown key` —
 * and the sentence about where keys live, which is the whole point of the
 * rule, never appeared. A reader repairing that file would have renamed
 * `unknown` and pasted the key somewhere else.
 *
 * Arrays are walked too, with an index in the path. `[{"apiKey": …}]` is a key
 * in a configuration file however unlikely the shape, and "any depth" that
 * stopped at the first array would be another rule with a quiet exception.
 */
function scanForKeys(path: string, value: unknown, problems: ConfigProblem[]): void {
  if (Array.isArray(value)) {
    value.forEach((element, index) => scanForKeys(`${path}[${index}]`, element, problems))
    return
  }
  if (value === null || typeof value !== 'object') return
  for (const [name, child] of Object.entries(value as Record<string, unknown>)) {
    const at = path === '' ? name : `${path}.${name}`
    if (isKeyLike(name)) {
      problems.push({ key: at, reason: KEYS_ARE_NEVER_IN_CONFIGURATION })
    }
    scanForKeys(at, child, problems)
  }
}

/**
 * A key that already carries the credential sentence carries nothing else.
 *
 * **There is one producer of that sentence** — `scanForKeys` — and the schema
 * walk says only "unknown key". That is a deliberate simplification of a
 * design that had both: a `unknownReason` helper picked the sentence at the
 * schema walk *as well*, which meant breaking either one left the other
 * saying it, and the mutation table reported an unheld safeguard while two
 * things held it. One producer, one row for it, and this to keep the reader
 * from seeing the same member refused twice for two reasons.
 */
function withoutRedundantReasons(problems: ConfigProblem[]): ConfigProblem[] {
  const credentialed = new Set(
    problems
      .filter((problem) => problem.reason === KEYS_ARE_NEVER_IN_CONFIGURATION)
      .map((problem) => problem.key)
  )
  return problems.filter(
    (problem) =>
      problem.reason === KEYS_ARE_NEVER_IN_CONFIGURATION || !credentialed.has(problem.key)
  )
}

/**
 * The largest organization mark this will take, in **bytes** of UTF-8.
 *
 * Measured with `TextEncoder`, not `String.length`. The two disagree by up to
 * **three** to one on a mark carrying non-ASCII — an `<svg>` with a `<title>`
 * in Japanese; a base64 `data:` URI is ASCII and unaffected — and a limit
 * documented as 64KB while counted in UTF-16 code units is a limit nobody can
 * check against the file on disk. Three and not four: a four-byte astral
 * character costs two UTF-16 code units, so it is 2:1, and the worst case is
 * the three-byte character that costs one.
 */
const MAX_MARK_BYTES = 65536

export interface DecodedConfig {
  /** Undefined where anything at all was refused. */
  values: Partial<DeskConfig> | undefined
  problems: ConfigProblem[]
  /** Which pane dimensions the file stated, as opposed to inheriting. */
  declaredPanes?: DeclaredPanes
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
  let declaredPanes: DeclaredPanes = { ...NOTHING_DECLARED }

  // The credential scan, first and over everything. See `scanForKeys`.
  scanForKeys('', parsed, problems)

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
    if (key === 'assistant' && location === 'project') {
      problems.push({ key: 'assistant', reason: ASSISTANT_AT_PROJECT })
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

  if ('assistant' in record && location === 'desk') {
    const assistant = section(record.assistant, 'assistant', ['endpoint'], problems)
    if (assistant) {
      values.assistant = { endpoint: endpointValue(assistant.endpoint, problems) }
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
      declaredPanes = {
        leftWidth: declares(left, 'width'),
        inspectorWidth: declares(inspector, 'width'),
        consoleHeight: declares(consoleSection, 'height')
      }
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

  // The one overlap the two passes produce: a key-shaped member that is also
  // an unadmitted member of a section the schema does know about is named by
  // both. Reported once.
  const unique: ConfigProblem[] = []
  const seen = new Set<string>()
  for (const problem of withoutRedundantReasons(problems)) {
    const identity = `${problem.key}\u0000${problem.reason}`
    if (seen.has(identity)) continue
    seen.add(identity)
    unique.push(problem)
  }
  if (unique.length > 0) return { values: undefined, problems: unique, declaredPanes }
  return { values, problems: [], declaredPanes }
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
 * The directory names the chassis excludes from every endpoint, and the prefix
 * it stages writes under.
 *
 * Mirrored from `skipDirs` in `internal/desk/watch.go` and from
 * `stagingPrefix` in `internal/desk/files.go` — and **held to them by a test
 * that reads those two files**, so this list cannot drift into a second answer
 * about what the desk edits. Without it, `"dir": "dist"` decodes clean, Admin
 * reports it as the pack location, and every create fails at the write with a
 * sentence about a directory nobody chose to look at.
 */
export const EXCLUDED_DIRECTORIES = ['.git', 'node_modules', 'dist', '.venv', 'vendor']
export const STAGING_PREFIX = '.jpack-desk-'

/**
 * The deepest `storage.packs.dir` the chassis will write into.
 *
 * `maxWalkDepth` in `internal/desk/files.go`, and the two are one number: the
 * listing gives up there and reports the tree as partial, and a write allowed
 * past it lands a file this API's own listing can never name. There is a test
 * on each side at the boundary.
 */
export const MAX_PACK_DIR_DEPTH = 64

/**
 * The directory new packs are written into, project-relative.
 *
 * The lexical shape the chassis will refuse anyway (`wireRelativePath`):
 * refusing it here means Admin names the key that is wrong instead of the
 * dialog failing on the write with a path nobody chose to look at. A trailing
 * separator is trimmed rather than refused — `"packs/"` is unambiguous and
 * means what it says — so `dir` never doubles a slash at use.
 *
 * That includes the directories the chassis excludes from its endpoints
 * altogether. `dist` is a plausible thing to type and a directory the desk
 * will never write into, and a configuration that decodes clean while making
 * every create fail is worse than one that is refused where it was written.
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
  const skipped = trimmed
    .split('/')
    .find(
      (part) =>
        part.toLowerCase().startsWith(STAGING_PREFIX) ||
        EXCLUDED_DIRECTORIES.some((name) => name.toLowerCase() === part.toLowerCase())
    )
  if (skipped !== undefined) {
    return bad(`must not name ${skipped}, which the desk never reads or writes`)
  }
  // The same bound the chassis holds writes to, refused where it was written
  // rather than on the write. `GET /api/files` gives up at this depth and
  // reports the tree as partial, so a pack created below it is a file the
  // desk's own listing can never name — and Admin can say which key is wrong,
  // where the dialog could only say that a write failed.
  if (trimmed.split('/').length > MAX_PACK_DIR_DEPTH) {
    return bad(
      `must be at most ${MAX_PACK_DIR_DEPTH} directories deep; the file listing gives up there`
    )
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

/** True where a section is present and states this key at all. */
function declares(section: Record<string, unknown> | undefined, member: string): boolean {
  return section !== undefined && section[member] !== undefined
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
  const bytes = new TextEncoder().encode(value).length
  if (bytes > MAX_MARK_BYTES) {
    problems.push({
      key: 'organization.mark',
      reason: `must be at most ${MAX_MARK_BYTES} bytes of UTF-8; found ${bytes}`
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

/**
 * The bounds each pane dimension is held to, and why they are not `>= 0`.
 *
 * A dimension is written into the grid, and the grid is a clipped viewport
 * frame. **Zero is the dangerous value**, not a harmless one: a pane the file
 * declares `open` at zero pixels renders as an open pane nobody can see, with
 * a toggle that appears to do nothing. And an enormous one is the same defect
 * from the other end — a console of 20,000px pushes the status strip out of a
 * frame that no longer scrolls, and a 5,000px rail leaves no main at all.
 *
 * The minima are the smallest size at which the pane is still the thing it
 * claims to be: a rail wider than its own 56px icon form, an Inspector wide
 * enough for a key and a value side by side, a console tall enough for its tab
 * strip and a line of log. The maxima are generous — this is a refusal of the
 * absurd, not a design opinion — and the CSS caps in `shell.css` are the
 * second line: they are viewport-relative, so a legal value on a large monitor
 * still cannot eat the frame on a small one.
 */
export const PANE_BOUNDS: Record<string, { min: number; max: number }> = {
  'panes.left.width': { min: 160, max: 640 },
  'panes.inspector.width': { min: 240, max: 720 },
  'panes.console.height': { min: 80, max: 720 }
}

function dimension(value: unknown, key: string, problems: ConfigProblem[]): number | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'number' || !Number.isInteger(value)) {
    problems.push({
      key,
      reason: `must be a whole number of pixels; found ${describe(value)}`
    })
    return undefined
  }
  const bounds = PANE_BOUNDS[key]
  if (bounds !== undefined && (value < bounds.min || value > bounds.max)) {
    problems.push({
      key,
      reason: `must be between ${bounds.min} and ${bounds.max} pixels inclusive; found ${value}`
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

/**
 * The endpoint object, or null.
 *
 * **Every member is required, and `tools` least optionally of all.** The other
 * three could plausibly take a default and do not, because there is no
 * endpoint a desk could invent; `tools` could not, because a defaulted tool
 * list is a capability granted by a file that never mentioned it. An empty
 * array is accepted and means what it says: an assistant that may call
 * nothing.
 *
 * The URL rule is the issuer rule, for the same reason and with the same
 * limit: `https:`, or `http:` on loopback so a locally-run endpoint works.
 * That is a rule about **transport** — a bearer credential sent in clear text
 * over a network is a credential given away — and deliberately not one about
 * who the endpoint is. Nothing reads the host.
 */
function endpointValue(
  value: unknown,
  problems: ConfigProblem[]
): AssistantEndpointConfig | null {
  if (value === undefined || value === null) return null
  const endpoint = section(
    value,
    'assistant.endpoint',
    ['url', 'kind', 'model', 'tools'],
    problems
  )
  if (!endpoint) return null

  const url = typeof endpoint.url === 'string' ? endpoint.url.trim() : undefined
  if (url === undefined || url === '') {
    problems.push({
      key: 'assistant.endpoint.url',
      reason: `must be a non-empty string; found ${describe(endpoint.url)}`
    })
  } else {
    const reason = endpointUrlProblem(url)
    if (reason !== undefined) problems.push({ key: 'assistant.endpoint.url', reason })
  }

  const kind = oneOf(endpoint.kind, 'assistant.endpoint.kind', ASSISTANT_KINDS, problems)
  if (endpoint.kind === undefined) {
    problems.push({ key: 'assistant.endpoint.kind', reason: 'required' })
  }

  const model = typeof endpoint.model === 'string' ? endpoint.model.trim() : undefined
  if (model === undefined || model === '') {
    problems.push({
      key: 'assistant.endpoint.model',
      reason: `must be a non-empty string; found ${describe(endpoint.model)}`
    })
  }

  let tools: string[] = []
  if (endpoint.tools === undefined) {
    problems.push({
      key: 'assistant.endpoint.tools',
      reason:
        'required — an absent tool list would be a capability granted by a file that never ' +
        'mentioned it; write [] for an assistant that may call nothing'
    })
  } else if (
    !Array.isArray(endpoint.tools) ||
    endpoint.tools.some((tool) => typeof tool !== 'string')
  ) {
    problems.push({
      key: 'assistant.endpoint.tools',
      reason: `must be an array of strings; found ${describe(endpoint.tools)}`
    })
  } else {
    // Named one at a time. "One of these is not allowed" makes the reader
    // check four names against a list; naming the one that is wrong does not.
    for (const tool of endpoint.tools as string[]) {
      if (!(ASSISTANT_TOOLS as readonly string[]).includes(tool)) {
        problems.push({
          key: 'assistant.endpoint.tools',
          reason:
            `${JSON.stringify(tool)} is not a tool the assistant may call; ` +
            `it accepts ${ASSISTANT_TOOLS.join(', ')}`
        })
      }
    }
    tools = endpoint.tools as string[]
  }

  return {
    url: url ?? '',
    kind: kind ?? 'openai-compatible',
    model: model ?? '',
    tools
  }
}

/**
 * The transport rule, and the credential rule beside it.
 *
 * **`https:`, or `http:` on `localhost` or `127.0.0.1`** — about transport,
 * because a bearer credential in clear text over a network is a credential
 * given away, and about transport only: nothing reads the host, compares it to
 * a list, or behaves differently for one endpoint than another.
 *
 * **Userinfo and a fragment are refused by name.** A URL is written into a
 * configuration file and shown on Admin; a credential smuggled into its
 * userinfo would be a second, unmanaged place for a secret to live, in the one
 * file this desk insists holds none — and it would make "the key is never
 * logged" false for a configuration this schema accepted. A query string is
 * *allowed*, because some gateways route on one, and is never logged.
 *
 * Held identical to `endpointURLProblem` in `internal/desk/deskfile.go` by the
 * shared fixtures both decoders read.
 */
function endpointUrlProblem(raw: string): string | undefined {
  let url: URL
  try {
    url = new URL(raw)
  } catch {
    return `must be an absolute URL; found ${JSON.stringify(raw)}`
  }
  if (url.username !== '' || url.password !== '') {
    return (
      'must not carry a user or password in the URL — a key is never written into ' +
      'configuration, and that includes into a URL'
    )
  }
  if (url.hash !== '' || raw.includes('#')) {
    return (
      'must not carry a fragment; an endpoint is a location a request is sent to, ' +
      'and a fragment is never sent'
    )
  }
  if (url.protocol === 'https:') return undefined
  if (url.protocol === 'http:' && (url.hostname === 'localhost' || url.hostname === '127.0.0.1')) {
    return undefined
  }
  return (
    'must be an https: URL, or an http: URL on localhost or 127.0.0.1 — a key sent in ' +
    'clear text over a network is a key given away'
  )
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

/**
 * A read that did not produce a file, with its provenance **carried** rather
 * than inferred.
 *
 * Every member is here because inferring it from a sibling was wrong at least
 * once. `responseReceived` was read off "is there a status?", which put a
 * `200` carrying a body this API does not promise into the transport-failure
 * bucket and had Admin say the request never got an answer. `source` is who
 * wrote `reason`: the chassis' own `{error}`, this desk's sentence about a
 * response it cannot use, or the browser's about a request that never
 * completed. A page that attributes a sentence has to know who said it.
 */
export interface ReadFailure {
  /** The reason, verbatim, whoever wrote it. */
  reason: string
  /** True where an HTTP response arrived at all. */
  responseReceived: boolean
  /** The status, present exactly where `responseReceived` is true. */
  status?: number
  /** Who authored `reason`. */
  source: 'chassis' | 'desk' | 'browser'
}

/** Which file an effective value came from. */
export type ValueSource = 'project file' | 'desk file' | 'default'

/**
 * One read of the desk-level `desk.json`, as the chassis reported it.
 *
 * `path` is carried whether or not a file was found, because it is what Admin
 * tells the reader to write. `present` and `decoded` are different facts: a
 * file that is there and refused is present with problems, and a file that is
 * absent is neither.
 */
export interface DeskLevelRead {
  /** Absolute, on this machine, and known even where nothing was read. */
  path: string
  present: boolean
  /** The decode, where a file was read at all. */
  decoded?: DecodedConfig
  /** Why nothing was read, where the file is simply absent. */
  note?: string
  /** The read did not produce a file, and it was not an absence. */
  readFailure?: ReadFailure
}

/** What Admin renders about the desk-level file. The decode, summarised. */
export interface DeskLevelSummary {
  path: string
  present: boolean
  problems: ConfigProblem[]
  note?: string
  readFailure?: ReadFailure
}

export interface EffectiveConfig {
  config: DeskConfig
  /** One badge per top-level section, so Admin can say where a value came from. */
  sources: Record<keyof Omit<DeskConfig, 'deskConfigVersion'>, ValueSource>
  problems: ConfigProblem[]
  /** The project-relative path the project file is read from. */
  path: string
  /**
   * Why no file was read, where none was **absent**. Not an error the page
   * reports — an absent config is defaults with no banner — but Admin says
   * which it is.
   */
  note?: string
  /**
   * The read did not produce a file, and it was not a 404.
   *
   * Kept apart from `note` because the two are different facts and only one of
   * them is ordinary. A 404 is a project that has not written the file. A 413,
   * a permission error or a non-UTF-8 body is a file that exists and was not
   * honoured; a dead socket establishes only that **absence was not
   * established**, which is weaker and is said as such. Reporting any of them
   * as the defaults, silently, is a desk that looks configured-by-nothing when
   * it is merely unread. Rendered on Admin and cued on the status strip.
   */
  readFailure?: ReadFailure
  /** Which pane dimensions the file that supplied them stated, rather than inherited. */
  declaredPanes: DeclaredPanes
  /**
   * The desk-level file, where one was looked for.
   *
   * Undefined means **nothing asked** — the shell's own defaults value, and
   * every test that builds one without a desk-level read. It is not the same
   * as a read that found no file, which is `present: false` with a path, and
   * Admin says which of the two it is rather than collapsing them.
   */
  desk?: DeskLevelSummary
}

export const PROJECT_CONFIG_PATH = 'jpack-desk.json'

/**
 * Merge the two decoded files onto the built-in defaults, per section.
 *
 * **The precedence is project file → desk-level file → built-in default**, and
 * it is stated once, here. A project's own file wins because it is the more
 * specific statement: the desk-level file is this machine's answer for every
 * project it opens, and a project that says something different is saying it
 * about itself.
 *
 * Two sections do not participate in that order at all, because they exist in
 * only one of the two files: `identity` and `assistant` are refused by name in
 * a project file — a project is a shared checkout, and committing either would
 * push one operator's arrangements onto every clone — so their only source is
 * the desk-level file.
 *
 * A refused file contributes nothing at all: `values` is undefined, its
 * sections fall through to whatever is behind it, and its problems travel to
 * Admin. That holds for each file independently — a refused desk-level file
 * does not refuse a good project file, and each is reported as its own.
 */
export function effectiveConfig(
  decoded: DecodedConfig | undefined,
  note?: string,
  readFailure?: ReadFailure,
  desk?: DeskLevelRead
): EffectiveConfig {
  const values = decoded?.values
  const deskValues = desk?.decoded?.values
  const from = <K extends keyof Omit<DeskConfig, 'deskConfigVersion'>>(key: K): ValueSource => {
    if (values?.[key] !== undefined) return 'project file'
    if (deskValues?.[key] !== undefined) return 'desk file'
    return 'default'
  }
  const pick = <K extends keyof Omit<DeskConfig, 'deskConfigVersion'>>(key: K): DeskConfig[K] =>
    (values?.[key] ?? deskValues?.[key] ?? DESK_DEFAULTS[key]) as DeskConfig[K]
  // The dimensions are read off whichever file actually supplied the panes.
  // Reading them off the project's decode while the desk-level file supplied
  // the section would report the Inspector's drawer against a width that file
  // never stated.
  const panesDeclaredBy =
    values?.panes !== undefined
      ? decoded?.declaredPanes
      : deskValues?.panes !== undefined
        ? desk?.decoded?.declaredPanes
        : undefined
  return {
    config: {
      deskConfigVersion: DESK_CONFIG_VERSION,
      organization: pick('organization'),
      user: pick('user'),
      // Never in a project file, so never from one: the desk-level file or the
      // default, and nothing in between.
      identity: deskValues?.identity ?? DESK_DEFAULTS.identity,
      assistant: deskValues?.assistant ?? DESK_DEFAULTS.assistant,
      appearance: pick('appearance'),
      panes: pick('panes'),
      storage: pick('storage')
    },
    sources: {
      organization: from('organization'),
      user: from('user'),
      identity: deskValues?.identity === undefined ? 'default' : 'desk file',
      assistant: deskValues?.assistant === undefined ? 'default' : 'desk file',
      appearance: from('appearance'),
      panes: from('panes'),
      storage: from('storage')
    },
    problems: decoded?.problems ?? [],
    path: PROJECT_CONFIG_PATH,
    note,
    readFailure,
    declaredPanes: panesDeclaredBy ?? NOTHING_DECLARED,
    desk:
      desk === undefined
        ? undefined
        : {
            path: desk.path,
            present: desk.present,
            problems: desk.decoded?.problems ?? [],
            note: desk.note,
            readFailure: desk.readFailure
          }
  }
}
