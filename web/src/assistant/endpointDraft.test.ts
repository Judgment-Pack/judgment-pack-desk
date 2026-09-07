/**
 * The arithmetic between what an author typed and what is written.
 *
 * **The load-bearing case is the first one**, and it is a whole-set assertion
 * rather than a search for a forbidden name: the object this form sends
 * carries exactly the six members the schema declares, whatever the draft it
 * was built from happens to be carrying. A blacklist could not state that —
 * the thing it excludes is *any* member nobody declared, and that set has no
 * enumeration.
 */
import { describe, expect, it } from 'vitest'
import {
  ASSISTANT_TOOLS,
  DESK_DEFAULTS,
  decodeDeskConfig,
  type AssistantConfig
} from '../config/deskConfig'
import {
  KIND_OPTIONS,
  PREFILLED_URL,
  TIER_OPTIONS,
  assistantWrite,
  draftFrom,
  seedOf,
  tierProvenance,
  tierSays,
  withKind,
  withTool,
  type EndpointDraft
} from './endpointDraft'

const DRAFT: EndpointDraft = {
  kind: 'gemini',
  url: 'https://api.example.invalid/',
  model: 'a-model',
  tools: ['validate', 'get_schema'],
  engine: 'builtin',
  thinking: 'ultra'
}

describe('the object a save sends', () => {
  it('carries exactly the members the schema declares, and no others', () => {
    const written = assistantWrite(DRAFT) as Record<string, unknown>
    expect(Object.keys(written).sort()).toEqual(['endpoint', 'engine', 'thinking'])
    expect(Object.keys(written.endpoint as object).sort()).toEqual([
      'kind',
      'model',
      'tools',
      'url'
    ])
  })

  it('carries no member a draft picked up, credential-shaped or otherwise', () => {
    // **A draft is page state**: it is merged, re-seeded and handed through
    // setters, and a spread would write whatever it was carrying into the one
    // file on this machine that names where a credential is presented. The
    // cast is how a mis-merge or a field added without thinking would look.
    const carrying = {
      ...DRAFT,
      apiKey: 'sk-a-real-looking-key-wxyz',
      note: 'something a future edit added'
    } as unknown as EndpointDraft
    const written = assistantWrite(carrying)
    expect(Object.keys(written as object).sort()).toEqual(['endpoint', 'engine', 'thinking'])
    expect(JSON.stringify(written)).not.toContain('apiKey')
    expect(JSON.stringify(written)).not.toContain('sk-a-real-looking-key-wxyz')
    expect(JSON.stringify(written)).not.toContain('something a future edit added')
  })

  it('writes the tool list off the closed list, in the closed list s order', () => {
    // Clicked in one order, written in the schema's — so a file rewritten
    // with no change to the choice is the file it already was.
    const written = assistantWrite({ ...DRAFT, tools: ['validate', 'get_schema'] }) as {
      endpoint: { tools: string[] }
    }
    expect(written.endpoint.tools).toEqual(['get_schema', 'validate'])
  })

  it('writes an empty tool list as one, because that is a real choice', () => {
    const written = assistantWrite({ ...DRAFT, tools: [] }) as { endpoint: { tools: string[] } }
    expect(written.endpoint.tools).toEqual([])
  })

  it('trims the two fields somebody types, and nothing else', () => {
    const written = assistantWrite({
      ...DRAFT,
      url: '  https://api.example.invalid/  ',
      model: '  a-model  '
    }) as { endpoint: { url: string; model: string } }
    expect(written.endpoint.url).toBe('https://api.example.invalid/')
    expect(written.endpoint.model).toBe('a-model')
  })

  it('writes something the shared decoder accepts, member for member', () => {
    // The two decoders are one contract, so what this composes is put through
    // the browser's half rather than only inspected. A composition that
    // drifted from the schema would fail here before a chassis ever saw it.
    const decoded = decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, assistant: assistantWrite(DRAFT) }),
      'desk'
    )
    expect(decoded.problems).toEqual([])
    expect(decoded.values?.assistant).toEqual({
      endpoint: {
        url: 'https://api.example.invalid/',
        kind: 'gemini',
        model: 'a-model',
        tools: ['get_schema', 'validate']
      },
      engine: 'builtin',
      thinking: 'ultra'
    })
  })
})

describe('the draft a configuration opens on', () => {
  it('grants every tool where nothing is configured', () => {
    // `[]` means an assistant that may call nothing, which is a decision. A
    // form that opened on it would have a blank field making it.
    expect(draftFrom(DESK_DEFAULTS.assistant).tools).toEqual([...ASSISTANT_TOOLS])
    expect(draftFrom(DESK_DEFAULTS.assistant).url).toBe('')
  })

  it('takes every field from the file where one is configured', () => {
    const config: AssistantConfig = {
      endpoint: {
        url: 'https://api.example.invalid/v1',
        kind: 'anthropic',
        model: 'a-model',
        tools: ['validate']
      },
      engine: 'builtin',
      thinking: 'on'
    }
    expect(draftFrom(config)).toEqual({
      kind: 'anthropic',
      url: 'https://api.example.invalid/v1',
      model: 'a-model',
      tools: ['validate'],
      engine: 'builtin',
      thinking: 'on'
    })
  })

  it('changes its seed exactly when the configuration it would open on changes', () => {
    const first = seedOf(DESK_DEFAULTS.assistant)
    expect(seedOf(DESK_DEFAULTS.assistant)).toBe(first)
    expect(seedOf({ ...DESK_DEFAULTS.assistant, thinking: 'ultra' })).not.toBe(first)
  })
})

describe('choosing a wire protocol', () => {
  it('offers the address that protocol s own reference documents', () => {
    const opened = draftFrom(DESK_DEFAULTS.assistant)
    expect(withKind(opened, 'gemini').url).toBe(PREFILLED_URL.gemini)
    expect(withKind(opened, 'anthropic').url).toBe(PREFILLED_URL.anthropic)
  })

  it('replaces an offer it made itself, and nothing anybody typed', () => {
    const offered = withKind(draftFrom(DESK_DEFAULTS.assistant), 'gemini')
    expect(withKind(offered, 'anthropic').url).toBe(PREFILLED_URL.anthropic)
    const typed = { ...offered, url: 'https://gateway.example.invalid/v1beta' }
    expect(withKind(typed, 'anthropic').url).toBe('https://gateway.example.invalid/v1beta')
    // And the protocol still changes: it is the URL that is left alone.
    expect(withKind(typed, 'anthropic').kind).toBe('anthropic')
  })

  it('offers one address per protocol and never two protocols one address', () => {
    expect(new Set(Object.values(PREFILLED_URL)).size).toBe(KIND_OPTIONS.length)
  })
})

describe('granting one tool', () => {
  it('adds and removes without disturbing the closed list s order', () => {
    const none = { ...DRAFT, tools: [] as EndpointDraft['tools'] }
    const one = withTool(none, 'validate', true)
    expect(one.tools).toEqual(['validate'])
    const two = withTool(one, 'get_schema', true)
    expect(two.tools).toEqual(['get_schema', 'validate'])
    expect(withTool(two, 'validate', false).tools).toEqual(['get_schema'])
  })
})

describe('what the tier picker says', () => {
  it('reads what goes on the wire off the desk s own table', () => {
    // Derived rather than restated: a sentence typed out beside the picker
    // would be a second copy of `thinking.ts` that nothing keeps in step.
    expect(tierSays('gemini', 'on')).toContain('thinkingBudget')
    expect(tierSays('gemini', 'off')).toContain('"thinkingBudget":0')
    expect(tierSays('openai-compatible', 'on')).toContain('reasoning_effort')
    expect(tierSays('anthropic', 'ultra')).toContain('xhigh')
  })

  it('says nothing goes on the wire where off is omission', () => {
    expect(tierSays('openai-compatible', 'off')).toBe('nothing at all goes on the wire')
    expect(tierSays('anthropic', 'off')).toBe('nothing at all goes on the wire')
  })

  it('accounts for the two numbers on the one family that has them', () => {
    expect(tierProvenance('gemini')).toContain('this desk’s choice')
    expect(tierProvenance('openai-compatible')).toBeUndefined()
    expect(tierProvenance('anthropic')).toBeUndefined()
  })

  it('offers the tiers the file admits and nothing else', () => {
    // A picker offering a fourth value would be offering a configuration the
    // decoder refuses by name — and the two states it cannot express are the
    // desk's to report, never a person's to select.
    expect(TIER_OPTIONS.map((option) => option.value)).toEqual(['off', 'on', 'ultra'])
  })
})
