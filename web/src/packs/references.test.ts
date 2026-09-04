import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import type { PackDocument } from '../mcp/types'
import { referencesFor } from './references'

const full = JSON.parse(
  readFileSync(join(import.meta.dirname, '__fixtures__', 'full.pack.json'), 'utf8')
) as PackDocument

describe('a rule’s references', () => {
  it('reports the outcome, the evidence, the sources and what cites it', () => {
    const lines = referencesFor(full, '/rules/1')
    expect(lines).toContainEqual({ relation: 'outcome', id: 'approve', target: '/outcomes/0' })
    expect(lines).toContainEqual({
      relation: 'evidence',
      id: 'screening-report',
      target: '/evidenceRequirements/0'
    })
    expect(lines).toContainEqual({
      relation: 'sources',
      id: 'insurance-rule',
      target: '/sources/1'
    })
    // The exception whose targetRule names this rule, from the other side.
    expect(lines).toContainEqual({
      relation: 'cited by',
      id: 'vendor-waiver',
      target: '/exceptions/0'
    })
  })
})

describe('an outcome’s references', () => {
  it('reports what produces it, and whether the document falls back to it', () => {
    const decline = referencesFor(full, '/outcomes/1')
    expect(decline).toContainEqual({
      relation: 'produced by rule',
      id: 'screen-first',
      target: '/rules/0'
    })
    expect(decline).toContainEqual({
      relation: 'fallback outcome',
      id: 'decline',
      target: '/fallbackOutcome'
    })
    const approve = referencesFor(full, '/outcomes/0')
    expect(approve.some((line) => line.relation === 'fallback outcome')).toBe(false)
  })
})

describe('an evidence requirement’s references', () => {
  it('reports the rules that reference it and the condition nodes that name it', () => {
    const lines = referencesFor(full, '/evidenceRequirements/0')
    expect(lines).toContainEqual({
      relation: 'required by rule',
      id: 'approve-when-clear',
      target: '/rules/1'
    })
    // The `evidence-present` node inside rule 0's own condition tree — a fact
    // only the tree holds, reported here at the node's own pointer.
    expect(lines).toContainEqual({
      relation: 'tested by condition',
      id: 'screening-report',
      target: '/rules/0/when'
    })
  })
})

describe('the escalation', () => {
  it('resolves nothing from its triggers, which name no id', () => {
    // `triggers` is a closed enum of five reason words. Reading one as an
    // evidence-requirement id printed "no declared evidence requirement
    // carries this id" on every conformant pack — a dangling-reference claim
    // about a document that made none.
    const lines = referencesFor(full, '/escalation')
    expect(lines.some((line) => line.unresolved !== undefined)).toBe(false)
    for (const trigger of full.escalation!.triggers!) {
      expect(lines.some((line) => line.id === trigger)).toBe(false)
    }
  })

  it('is not reported back from an evidence requirement either', () => {
    // The same wrong model, run backwards: `unknown`, `conflict` and
    // `no-match` are all legal `localId` spellings, so a requirement carrying
    // one used to grow a fabricated "escalation trigger" line.
    const named: PackDocument = {
      ...full,
      evidenceRequirements: [
        { ...full.evidenceRequirements![0]!, id: 'unknown' }
      ]
    }
    const lines = referencesFor(named, '/evidenceRequirements/0')
    expect(lines.some((line) => line.relation === 'escalation trigger')).toBe(false)
  })
})

describe('a rule mid-draft', () => {
  it('says nothing about an outcome the document has not written', () => {
    // `outcome` is exactly the member a draft omits, and the panel used to
    // print an empty id beside "no declared outcome carries this id".
    const drafting = {
      ...full,
      rules: [{ ...full.rules[0]!, outcome: undefined as unknown as string }]
    } as PackDocument
    const lines = referencesFor(drafting, '/rules/0')
    expect(lines.some((line) => line.relation === 'outcome')).toBe(false)
  })
})

describe('a source’s references', () => {
  it('reports its citers on both sides', () => {
    const lines = referencesFor(full, '/sources/0')
    expect(lines.map((line) => `${line.relation} ${line.id}`)).toEqual(
      expect.arrayContaining([
        'cited by rule screen-first',
        'cited by rule approve-when-clear',
        'cited by exception vendor-waiver'
      ])
    )
  })
})

describe('an id the document does not declare', () => {
  it('is a document fact and never a verdict', () => {
    const broken: PackDocument = {
      ...full,
      rules: [{ ...full.rules[0]!, outcome: 'nope', sourceRefs: ['missing-source'] }]
    }
    const lines = referencesFor(broken, '/rules/0')
    expect(lines).toContainEqual({
      relation: 'outcome',
      id: 'nope',
      unresolved: 'no declared outcome carries this id'
    })
    expect(lines).toContainEqual({
      relation: 'sources',
      id: 'missing-source',
      unresolved: 'no declared source carries this id'
    })
    // The runtime's word for this is JPS-SEMANTIC-UNRESOLVED-OUTCOME. This
    // module must not shadow it with one of its own.
    const words = lines.map((line) => line.unresolved ?? '').join(' ')
    for (const verdict of ['invalid', 'error', 'broken', 'fail', 'wrong']) {
      expect(words).not.toContain(verdict)
    }
  })
})

describe('the document itself', () => {
  it('reports nothing, because a whole document is not a reference', () => {
    expect(referencesFor(full, '')).toEqual([])
    expect(referencesFor(undefined, '/rules/0')).toEqual([])
  })
})

describe('an id the document declares twice', () => {
  // A last-wins map left one pointer behind, so a rule naming `approve` linked
  // to `/outcomes/1` as though the document had said which one it meant. It
  // had said the id twice, which is a different fact and the runtime's to
  // refuse — until it does, this panel says what is there.
  const ambiguous = {
    outcomes: [{ id: 'approve' }, { id: 'approve' }, { id: 'decline' }],
    rules: [{ id: 'r0', outcome: 'approve' }, { id: 'r1', outcome: 'decline' }]
  } as unknown as PackDocument

  it('offers every place it is declared, and picks none', () => {
    const [line] = referencesFor(ambiguous, '/rules/0')
    expect(line!.relation).toBe('outcome')
    expect(line!.id).toBe('approve')
    expect(line!.target).toBeUndefined()
    expect(line!.candidates).toEqual(['/outcomes/0', '/outcomes/1'])
  })

  it('still resolves an id that is declared once', () => {
    const [line] = referencesFor(ambiguous, '/rules/1')
    expect(line!.target).toBe('/outcomes/2')
    expect(line!.candidates).toBeUndefined()
  })

  it('still says nothing carries an id nothing declares', () => {
    const missing = {
      outcomes: [{ id: 'approve' }],
      rules: [{ id: 'r0', outcome: 'nowhere' }]
    } as unknown as PackDocument
    const [line] = referencesFor(missing, '/rules/0')
    expect(line!.unresolved).toContain('no declared outcome carries this id')
    expect(line!.candidates).toBeUndefined()
  })
})

describe('a reference address is an address', () => {
  // **Two rules, deliberately.** With one, `/rules/01` reads index 1 under the
  // loose grammar and finds nothing there — so a document with a single rule
  // cannot tell a reader that refuses the address from one that accepts it and
  // then runs off the end.
  const document = {
    outcomes: [{ id: 'approve' }, { id: 'decline' }],
    rules: [
      { id: 'r0', outcome: 'approve' },
      { id: 'r1', outcome: 'decline' }
    ]
  } as unknown as PackDocument

  it.each(['/rules/01', '/rules/1e0', '/rules/-0', '/rules/'])(
    'reads no rule at %s',
    (bad) => {
      // `^\d+$` took `01` for an index, which is the same leading-zero hole
      // the evaluator had, arriving from the other side.
      expect(referencesFor(document, bad)).toEqual([])
    }
  )

  it.each([
    '/rules/0/nonesuch',
    '/rules/0/constructor',
    '/rules/0/outcome/nope',
    '/rules/0/hasOwnProperty',
    '/rules/2',
    '/rules/0/~2',
    '/outcomes/0/toString'
  ])('answers nothing for %s, which the document does not carry', (bad) => {
    // **One evaluator, or the two panels contradict each other.** This read the
    // index out of token one and never looked further, so every address under
    // `/rules/0` — including members no JSON document has — printed rule zero's
    // references while the block beside it showed nothing at all.
    expect(referencesFor(document, bad)).toEqual([])
  })

  it.each(['/rules/1/when', '/rules/1/when/conditions/0', '/rules/1/when/conditions/0/value'])(
    'still answers for %s, which is a block inside the rule',
    (deep) => {
      // The panel is opened from a selection, and a selection is any block —
      // a condition operand as readily as the rule card. Rejecting an address
      // the document does not carry must not reject the ones it does.
      const lines = referencesFor(full, deep)
      expect(lines).toContainEqual({ relation: 'outcome', id: 'approve', target: '/outcomes/0' })
    }
  )

  it('still reads the rule the address does name', () => {
    expect(referencesFor(document, '/rules/0')).toEqual([
      { relation: 'outcome', id: 'approve', target: '/outcomes/0' }
    ])
    expect(referencesFor(document, '/rules/1')).toEqual([
      { relation: 'outcome', id: 'decline', target: '/outcomes/1' }
    ])
  })
})
