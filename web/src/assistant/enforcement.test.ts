/**
 * The assistant slot's boundary, enforced rather than described.
 *
 * The identity slot's enforcement test is the model, and the load-bearing
 * assertion is the same one: **the key sets are asserted whole**, at the type
 * level and again against the declaration text, so the rule being held is "one
 * nullable field, and exactly these four members" rather than "not one of
 * these forbidden names". A blacklist of discriminators cannot state "one
 * field", because the thing it excludes is *any second field*, and that set
 * has no enumeration.
 *
 * **`kind` is inside the four, and that is the one place this differs from
 * identity.** It is not an exception smuggled in: it names the endpoint's wire
 * protocol, which is a real difference in how a request is *shaped* — the two
 * protocols put the credential in different headers and the call on a
 * different path, so no single request could satisfy both. It is not a
 * difference in *who is at the other end*, and the guards below are written
 * against exactly that distinction: nothing may compare the URL to anything,
 * and there is no vendor, operator or mode member for a supplied endpoint to
 * become a third case in.
 *
 * The last two guards are string enumerations over the source tree. They
 * cannot catch a novel spelling and are labelled as the weak guards they are,
 * because a guard whose limits are not written down gets read as a proof.
 */
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  ASSISTANT_ENGINES,
  ASSISTANT_KINDS,
  ASSISTANT_THINKING,
  ASSISTANT_TOOLS,
  DESK_DEFAULTS,
  KEYS_ARE_NEVER_IN_CONFIGURATION,
  decodeDeskConfig,
  type AssistantConfig,
  type AssistantEndpointConfig
} from '../config/deskConfig'
import { PROBE_DIAGNOSTICS } from './client'
import { PREFILLED_URL } from './endpointDraft'
import { LISTING_SUFFIX } from './modelListing'
import { suffixProblem } from './session'
import type { AssistantSlot } from './useAssistantSlot'

const SRC = join(import.meta.dirname, '..')

function read(relative: string): string {
  return readFileSync(join(SRC, relative), 'utf8')
}

/**
 * Every source under these directories, **all the way down**.
 *
 * It used to read one level and stop, which meant the two guards below —
 * neither of them strong, both of them enumerations — did not look at
 * `assistant/engines/` at all. An adapter is exactly where a vendor's base URL
 * or a comparison against one would be written, and the `vercel` adapter is
 * three levels down and carries the one URL literal the assistant has.
 */
function sourcesUnder(...directories: string[]): { path: string; text: string }[] {
  const found: { path: string; text: string }[] = []
  const walk = (directory: string) => {
    for (const entry of readdirSync(join(SRC, directory), { withFileTypes: true })) {
      const path = `${directory}/${entry.name}`
      if (entry.isDirectory()) {
        walk(path)
        continue
      }
      if (!entry.isFile()) continue
      if (!entry.name.endsWith('.ts') && !entry.name.endsWith('.tsx')) continue
      found.push({ path, text: read(path) })
    }
  }
  for (const directory of directories) walk(directory)
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

/** The members an interface body declares, at its own depth. */
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

/** True only where two key sets are the same set, both ways round. */
type Exactly<A, B> = [A] extends [B] ? ([B] extends [A] ? true : false) : false

/**
 * The serialized assistant slot: one nullable field and two settings about how
 * it runs — and **no fourth member**, which is what this set being asserted
 * whole is for. `engine` and `thinking` say *how*; neither is a discriminator
 * and neither is a place a supplied endpoint could become a different thing
 * from one you run yourself.
 */
const ASSISTANT_KEYS = ['endpoint', 'engine', 'thinking'] as const

/** The endpoint object's members, exactly as the schema declares them. */
const ENDPOINT_KEYS = ['url', 'kind', 'model', 'tools'] as const

/** What the future assistant pane reads. */
const SLOT_KEYS = ['state', 'endpoint', 'keyPresent', 'engine', 'thinking'] as const

const assistantKeysAreExact: Exactly<keyof AssistantConfig, (typeof ASSISTANT_KEYS)[number]> = true
const endpointKeysAreExact: Exactly<
  keyof AssistantEndpointConfig,
  (typeof ENDPOINT_KEYS)[number]
> = true
const slotKeysAreExact: Exactly<keyof AssistantSlot, (typeof SLOT_KEYS)[number]> = true

/** One desk-level file carrying whatever this case is about. */
function decodeDesk(assistant: unknown) {
  return decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1, assistant }), 'desk')
}

const GOOD_ENDPOINT = {
  url: 'https://api.example.invalid/v1',
  kind: 'openai-compatible',
  model: 'a-model',
  tools: []
}

describe('(1) the assistant slot is one nullable field, two settings, and four endpoint members', () => {
  it('declares the assistant key set as exactly endpoint, engine and thinking', () => {
    expect(assistantKeysAreExact).toBe(true)
    expect(membersOf(interfaceBody(read('config/deskConfig.ts'), 'AssistantConfig'))).toEqual([
      ...ASSISTANT_KEYS
    ])
  })

  it('declares the endpoint member set as exactly url, kind, model and tools', () => {
    // Whole, not a blacklist. `assistant.endpoint.operator` would walk past
    // any list of forbidden names somebody happened to think of.
    expect(endpointKeysAreExact).toBe(true)
    expect(
      membersOf(interfaceBody(read('config/deskConfig.ts'), 'AssistantEndpointConfig'))
    ).toEqual([...ENDPOINT_KEYS])
  })

  it('refuses any member the endpoint object does not declare, by name', () => {
    for (const name of ['vendor', 'operator', 'mode', 'organization', 'provider', 'deployment']) {
      const decoded = decodeDesk({ endpoint: { ...GOOD_ENDPOINT, [name]: 'v' } })
      expect(decoded.values, `${name} refuses the whole file`).toBeUndefined()
      expect(decoded.problems.map((problem) => problem.key)).toContain(
        `assistant.endpoint.${name}`
      )
    }
  })

  it('refuses any member beside the endpoint, by name', () => {
    // The other depth, and the one an inner-object-only guard leaves open: a
    // `kind` here would branch the desk just as effectively as one inside.
    // `engine` and `thinking` are not in this list because they are declared
    // members now — and the set above is what says there is no fourth.
    for (const name of ['kind', 'mode', 'operator', 'vendor', 'supplied']) {
      const decoded = decodeDesk({ endpoint: null, [name]: 'v' })
      expect(decoded.values, `assistant.${name} refuses the whole file`).toBeUndefined()
      expect(decoded.problems.map((problem) => problem.key)).toContain(`assistant.${name}`)
    }
  })

  it('exposes a nullable endpoint with no discriminator to branch on', () => {
    expect(slotKeysAreExact).toBe(true)
    expect(membersOf(interfaceBody(read('assistant/useAssistantSlot.ts'), 'AssistantSlot'))).toEqual(
      [...SLOT_KEYS]
    )
    // Two states, and the third would be the place a supplied endpoint became
    // a different thing from one you run yourself.
    const slot = read('assistant/useAssistantSlot.ts')
    expect(slot).toContain("'none' | 'configured'")
    for (const forbidden of ['bring-your-own', 'supplied', 'byo', 'managed']) {
      expect(slot, `the slot has a ${forbidden} state`).not.toContain(`'${forbidden}'`)
    }
  })
})

describe('(1a) engine and thinking are closed lists that say how, not whether', () => {
  it('accepts every engine and every tier the desk declares', () => {
    for (const engine of ASSISTANT_ENGINES) {
      const decoded = decodeDesk({ endpoint: GOOD_ENDPOINT, engine })
      expect(decoded.problems, engine).toEqual([])
      expect(decoded.values?.assistant?.engine).toBe(engine)
    }
    for (const thinking of ASSISTANT_THINKING) {
      const decoded = decodeDesk({ endpoint: GOOD_ENDPOINT, thinking })
      expect(decoded.problems, thinking).toEqual([])
      expect(decoded.values?.assistant?.thinking).toBe(thinking)
    }
  })

  it('refuses an unknown value, and a wrong type, by its exact key path', () => {
    // Named the way `assistant.endpoint.kind` is named. An engine nobody
    // certified is a setting that reads as a grant to whoever wrote it, so it
    // refuses the whole file rather than falling back to the default.
    for (const [member, value] of [
      ['engine', 'langchain'],
      ['engine', 1],
      ['engine', null],
      ['thinking', 'hard'],
      ['thinking', true]
    ] as [string, unknown][]) {
      const decoded = decodeDesk({ endpoint: GOOD_ENDPOINT, [member]: value })
      expect(decoded.values, `assistant.${member} = ${String(value)} was accepted`).toBeUndefined()
      expect(decoded.problems.map((problem) => problem.key)).toContain(`assistant.${member}`)
    }
  })

  it('allows both beside a null endpoint, because they say how and not whether', () => {
    const decoded = decodeDesk({ endpoint: null, engine: 'builtin', thinking: 'ultra' })
    expect(decoded.problems).toEqual([])
    expect(decoded.values?.assistant).toEqual({
      endpoint: null,
      engine: 'builtin',
      thinking: 'ultra'
    })
  })

  it('defaults to vercel and off where the file says nothing', () => {
    expect(DESK_DEFAULTS.assistant.engine).toBe('vercel')
    expect(DESK_DEFAULTS.assistant.thinking).toBe('off')
    const decoded = decodeDesk({ endpoint: GOOD_ENDPOINT })
    expect(decoded.values?.assistant?.engine).toBe('vercel')
    expect(decoded.values?.assistant?.thinking).toBe('off')
  })

  it('is the same three lists the chassis refuses by', () => {
    // Two implementations of one contract drift; the fixtures hold the
    // verdicts and this holds the vocabularies, read out of the Go source the
    // same way the tool list is.
    //
    // **The kinds are on it now**, and they were the one closed list held to
    // the chassis by nothing at all: a protocol added on one side and not the
    // other is a file one decoder accepts and the other refuses — and the
    // refusing one is the one that decides whether a credential leaves this
    // machine.
    const source = readFileSync(
      join(SRC, '..', '..', 'internal', 'desk', 'assistant.go'),
      'utf8'
    )
    for (const [declaration, list] of [
      ['AssistantKinds', ASSISTANT_KINDS],
      ['AssistantEngines', ASSISTANT_ENGINES],
      ['AssistantThinkingTiers', ASSISTANT_THINKING]
    ] as [string, readonly string[]][]) {
      const found = new RegExp(`var ${declaration} = \\[\\]string\\{([^}]*)\\}`).exec(source)
      expect(found, `${declaration} is declared in internal/desk/assistant.go`).not.toBeNull()
      const go = [...found![1]!.matchAll(/"([^"]+)"/g)].map((match) => match[1]!)
      expect(go).toEqual([...list])
    }
  })
})

describe('(2) a key is refused wherever it is written, and refused for being one', () => {
  it('refuses a key-shaped member inside the endpoint, with the sentence about keys', () => {
    for (const name of ['apiKey', 'key', 'secret', 'token', 'api_key', 'bearerToken']) {
      const decoded = decodeDesk({ endpoint: { ...GOOD_ENDPOINT, [name]: 'sk-nope' } })
      expect(decoded.values, `${name} refuses the whole file`).toBeUndefined()
      const problem = decoded.problems.find(
        (each) => each.key === `assistant.endpoint.${name}`
      )
      expect(problem, `${name} is refused by name`).toBeDefined()
      // **Not "unknown key".** Whoever pasted a key has made a mistake about
      // where keys live, and a refusal that only says the spelling is wrong
      // invites them to go looking for the right spelling.
      expect(problem!.reason).toBe(KEYS_ARE_NEVER_IN_CONFIGURATION)
      expect(problem!.reason).toContain('never stored in configuration')
    }
  })

  it('refuses one at every other depth too, including the top level', () => {
    const depths: [string, unknown][] = [
      ['apiKey', { deskConfigVersion: 1, apiKey: 'sk-nope' }],
      ['assistant.apiKey', { deskConfigVersion: 1, assistant: { apiKey: 'sk-nope' } }],
      [
        'identity.provider.clientSecret',
        {
          deskConfigVersion: 1,
          identity: {
            provider: { issuer: 'https://issuer.example/', clientId: 'x', clientSecret: 's' }
          }
        }
      ],
      ['organization.secret', { deskConfigVersion: 1, organization: { secret: 's' } }],
      ['storage.packs.token', { deskConfigVersion: 1, storage: { packs: { token: 't' } } }]
    ]
    for (const [key, file] of depths) {
      const decoded = decodeDeskConfig(JSON.stringify(file), 'desk')
      expect(decoded.values, `${key} refuses the whole file`).toBeUndefined()
      const problem = decoded.problems.find((each) => each.key === key)
      expect(problem, `${key} is refused by name`).toBeDefined()
      expect(problem!.reason).toBe(KEYS_ARE_NEVER_IN_CONFIGURATION)
    }
  })

  it('says where the key does go instead, rather than only where it does not', () => {
    expect(KEYS_ARE_NEVER_IN_CONFIGURATION).toContain('on this machine')
    expect(KEYS_ARE_NEVER_IN_CONFIGURATION).toContain('Admin › Assistant')
  })

  it('refuses the assistant member in a project file, with its own reason', () => {
    // A project is a shared checkout. Committing an endpoint would push one
    // operator's model endpoint onto every clone of it.
    const decoded = decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, assistant: { endpoint: null } }),
      'project'
    )
    expect(decoded.values).toBeUndefined()
    const problem = decoded.problems.find((each) => each.key === 'assistant')
    expect(problem).toBeDefined()
    expect(problem!.reason).toContain('desk-level desk.json')
    expect(problem!.reason).toContain('shared checkout')
  })
})

describe('(3) the tool list is closed, and is one list across both sides', () => {
  it('accepts the five and refuses anything else by name', () => {
    // Written out rather than compared to the constant, so that adding a name
    // to the list is a line in this file too. `list_examples` is here because
    // the runtime's `author_pack` prompt calls it.
    expect([...ASSISTANT_TOOLS]).toEqual([
      'get_schema',
      'list_examples',
      'get_example',
      'validate',
      'experimental_evaluate'
    ])
    const good = decodeDesk({ endpoint: { ...GOOD_ENDPOINT, tools: [...ASSISTANT_TOOLS] } })
    expect(good.problems).toEqual([])
    expect(good.values?.assistant?.endpoint?.tools).toEqual([...ASSISTANT_TOOLS])

    for (const tool of [
      'write_file',
      'get_pack',
      'evaluate',
      'bash',
      'GET_SCHEMA',
      'list_packs'
    ]) {
      const decoded = decodeDesk({ endpoint: { ...GOOD_ENDPOINT, tools: [tool] } })
      expect(decoded.values, `${tool} refuses the whole file`).toBeUndefined()
      const problem = decoded.problems.find((each) => each.key === 'assistant.endpoint.tools')
      expect(problem, `${tool} is refused`).toBeDefined()
      // Named one at a time: "one of these is not allowed" makes a reader
      // check five names against a list.
      expect(problem!.reason).toContain(JSON.stringify(tool))
    }
  })

  it('is the same list the chassis refuses by', () => {
    // Both sides refuse by this list, and two answers about what the assistant
    // may call is worse than either one on its own. Read out of the Go source
    // the same way the excluded-directory mirror is.
    const source = readFileSync(
      join(SRC, '..', '..', 'internal', 'desk', 'assistant.go'),
      'utf8'
    )
    const declaration = /var AssistantTools = \[\]string\{([^}]*)\}/.exec(source)
    expect(declaration, 'AssistantTools is declared in internal/desk/assistant.go').not.toBeNull()
    const go = [...declaration![1]!.matchAll(/"([^"]+)"/g)].map((match) => match[1]!)
    expect(go).toEqual([...ASSISTANT_TOOLS])
  })

  it('requires the list rather than defaulting it', () => {
    // A defaulted tool list is a capability granted by a file that never
    // mentioned it.
    const { url, kind, model } = GOOD_ENDPOINT
    const decoded = decodeDesk({ endpoint: { url, kind, model } })
    expect(decoded.values).toBeUndefined()
    const problem = decoded.problems.find((each) => each.key === 'assistant.endpoint.tools')
    expect(problem!.reason).toContain('required')
    // And an explicitly empty list is accepted, meaning what it says.
    const empty = decodeDesk({ endpoint: { url, kind, model, tools: [] } })
    expect(empty.problems).toEqual([])
    expect(empty.values?.assistant?.endpoint?.tools).toEqual([])
  })
})

describe('(4) no component branches on the endpoint — a WEAK, enumerated guard', () => {
  it('finds no comparison against an endpoint URL in assistant/, config/ or routes/', () => {
    // Weak by construction: it looks for the shapes a comparison is usually
    // written in, and a novel spelling walks straight past it. (1) is what
    // actually holds the design in place.
    // `typeof endpoint.url === 'string'` is a type check and not a comparison
    // against a URL, so it is excluded by the lookbehind rather than by an
    // allow-list that would also excuse a real one written next to it.
    const comparisons = [
      /(?<!typeof\s)\bendpoint\.url\s*[=!]==/,
      /url\.includes\(/,
      /\.includes\(\s*['"]https/
    ]
    for (const source of sourcesUnder('assistant', 'config', 'routes')) {
      if (source.path.includes('.test.')) continue
      for (const pattern of comparisons) {
        expect(source.text, `${source.path} compares an endpoint`).not.toMatch(pattern)
      }
    }
  })

  it('compares a host to nothing but the loopback names', () => {
    // The sharper half, and the one worth having. Both URL rules — the
    // issuer's and the endpoint's — do compare a hostname, because `http:` is
    // permitted on loopback and nowhere else. That is a statement about
    // **transport**: a bearer credential in clear text over a network is a
    // credential given away. Every host literal in these directories is
    // enumerated here and has to be one of the loopback spellings, so a
    // comparison against a *vendor's* host cannot hide among them.
    const loopback = ["'localhost'", "'127.0.0.1'", "'::1'"]
    for (const source of sourcesUnder('assistant', 'config', 'routes')) {
      if (source.path.includes('.test.')) continue
      for (const match of source.text.matchAll(/host(?:name)?\s*[=!]==\s*(('[^']*')|("[^"]*"))/g)) {
        expect(loopback, `${source.path} compares a host to ${match[1]}`).toContain(match[1])
      }
    }
  })
})

describe('(5) no endpoint literal in the source — a WEAK, enumerated guard', () => {
  it('finds no reachable https:// endpoint literal outside tests and docs', () => {
    const allowed = [
      'https://github.com/Judgment-Pack/judgment-pack-desk',
      'https://github.com/Judgment-Pack/judgment-pack-runtime',
      // Reserved by RFC 2606 precisely so a placeholder can never resolve.
      // The paste block on Admin shows one, and a reader who copies it gets a
      // URL that cannot reach anything rather than one that reaches us.
      'https://api.example.invalid/',
      'https://example.invalid/judgment-packs/',
      // The placeholder origin the `vercel` adapter's providers are built
      // against and which nothing ever resolves — reserved by the same RFC, for
      // the same reason. It is a URL an engine composes so that the desk can
      // reduce it to a path suffix, and if it ever escaped to a real `fetch` it
      // would fail rather than arrive somewhere.
      'https://relay.invalid'
    ]
    // **The three prefills, admitted in one module and as nothing but values
    // of one table.** Admin's form offers the base each protocol's own
    // reference documents, so that choosing a wire protocol does not mean
    // retyping an address the README already names — and every one of them is
    // replaced by typing over it. That is a *default in an editable field*,
    // which is a different thing from a destination the desk holds: nothing
    // reads them back, and the sharper guard above — every host comparison in
    // these directories is a loopback name — is what says so and is untouched.
    // Admitting them anywhere else, or under any other name, still fails.
    const prefills = Object.values(PREFILLED_URL)
    const PREFILL_MODULE = 'assistant/endpointDraft.ts'
    for (const source of sourcesUnder('assistant', 'config', 'routes')) {
      if (source.path.includes('.test.')) continue
      for (const literal of [...source.text.matchAll(/https:\/\/[^\s'"`)]+/g)].map((m) => m[0])) {
        const prefill = prefills.includes(literal) && source.path === PREFILL_MODULE
        expect(
          prefill || allowed.some((prefix) => literal.startsWith(prefix)),
          `${source.path} carries the literal ${literal}`
        ).toBe(true)
      }
    }
  })

  it('keeps every prefill inside that one table, and nothing else in it', () => {
    // The other half of the allowance: the module admitted above must carry
    // the three literals **as the table** and carry no fourth address of its
    // own. Read off the file rather than off the export, so a literal written
    // beside the table — a comment's example, a second map — fails here.
    const source = sourcesUnder('assistant').find(
      (each) => each.path === 'assistant/endpointDraft.ts'
    )
    expect(source, 'the prefill table is in its own module').toBeDefined()
    const literals = [...source!.text.matchAll(/https:\/\/[^\s'"`)]+/g)].map((m) => m[0])
    expect(literals.sort()).toEqual(Object.values(PREFILLED_URL).sort())
    // One per kind, and each a distinct address: a table with two kinds on one
    // base would be a picker that changes the protocol and not the endpoint.
    expect(Object.keys(PREFILLED_URL).sort()).toEqual([...ASSISTANT_KINDS].sort())
    expect(new Set(Object.values(PREFILLED_URL)).size).toBe(ASSISTANT_KINDS.length)
  })
})

describe('(5a) the listing suffixes are one table, on both sides', () => {
  it('is the same map the relay scans by', () => {
    // **The relay reads a listing's body and no other answer's**, because a
    // listing is the one relayed answer the desk *renders* — into a picker,
    // into state, into a field somebody can copy. Which requests are listings
    // is decided by this table on both sides: a suffix the page asks at and
    // the relay does not scan is an answer rendered unscanned, and one the
    // relay scans and the page never asks at is a scan of nothing.
    const source = readFileSync(
      join(SRC, '..', '..', 'internal', 'desk', 'modelrelay.go'),
      'utf8'
    )
    const block = /var relayListingSuffix = map\[string\]string\{([\s\S]*?)\n\}/.exec(source)
    expect(block, 'relayListingSuffix is declared in internal/desk/modelrelay.go').not.toBeNull()
    const declared = Object.fromEntries(
      [...block![1]!.matchAll(/"([^"]+)":\s*"([^"]*)"/g)].map((match) => [match[1]!, match[2]!])
    )
    expect(declared).toEqual({ ...LISTING_SUFFIX })
    // And it covers every kind either side admits, so a fourth protocol
    // cannot arrive with no listing rule at all.
    expect(Object.keys(declared).sort()).toEqual([...ASSISTANT_KINDS].sort())
  })
})

describe('(6) the probe answers from a closed vocabulary, shared with the desk', () => {
  it('declares the same words the desk declares', () => {
    // Two lists of one vocabulary drift, and the page's copy is what turns a
    // word into a sentence a reader sees. Read out of the Go declaration.
    const source = readFileSync(
      join(SRC, '..', '..', 'internal', 'desk', 'assistant.go'),
      'utf8'
    )
    const block = /var AssistantDiagnostics = \[\]string\{([\s\S]*?)\}/.exec(source)
    expect(block, 'AssistantDiagnostics is declared in internal/desk/assistant.go').not.toBeNull()
    const declared = [...block![1]!.matchAll(/Diagnostic([A-Za-z]+)/g)].map((match) => match[1]!)
    // The Go side names them by constant; the constants' values are asserted
    // against this list on that side, and the *count* is asserted here so a
    // word added there without one here fails.
    expect(declared.length).toBe(PROBE_DIAGNOSTICS.length)
    for (const word of PROBE_DIAGNOSTICS) {
      expect(source, `${word} is not a value in internal/desk/assistant.go`).toContain(`"${word}"`)
    }
  })

  it('never renders text the endpoint wrote', () => {
    // The page has no member to render it from. Asserted against the type's
    // own declaration, so restoring one would fail here rather than being
    // noticed by a reader.
    const client = read('assistant/client.ts')
    expect(membersOf(interfaceBody(client, 'ProbeResult'))).toEqual([
      'reachable',
      'status',
      'latencyMs',
      'diagnostic'
    ])
    expect(client, 'the probe result still carries a detail member').not.toMatch(/\bdetail\b\s*:/)
  })
})

describe('(7) one member, one refusal', () => {
  it('refuses a credential-shaped member once, with the sentence about keys', () => {
    // The credential sentence has **one producer** — the recursive pre-scan —
    // and the schema walk says only "unknown key". A member that is both was
    // reported twice for two reasons, which reads on Admin as two mistakes.
    const decoded = decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, assistant: { endpoint: { apiKey: 'sk-nope' } } }),
      'desk'
    )
    const about = decoded.problems.filter(
      (problem) => problem.key === 'assistant.endpoint.apiKey'
    )
    expect(about).toHaveLength(1)
    expect(about[0]!.reason).toBe(KEYS_ARE_NEVER_IN_CONFIGURATION)
  })

  it('still says "unknown key" for a member that is not credential-shaped', () => {
    const decoded = decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, colour: 'blue' }),
      'desk'
    )
    expect(decoded.problems.find((problem) => problem.key === 'colour')!.reason).toBe('unknown key')
  })
})

/**
 * (8) The engine slot's own boundary.
 *
 * ADR-0001 puts the desk's promises **below** whatever runs the loop, and the
 * sentence it holds them with is "the engine's `callTool` is bound through the
 * gate and the engine never holds the raw client". That is a claim about a
 * member set — there is nothing else on the session — so it is asserted whole,
 * both ways round, exactly as (1) asserts the endpoint's.
 */
const SESSION_KEYS = [
  'prompt',
  'testPrompt',
  'tools',
  'callTool',
  'model',
  'thinking',
  'signal'
] as const

const sessionKeysAreExact: Exactly<
  keyof import('./engine').AssistantSession,
  (typeof SESSION_KEYS)[number]
> = true

describe('(8) the engine is handed a bound callTool and nothing else', () => {
  it('declares the session member set as exactly those seven', () => {
    // A `client` here — or a `transport`, or a `fetch` — would put a door
    // beside the ToolGate rather than behind it, and every guarantee the gate
    // holds would become a guarantee about the door engines happened to use.
    expect(sessionKeysAreExact).toBe(true)
    expect(membersOf(interfaceBody(read('assistant/engine.ts'), 'AssistantSession'))).toEqual([
      ...SESSION_KEYS
    ])
  })

  it('declares callTool as a function type, which cannot grow a member', () => {
    const source = read('assistant/engine.ts')
    expect(source).toContain(
      'export type CallTool = (name: string, args: Record<string, unknown>) => Promise<McpToolResult>'
    )
  })

  it('hands the model a capability and never an address', () => {
    // The reason the contract deviates from ADR-0001's sketch, held as a
    // member set rather than as prose. The original reason was that a
    // `baseUrl` for this relay carried this chassis' session token; the session
    // is a bearer the page holds now, so no address is a credential — and the
    // deviation stands on what it always really bought, which is that the desk
    // decides *what* an engine may reach rather than handing it an address.
    // Nothing at **run time** stops an engine reaching for a global: the seal
    // on `WebSocket` lives in the conformance session, so an engine that opened
    // its own connection would fail this repository's suite rather than be
    // prevented by the running desk.
    const source = read('assistant/engine.ts')
    expect(membersOf(interfaceBody(source, 'ModelRequest'))).toEqual([
      'headers',
      'method',
      'body',
      'signal'
    ])
    expect(source).toContain(
      'export type ModelCall = (suffix: string, request: ModelRequest) => Promise<Response>'
    )
    expect(source).toContain('model: { family: EndpointKind; model: string; call: ModelCall }')
  })

  it('admits two methods and no more, because the relay forwards the method', () => {
    // **`method` is the one member this set has grown, and it is a closed
    // pair.** The relay carries the method verbatim, so an open member would
    // let whoever holds a capability ask the configured endpoint to *do*
    // something nobody wrote down with the machine-held credential attached —
    // the same argument that closes the path's colon methods. `GET` is here
    // because each protocol's model listing is one and the listing goes over
    // this capability rather than round it; the type is what holds the pair,
    // and this reads the declaration so that widening it fails here.
    const source = read('assistant/engine.ts')
    expect(source).toContain("method?: 'GET' | 'POST'")
  })

  // **The string-enumeration guard that used to stand here is gone.** It
  // forbade a handful of spellings — `@modelcontextprotocol/sdk/client`,
  // `DeskWebSocketTransport`, `sessionToken` — under `engines/`, and said of
  // itself that a novel spelling walks past it. `new globalThis["Web"+"Socket"]`
  // is that novel spelling. The conformance session seals `fetch`, `WebSocket`,
  // `XMLHttpRequest` and `EventSource` for the duration of every engine's run
  // instead, which is a guard over *behaviour* and needs no list.

  it('writes no tool schema of its own: the runtime’s served one is the only one', () => {
    // K2, over **every** engine source rather than one directory of one engine.
    // The sweep used to read `builtin/providers` and nothing else, so the whole
    // of `engines/vercel/` — where an adapter-authored permissive schema
    // actually was — went unlooked-at.
    //
    // The model is shown the contract the runtime enforces or it is shown
    // nothing, so no engine may carry a schema of its own. The one tool name any
    // engine may write is `experimental_evaluate`, because ADR-0001 names it:
    // the SDK-backed adapter's rehearsal hook is keyed by tool name. Every other
    // name is a capability the engine would be asserting.
    const sources = sourcesUnder('assistant/engines')
    expect(sources.length, 'the sweep found no engine sources at all').toBeGreaterThan(5)
    for (const source of sources) {
      if (source.path.includes('.test.')) continue
      for (const tool of ASSISTANT_TOOLS) {
        if (tool === 'experimental_evaluate') continue
        expect(source.text, `${source.path} names the tool ${tool}`).not.toContain(`'${tool}'`)
        expect(source.text, `${source.path} names the tool ${tool}`).not.toContain(`"${tool}"`)
      }
      // A schema is an object with `properties` or a `type: 'object'` beside a
      // tool. Weak, and enumerated — what actually holds the rule is that a
      // served schema is the only thing any engine passes on, and that a tool
      // arriving without one is refused rather than given one.
      expect(source.text, `${source.path} writes a schema of its own`).not.toMatch(
        /properties\s*:\s*\{/
      )
    }
  })
})

describe('the proposal is canonicalized in one place, and nowhere else', () => {
  /**
   * A string sweep, and a weak guard like the two above it: it cannot catch a
   * novel spelling of a JSON round trip. What it does catch is the ordinary
   * one, which is what a second canonicalization looks like when somebody adds
   * it back for safety — and that is the defect this holds against, because two
   * readings of one proposal are two documents: the diff can describe the first
   * and the writer write the second.
   */
  const others = () =>
    sourcesUnder('assistant').filter(
      (source) =>
        !source.path.includes('.test.') &&
        !source.path.includes('/conformance/') &&
        source.path !== 'assistant/useAssistantRun.ts'
    )

  it('round-trips and freezes in the run hook, and in no other module', () => {
    const hook = read('assistant/useAssistantRun.ts')
    expect(hook).toContain('JSON.stringify(value)')
    expect(hook).toContain('JSON.parse(text)')
    expect(hook).toContain('Object.freeze(value)')
    const swept = others()
    expect(swept.length).toBeGreaterThan(3)
    for (const source of swept) {
      expect(source.text, `${source.path} round-trips a proposal`).not.toContain(
        'JSON.parse(JSON.stringify'
      )
      expect(source.text, `${source.path} calls plain()`).not.toMatch(/[^A-Za-z]plain\(/)
      expect(source.text, `${source.path} freezes again`).not.toContain('Object.freeze(')
    }
  })

  it('says so in the two modules that read what it produced', () => {
    // A claim a reader has to hold when they change the diff or the writer, so
    // it is written where they are rather than only here.
    for (const path of ['assistant/proposalDiff.ts', 'assistant/acceptProposal.ts']) {
      expect(read(path), `${path} names the guard`).toContain('enforcement.test.ts')
    }
  })
})

describe('(9) the page sends no request header the chassis would drop', () => {
  it('holds MODEL_REQUEST_HEADERS inside the chassis outbound allow-list', () => {
    // **Two hand-mirrored allow-lists, and nothing held them together.** The
    // page filters a model request's headers to `MODEL_REQUEST_HEADERS` and
    // the chassis rebuilds the outbound set from `relayedRequestHeaders`; a
    // name on the page's list and not the chassis' is a header the engine
    // believes it sent and the endpoint never sees, which is exactly the kind
    // of failure a protocol change produces and no test would have named.
    //
    // **Containment and not equality**, because the two lists are not the same
    // list and should not be: the chassis additionally carries what a *browser*
    // sets on its own — `accept-encoding`, `content-length`, `user-agent` — and
    // the `X-Stainless-*` family, none of which a page-side engine writes. What
    // must hold is one direction: everything the page may send, the chassis
    // carries.
    const go = readFileSync(join(SRC, '..', '..', 'internal', 'desk', 'modelrelay.go'), 'utf8')
    const declared = /var relayedRequestHeaders = \[\]string\{([^}]*)\}/.exec(go)
    expect(declared, 'relayedRequestHeaders is declared in internal/desk/modelrelay.go').not.toBeNull()
    const carried = [...declared![1]!.matchAll(/"([^"]+)"/g)].map((match) =>
      match[1]!.toLowerCase()
    )
    expect(carried.length).toBeGreaterThan(5)

    const page = /const MODEL_REQUEST_HEADERS: readonly string\[\] = \[([^\]]*)\]/.exec(
      read('assistant/session.ts')
    )
    expect(page, 'MODEL_REQUEST_HEADERS is declared in assistant/session.ts').not.toBeNull()
    const sent = [...page![1]!.matchAll(/'([^']+)'/g)].map((match) => match[1]!.toLowerCase())
    expect(sent.length).toBeGreaterThan(5)

    for (const header of sent) {
      expect(carried, `the chassis drops ${header}, which the page may send`).toContain(header)
    }
  })
})

describe('(10) the page addresses nothing the chassis relay would refuse', () => {
  /** The Go source, read once for the three declarations below. */
  const chassis = () =>
    readFileSync(join(SRC, '..', '..', 'internal', 'desk', 'modelrelay.go'), 'utf8')
  const page = () => read('assistant/session.ts')

  it('holds the colon method list equal to the chassis’, name for name', () => {
    // **Two hand-mirrored closed lists, and containment is not enough here.**
    // A method on the page's list and not the chassis' is a call the engine
    // believes it made and the relay refused; one on the chassis' list and not
    // the page's is a capability the desk grants and the page cannot reach. The
    // part after a colon is a *verb*, so both directions matter: the lists are
    // the same list, written twice because one is Go and one is TypeScript.
    const declared = /var relayPathMethods = \[\]string\{([^}]*)\}/.exec(chassis())
    expect(declared, 'relayPathMethods is declared in internal/desk/modelrelay.go').not.toBeNull()
    const chassisMethods = [...declared![1]!.matchAll(/"([^"]+)"/g)].map((match) => match[1]!)
    expect(chassisMethods.length).toBeGreaterThan(0)

    const mirrored = /const RELAY_PATH_METHODS: readonly string\[\] = \[([^\]]*)\]/.exec(page())
    expect(mirrored, 'RELAY_PATH_METHODS is declared in assistant/session.ts').not.toBeNull()
    const pageMethods = [...mirrored![1]!.matchAll(/'([^']+)'/g)].map((match) => match[1]!)

    expect([...pageMethods].sort()).toEqual([...chassisMethods].sort())
  })

  it('holds the one admitted query pair, and the kinds that admit it, equal to the chassis’', () => {
    // The chassis says which literal and which kind in two places — the pair is
    // a constant and the table is a switch — and the page mirrors both. A pair
    // the page admitted on a kind the relay does not would be a request refused
    // after it left the page; the reverse would be a stream the desk cannot ask
    // for.
    const go = chassis()
    const pair = /relayStreamPair\s+= "([^"]+)"/.exec(go)
    expect(pair, 'relayStreamPair is declared in internal/desk/modelrelay.go').not.toBeNull()
    const mirroredPair = /const RELAY_STREAM_PAIR = '([^']+)'/.exec(page())
    expect(mirroredPair, 'RELAY_STREAM_PAIR is declared in assistant/session.ts').not.toBeNull()
    expect(mirroredPair![1]).toBe(pair![1])

    // `relayExtraQueryPair` is a switch over kinds; every kind it names returns
    // the pair, and every other kind returns nothing.
    const table = /func relayExtraQueryPair\(kind string\) string \{([\s\S]*?)\n\}/.exec(go)
    expect(table, 'relayExtraQueryPair is declared in internal/desk/modelrelay.go').not.toBeNull()
    const admitting = [...table![1]!.matchAll(/case "([^"]+)":/g)].map((match) => match[1]!)
    expect(admitting.length).toBeGreaterThan(0)

    for (const kind of ASSISTANT_KINDS) {
      const expected = admitting.includes(kind) ? '' : 'refused'
      const asked = suffixProblem(`v1beta/models/m:streamGenerateContent?${pair![1]!}`, kind)
      expect(
        asked === '' ? '' : 'refused',
        `the chassis ${admitting.includes(kind) ? 'admits' : 'refuses'} ${pair![1]!} on ${kind}`
      ).toBe(expected)
    }
  })

  it('refuses every method the chassis refuses, and admits the ones it admits', () => {
    // The rule is about the path and is not gated on the kind, exactly as the
    // chassis writes it: the kind decides the credential, and a mirror that read
    // one to decide the other would be two rules where there is one.
    const declared = /var relayPathMethods = \[\]string\{([^}]*)\}/.exec(chassis())
    const methods = [...declared![1]!.matchAll(/"([^"]+)"/g)].map((match) => match[1]!)
    for (const kind of ASSISTANT_KINDS) {
      for (const method of methods) {
        expect(suffixProblem(`v1beta/models/m:${method}`, kind), `${kind} ${method}`).toBe('')
      }
      expect(suffixProblem('v1beta/models/m:deleteModel', kind)).not.toBe('')
    }
  })
})
