/**
 * The slug, the collision, and the two ways a first document is arrived at.
 *
 * The schema fixture is the shape of the runtime's own JPS schema
 * (`internal/artifacts/jps/0.2.0-draft/schema.json` at v0.19.0): a top-level
 * `required` list, a `const` on `specVersion`, `minItems` on `outcomes` and
 * `rules`, and members stated through local `$ref`s.
 */
import { describe, expect, it } from 'vitest'
import {
  collisionIn,
  emptyPackFrom,
  packPathFor,
  shapeTemplate,
  slugFor
} from './newPack'

const SCHEMA = JSON.stringify({
  type: 'object',
  required: ['specVersion', 'id', 'version', 'title', 'decision', 'outcomes', 'rules'],
  properties: {
    specVersion: { const: '0.2.0-draft' },
    id: { type: 'string', format: 'uri', minLength: 1 },
    version: { type: 'string', pattern: '^\\d+\\.\\d+\\.\\d+$' },
    title: { $ref: '#/$defs/nonEmptyString' },
    description: { $ref: '#/$defs/nonEmptyString' },
    decision: { $ref: '#/$defs/decision' },
    outcomes: { type: 'array', minItems: 2 },
    rules: { type: 'array', minItems: 1 }
  },
  $defs: {
    nonEmptyString: { type: 'string', minLength: 1 },
    decision: { type: 'object', required: ['intent', 'question'] }
  }
})

describe('slugFor', () => {
  it.each([
    ['Vendor Onboarding', 'vendor-onboarding'],
    ['vendor onboarding', 'vendor-onboarding'],
    ['VENDOR  ONBOARDING', 'vendor-onboarding'],
    ['Expense approval (EU)', 'expense-approval-eu'],
    ['  Refund / return  ', 'refund-return'],
    ['a—b', 'a-b'],
    ['Tier 2 review', 'tier-2-review'],
    ['already-a-slug', 'already-a-slug']
  ])('derives %s → %s', (name, slug) => {
    expect(slugFor(name)).toEqual({ slug })
  })

  it('refuses a name with no letter or number in it', () => {
    for (const name of ['', '   ', '!!!', '---']) {
      expect(slugFor(name)).toEqual({ problem: 'A name needs at least one letter or number.' })
    }
  })

  it('refuses a name whose slug would not start with a letter', () => {
    // A `packs` key is a `decisionId` and must begin with `[a-z]`. Prefixing a
    // letter would be the desk inventing half the id.
    for (const name of ['2024 review', '9 lives']) {
      expect(slugFor(name)).toEqual({ problem: 'A name must start with a letter.' })
    }
  })
})

describe('packPathFor', () => {
  it('puts the slug in the configured directory under a .pack.json suffix', () => {
    expect(packPathFor('packs', 'vendor-onboarding')).toBe('packs/vendor-onboarding.pack.json')
    expect(packPathFor('a/b', 'x')).toBe('a/b/x.pack.json')
  })

  it('cannot land inside the runtime’s optional filename convention', () => {
    // `<decision-id>-<semver>.pack.json` needs dots in the version group, and
    // a slug can carry none — so the convention's check is reported skipped
    // and never failed, and never matches by accident either.
    const convention = /^([a-z][a-z0-9]*(?:-[a-z0-9]+)*)-((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))\.pack\.json$/
    for (const name of ['Vendor onboarding', 'Pack 0 1 0', 'a 1 2 3']) {
      const derived = slugFor(name)
      expect('slug' in derived).toBe(true)
      const file = packPathFor('packs', (derived as { slug: string }).slug).split('/').pop()!
      expect(convention.test(file), file).toBe(false)
    }
  })
})

describe('collisionIn', () => {
  const project = {
    keys: ['sanctions-screening'],
    files: ['jpack.json', 'packs/vendor-onboarding.pack.json'],
    path: 'packs/vendor-onboarding.pack.json'
  }

  it('refuses a slug the project already uses as a pack key', () => {
    expect(
      collisionIn('sanctions-screening', { ...project, path: 'packs/sanctions-screening.pack.json' })
    ).toBe('This project already has a pack called sanctions-screening.')
  })

  it('refuses a slug whose file is already there', () => {
    expect(collisionIn('vendor-onboarding', project)).toBe(
      'There is already a file where this pack would be written.'
    )
  })

  it('allows a slug that collides with neither', () => {
    expect(
      collisionIn('expense-approval', { ...project, path: 'packs/expense-approval.pack.json' })
    ).toBeUndefined()
  })
})

describe('shapeTemplate', () => {
  const TEMPLATE = JSON.stringify({
    specVersion: '0.2.0-draft',
    id: 'https://runtime.example/examples/minimal',
    version: '3.4.5',
    title: 'Minimal expense approval',
    description: 'The example’s own line.',
    decision: { intent: 'decide', question: 'Approve?' },
    outcomes: [{ id: 'approve' }, { id: 'decline' }],
    rules: [{ id: 'r1' }],
    metadata: { author: 'the runtime' }
  })

  const shaped = (description: string) =>
    JSON.parse(
      shapeTemplate(TEMPLATE, {
        name: 'Vendor Onboarding',
        description,
        slug: 'vendor-onboarding',
        idBase: 'https://example.invalid/judgment-packs/'
      })
    ) as Record<string, unknown>

  it('sets the four members the dialog asked about', () => {
    const document = shaped('Whether a vendor may be onboarded.')
    expect(document.title).toBe('Vendor Onboarding')
    expect(document.description).toBe('Whether a vendor may be onboarded.')
    expect(document.id).toBe('https://example.invalid/judgment-packs/vendor-onboarding')
    expect(document.version).toBe('0.1.0')
  })

  it('leaves specVersion and every other member exactly as the template carried them', () => {
    // `specVersion` is the runtime's statement about which version of the
    // format this document is written to. It is never the desk's.
    const document = shaped('One line.')
    expect(document.specVersion).toBe('0.2.0-draft')
    expect(document.decision).toEqual({ intent: 'decide', question: 'Approve?' })
    expect(document.outcomes).toEqual([{ id: 'approve' }, { id: 'decline' }])
    expect(document.rules).toEqual([{ id: 'r1' }])
    expect(document.metadata).toEqual({ author: 'the runtime' })
  })

  it('drops the template’s own description when the dialog was given none', () => {
    // Keeping it would put the example's sentence under the new pack's name.
    const document = shaped('   ')
    expect('description' in document).toBe(false)
  })

  it('keeps the template’s member order and appends nothing it did not have', () => {
    expect(Object.keys(shaped('One line.'))).toEqual([
      'specVersion',
      'id',
      'version',
      'title',
      'description',
      'decision',
      'outcomes',
      'rules',
      'metadata'
    ])
  })

  it('writes two-space JSON with a trailing newline', () => {
    const text = shapeTemplate(TEMPLATE, {
      name: 'A',
      description: '',
      slug: 'a',
      idBase: 'https://example.invalid/p/'
    })
    expect(text.endsWith('}\n')).toBe(true)
    expect(text).toContain('\n  "specVersion"')
  })

  it('joins idBase and slug with exactly what the decoder normalised', () => {
    const fragment = JSON.parse(
      shapeTemplate(TEMPLATE, {
        name: 'A',
        description: '',
        slug: 'a',
        idBase: 'https://example.invalid/p#'
      })
    ) as { id: string }
    expect(fragment.id).toBe('https://example.invalid/p#a')
  })

  it('says so when the template is not a JSON object', () => {
    expect(() =>
      shapeTemplate('[]', { name: 'A', description: '', slug: 'a', idBase: 'https://e.invalid/' })
    ).toThrow(/not a JSON object/)
    expect(() =>
      shapeTemplate('{oops', { name: 'A', description: '', slug: 'a', idBase: 'https://e.invalid/' })
    ).toThrow(/not valid JSON/)
  })
})

describe('emptyPackFrom', () => {
  const skeleton = () => JSON.parse(emptyPackFrom(SCHEMA)) as Record<string, unknown>

  it('emits only the members the schema itself lists as required', () => {
    expect(Object.keys(skeleton())).toEqual([
      'specVersion',
      'id',
      'version',
      'title',
      'decision',
      'outcomes',
      'rules'
    ])
    // `description` is a property of the schema and not a required one, so it
    // is not invented here — the dialog adds it when it has one.
    expect('description' in skeleton()).toBe(false)
  })

  it('takes a const verbatim and empties everything else by its declared type', () => {
    expect(skeleton()).toEqual({
      specVersion: '0.2.0-draft',
      id: '',
      version: '',
      title: '',
      decision: {},
      outcomes: [],
      rules: []
    })
  })

  it('invents no outcome and no rule to satisfy minItems', () => {
    // An invented outcome id would be the desk asserting what the decision is.
    // The runtime is the thing entitled to say this document is incomplete.
    const document = skeleton()
    expect(document.outcomes).toEqual([])
    expect(document.rules).toEqual([])
  })

  it('is an empty object where no schema was advertised', () => {
    // Nothing is invented from nothing. The dialog fills its four members on
    // top of this, which is what a runtime advertising neither tool leaves.
    expect(JSON.parse(emptyPackFrom(undefined))).toEqual({})
    expect(JSON.parse(emptyPackFrom('not json'))).toEqual({})
  })

  it('is shaped by the dialog exactly as a served template is', () => {
    const document = JSON.parse(
      shapeTemplate(emptyPackFrom(SCHEMA), {
        name: 'Vendor Onboarding',
        description: '',
        slug: 'vendor-onboarding',
        idBase: 'https://example.invalid/judgment-packs/'
      })
    ) as Record<string, unknown>
    expect(document.specVersion).toBe('0.2.0-draft')
    expect(document.title).toBe('Vendor Onboarding')
    expect(document.id).toBe('https://example.invalid/judgment-packs/vendor-onboarding')
    expect(document.version).toBe('0.1.0')
  })
})
