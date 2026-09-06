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
  MAX_SLUG_LENGTH,
  collisionIn,
  emptyPackFrom,
  packPathFor,
  packFromProposal,
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

  it('folds diacritics rather than dropping the letters that carry them', () => {
    // `Überprüfung` used to derive `berpr-fung` — an id with its first letter
    // silently removed, shown live under the field as if it were the answer.
    expect(slugFor('Überprüfung')).toEqual({ slug: 'uberprufung' })
    expect(slugFor('Café Décision')).toEqual({ slug: 'cafe-decision' })
  })

  it('states the alphabet rather than claiming a name has no letters', () => {
    // `決裁レビュー` is made of letters. Telling its author it "needs at least
    // one letter or number" is a false statement about the name they typed;
    // the id alphabet is the thing that cannot carry it, and that is what the
    // sentence now says.
    const problem = {
      problem: 'A name needs at least one letter a–z or digit 0–9 — an id can carry no others.'
    }
    for (const name of ['', '   ', '!!!', '---', '決裁レビュー', 'Проверка', '日本語の決定']) {
      expect(slugFor(name)).toEqual(problem)
    }
  })

  it('refuses a name too long for the id it would make', () => {
    // Not a rule of the format: a pack is written as `<slug>.pack.json`, and a
    // single name component tops out at 255 bytes. Refused in the field, where
    // it can be acted on, rather than as a containment error from the write.
    expect(slugFor('a'.repeat(MAX_SLUG_LENGTH))).toEqual({ slug: 'a'.repeat(MAX_SLUG_LENGTH) })
    expect(slugFor('a'.repeat(MAX_SLUG_LENGTH + 1))).toEqual({
      problem: `A name is too long: an id can be at most ${MAX_SLUG_LENGTH} characters.`
    })
  })

  it('refuses a name whose slug would not start with a letter', () => {
    // A `packs` key is a `decisionId` and must begin with `[a-z]`. Prefixing a
    // letter would be the desk inventing half the id.
    for (const name of ['2024 review', '9 lives']) {
      expect(slugFor(name)).toEqual({ problem: 'A name must start with a letter.' })
    }
  })
})

describe('slugFor and the letters NFKD leaves whole', () => {
  // NFKD plus a diacritic sweep handles a letter that decomposes. It does
  // nothing for a letter whose mark is part of the glyph, and those were being
  // deleted by the `[^a-z0-9]` pass — `Łódź` came out `odz`, which is not a
  // transposition of anybody's name but a different word.
  it('transliterates the standard cases rather than dropping them', () => {
    expect(slugFor('Łódź')).toEqual({ slug: 'lodz' })
    expect(slugFor('Smørrebrød')).toEqual({ slug: 'smorrebrod' })
    expect(slugFor('Straße')).toEqual({ slug: 'strasse' })
    expect(slugFor('Æther œuvre')).toEqual({ slug: 'aether-oeuvre' })
    expect(slugFor('Þorsteinn Ðjúpur')).toEqual({ slug: 'thorsteinn-djupur' })
    expect(slugFor('Đakovo')).toEqual({ slug: 'dakovo' })
  })

  it('still folds the letters that do decompose', () => {
    expect(slugFor('Überprüfung')).toEqual({ slug: 'uberprufung' })
    expect(slugFor('Évaluation')).toEqual({ slug: 'evaluation' })
  })

  it('folds the Latin digraphs NFKD does take apart', () => {
    // `Ǆ` and `Ǉ` are single code points that decompose into two letters, so
    // they need no table — and asserting them here says which half of this
    // rule is NFKD's and which is the desk's.
    expect(slugFor('Ǆungla')).toEqual({ slug: 'dzungla' })
    expect(slugFor('Ǉubljana')).toEqual({ slug: 'ljubljana' })
  })

  it('names a Latin letter it cannot carry instead of deleting it', () => {
    // The alphabet sentence is true of another script and false of this: `ə`
    // is a Latin letter, it has no decomposition and no standard single
    // romanisation, and deleting it silently is the defect. So it is refused
    // by name, and whoever typed it can decide what they meant.
    const refused = slugFor('Azərbaycan')
    expect('problem' in refused).toBe(true)
    if ('problem' in refused) {
      expect(refused.problem).toContain('cannot carry')
      expect(refused.problem).toContain('ə')
      expect(refused.problem).toContain('will not drop a letter')
    }
  })

  it('keeps saying the alphabet for a name in another script', () => {
    const refused = slugFor('決裁レビュー')
    expect('problem' in refused).toBe(true)
    if ('problem' in refused) {
      expect(refused.problem).toContain('at least one letter')
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
    paths: ['sanctions-screening-0.1.0.pack.json'],
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

  it('refuses a slug whose file another entry already claims, with nothing on disk', () => {
    // The window this closes: an entry naming a file that has not been written
    // yet is an ordinary state for a project under construction, and a second
    // pack created over it would leave the first registered nowhere.
    expect(
      collisionIn('expense-approval', {
        ...project,
        paths: ['packs/expense-approval.pack.json'],
        files: ['jpack.json'],
        path: 'packs/expense-approval.pack.json'
      })
    ).toBe('Another pack in this project already uses that file.')
  })

  it('allows a slug that collides with none of the three', () => {
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

describe('packFromProposal', () => {
  /**
   * A proposal as the run hook hands one over: plain JSON data, frozen all the
   * way down. Shaped from *this* object, because the snapshot is what was on
   * screen and a second reading of anything else is a second document.
   */
  const PROPOSED = Object.freeze({
    specVersion: '0.2.0-draft',
    id: 'https://example.com/judgment-packs/expense-reimbursement',
    version: '0.1.0',
    title: 'Expense reimbursement',
    description: 'The assistant’s own line.',
    decision: Object.freeze({ intent: 'decide', question: 'Approve?' }),
    outcomes: Object.freeze([{ id: 'approve' }, { id: 'decline' }]),
    rules: Object.freeze([{ id: 'r1' }])
  })

  const shaped = (description: string) =>
    JSON.parse(
      packFromProposal(PROPOSED, {
        name: 'Vendor Onboarding',
        description,
        slug: 'vendor-onboarding',
        idBase: 'https://example.invalid/judgment-packs/'
      })
    ) as Record<string, unknown>

  it('writes the name that was typed, not the one the proposal gave itself', () => {
    // The one rule that is not shared with a template for a reason: a template
    // is the runtime's own document and a proposal is a model's, and the
    // person who typed the name is the author of neither.
    const document = shaped('Whether a vendor may be onboarded.')
    expect(document.title).toBe('Vendor Onboarding')
    expect(document.id).toBe('https://example.invalid/judgment-packs/vendor-onboarding')
    expect(document.version).toBe('0.1.0')
    expect(document.description).toBe('Whether a vendor may be onboarded.')
  })

  it('leaves the format version the proposal wrote, for the runtime to judge', () => {
    // **Not one of the four.** The desk owns exactly what it owns for a
    // template, and rewriting the one member that says what the document *is*
    // would mean the bytes the runtime checked were not the bytes the assistant
    // proposed. A version that is wrong is the runtime's to refuse.
    const claimed = Object.freeze({ ...PROPOSED, specVersion: '99' })
    const document = JSON.parse(
      packFromProposal(claimed, {
        name: 'A',
        description: '',
        slug: 'a',
        idBase: 'https://example.invalid/p/'
      })
    ) as Record<string, unknown>
    expect(document.specVersion).toBe('99')
  })

  it('leaves every other member exactly as the proposal carried it', () => {
    // **Left, and checked — not stripped.** An unknown top-level member is a
    // document the runtime's schema refuses, and the dialog refuses it before
    // either write with the runtime's own diagnostics. Removing it here would
    // be the desk editing a document on a model's behalf and telling nobody.
    const document = shaped('One line.')
    expect(document.specVersion).toBe('0.2.0-draft')
    expect(document.decision).toEqual({ intent: 'decide', question: 'Approve?' })
    expect(document.outcomes).toEqual([{ id: 'approve' }, { id: 'decline' }])
    expect(document.rules).toEqual([{ id: 'r1' }])
    const extra = JSON.parse(
      packFromProposal(Object.freeze({ ...PROPOSED, fileName: 'model-choice.pack.json' }), {
        name: 'A',
        description: '',
        slug: 'a',
        idBase: 'https://example.invalid/p/'
      })
    ) as Record<string, unknown>
    expect(extra.fileName).toBe('model-choice.pack.json')
  })

  it('drops the proposal’s own description where the dialog was given none', () => {
    expect('description' in shaped('  ')).toBe(false)
  })

  it('writes the same bytes a template is written as', () => {
    const text = packFromProposal(PROPOSED, {
      name: 'A',
      description: '',
      slug: 'a',
      idBase: 'https://example.invalid/p/'
    })
    expect(text.endsWith('}\n')).toBe(true)
    expect(text).toContain('\n  "specVersion"')
  })

  it('does not move the snapshot it was given', () => {
    // It is frozen, so a mutation would throw in strict mode rather than
    // silently succeed — but the claim is about what comes out: plain data
    // built from a spread, and the snapshot still saying what it said.
    shaped('One line.')
    expect(PROPOSED.title).toBe('Expense reimbursement')
    expect(PROPOSED.id).toBe('https://example.com/judgment-packs/expense-reimbursement')
  })

  it('says so when the proposal is not a JSON object', () => {
    const fields = { name: 'A', description: '', slug: 'a', idBase: 'https://e.invalid/' }
    for (const value of [null, [], 7, 'a pack']) {
      expect(() => packFromProposal(value, fields)).toThrow(/not a JSON object/)
    }
  })
})

describe('emptyPackFrom', () => {
  const skeleton = () => JSON.parse(emptyPackFrom(SCHEMA)!) as Record<string, unknown>

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

  it('is nothing at all where there is no schema to derive it from', () => {
    // Not an empty object. `{}` shaped by the dialog is a file with a title, an
    // id and a version and no `specVersion` — which is not an incomplete pack
    // but a document nothing can read as one. It was written, reported as a
    // success, and the runtime then called it invalid.
    expect(emptyPackFrom(undefined)).toBeUndefined()
    expect(emptyPackFrom('not json')).toBeUndefined()
  })

  it('is nothing where the schema yields no specVersion', () => {
    // The one member that says which version of the format the rest is written
    // to. A skeleton without it is refused here rather than written and
    // discovered later.
    const withoutSpecVersion = JSON.stringify({
      required: ['id', 'title'],
      properties: { id: { type: 'string' }, title: { type: 'string' } }
    })
    expect(emptyPackFrom(withoutSpecVersion)).toBeUndefined()
  })

  it('is shaped by the dialog exactly as a served template is', () => {
    const document = JSON.parse(
      shapeTemplate(emptyPackFrom(SCHEMA)!, {
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
