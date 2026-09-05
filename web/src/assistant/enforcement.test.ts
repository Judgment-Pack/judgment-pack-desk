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
  ASSISTANT_TOOLS,
  KEYS_ARE_NEVER_IN_CONFIGURATION,
  decodeDeskConfig,
  type AssistantConfig,
  type AssistantEndpointConfig
} from '../config/deskConfig'
import type { AssistantSlot } from './useAssistantSlot'

const SRC = join(import.meta.dirname, '..')

function read(relative: string): string {
  return readFileSync(join(SRC, relative), 'utf8')
}

function sourcesUnder(...directories: string[]): { path: string; text: string }[] {
  const found: { path: string; text: string }[] = []
  for (const directory of directories) {
    for (const entry of readdirSync(join(SRC, directory), { withFileTypes: true })) {
      if (!entry.isFile()) continue
      if (!entry.name.endsWith('.ts') && !entry.name.endsWith('.tsx')) continue
      found.push({ path: `${directory}/${entry.name}`, text: read(`${directory}/${entry.name}`) })
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

/** The serialized assistant slot: one nullable field, and no second one. */
const ASSISTANT_KEYS = ['endpoint'] as const

/** The endpoint object's members, exactly as the schema declares them. */
const ENDPOINT_KEYS = ['url', 'kind', 'model', 'tools'] as const

/** What the future assistant pane reads. */
const SLOT_KEYS = ['state', 'endpoint', 'keyPresent'] as const

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

describe('(1) the assistant slot is one nullable field with exactly four members', () => {
  it('declares the assistant key set as exactly endpoint, and no other member', () => {
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
  it('accepts the four and refuses anything else by name', () => {
    expect([...ASSISTANT_TOOLS]).toEqual([
      'get_schema',
      'get_example',
      'validate',
      'experimental_evaluate'
    ])
    const good = decodeDesk({ endpoint: { ...GOOD_ENDPOINT, tools: [...ASSISTANT_TOOLS] } })
    expect(good.problems).toEqual([])
    expect(good.values?.assistant?.endpoint?.tools).toEqual([...ASSISTANT_TOOLS])

    for (const tool of ['write_file', 'get_pack', 'evaluate', 'bash', 'GET_SCHEMA']) {
      const decoded = decodeDesk({ endpoint: { ...GOOD_ENDPOINT, tools: [tool] } })
      expect(decoded.values, `${tool} refuses the whole file`).toBeUndefined()
      const problem = decoded.problems.find((each) => each.key === 'assistant.endpoint.tools')
      expect(problem, `${tool} is refused`).toBeDefined()
      // Named one at a time: "one of these is not allowed" makes a reader
      // check four names against a list.
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
      'https://example.invalid/judgment-packs/'
    ]
    for (const source of sourcesUnder('assistant', 'config', 'routes')) {
      if (source.path.includes('.test.')) continue
      for (const literal of [...source.text.matchAll(/https:\/\/[^\s'"`)]+/g)].map((m) => m[0])) {
        expect(
          allowed.some((prefix) => literal.startsWith(prefix)),
          `${source.path} carries the literal ${literal}`
        ).toBe(true)
      }
    }
  })
})
