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
    expect(lines).toContainEqual({
      relation: 'escalation trigger',
      id: 'screening-report',
      target: '/escalation'
    })
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
