/**
 * The check reader, against the runtime's own payloads.
 *
 * Every fixture below is a shape the live probe in the PR body produced from
 * jpack 0.19.0, or one the ladder's source says it produces. Nothing here is
 * invented: the two `unsupported` shapes in particular are the ones that would
 * be mis-described by any rule that read a status word instead of the rows.
 */
import { describe, expect, it } from 'vitest'
import type { ValidationReport } from '../mcp/types'
import { anchor, diagnosticsFor, isStale, layersReached, truncationNote } from './checks'

const passed = (name: string) => ({ name, status: 'passed' })

describe('which layers ran', () => {
  it('names the status, every row it was given, and the layers that never ran', () => {
    const report: ValidationReport = {
      status: 'invalid',
      layers: [passed('carrier'), { name: 'structural', status: 'failed' }],
      diagnostics: [
        {
          code: 'JPS-STRUCTURE-DECIMAL-OPERAND',
          layer: 'structural',
          severity: 'error',
          instancePath: '/rules/0/when/value',
          message: 'Ordered comparison operand must be a decimal string, for example "5000".'
        }
      ]
    }
    // The old sentence was `structural — 1 diagnostic.`: it named neither the
    // verdict the runtime reached nor the carrier layer that actually ran, so
    // a reader learned less from a failure than from a pass.
    const sentence = layersReached(report)
    expect(sentence.status).toBe('invalid')
    expect(sentence.text).toBe(
      'invalid — carrier passed, structural failed, 1 diagnostic. The semantic layer did not run.'
    )
  })

  it('names both later layers where the carrier layer failed', () => {
    const report: ValidationReport = {
      status: 'invalid',
      layers: [{ name: 'carrier', status: 'failed' }],
      diagnostics: [
        {
          code: 'JPS-CARRIER-DUPLICATE-MEMBER',
          layer: 'carrier',
          severity: 'error',
          instancePath: '/a',
          message: 'Object member name is duplicated.'
        }
      ]
    }
    expect(layersReached(report).text).toBe(
      'invalid — carrier failed, 1 diagnostic. The structural and semantic layers did not run.'
    )
  })

  it('describes an unbundled spec version without inventing a capability row', () => {
    // validator.go:203-210. The status is `unsupported`, one layer row exists,
    // and the diagnostic's own layer is `capability` — which appears in **no**
    // layer row. A sentence that turned the diagnostic's layer into a row
    // would report a layer the runtime never ran.
    const report: ValidationReport = {
      status: 'unsupported',
      layers: [passed('carrier')],
      diagnostics: [
        {
          code: 'JPS-CAPABILITY-SPEC-VERSION',
          layer: 'capability',
          severity: 'error',
          instancePath: '/specVersion',
          message: 'The exact JPS specification version is not bundled with this CLI.'
        }
      ]
    }
    const sentence = layersReached(report)
    expect(sentence.status).toBe('unsupported')
    expect(sentence.text).toBe(
      'unsupported — carrier passed, 1 diagnostic. The structural and semantic layers did not run.'
    )
    expect(sentence.text).not.toContain('capability')
  })

  it('describes an unsupported required extension as all three layers passing', () => {
    // validator.go:268-285. The same status word, and the opposite fact about
    // the ladder: everything ran.
    const report: ValidationReport = {
      status: 'unsupported',
      layers: [passed('carrier'), passed('structural'), passed('semantic')],
      extensions: { required: ['example.thing'], supported: [], unsupported: ['example.thing'] },
      diagnostics: []
    }
    const sentence = layersReached(report)
    expect(sentence.text).toBe(
      'unsupported — carrier passed, structural passed, semantic passed, 0 diagnostics.'
    )
    expect(sentence.text).not.toContain('did not run')
  })

  it('quotes the runtime’s own status word on a clean document', () => {
    const report: ValidationReport = {
      status: 'valid',
      layers: [passed('carrier'), passed('structural'), passed('semantic')],
      diagnostics: []
    }
    expect(layersReached(report).text).toBe(
      'valid — carrier passed, structural passed, semantic passed, 0 diagnostics.'
    )
  })

  it('says nothing about layers where the payload listed none', () => {
    expect(layersReached({ status: 'valid', diagnostics: [] }).text).toBe(
      'valid — 0 diagnostics. This answer lists no layer.'
    )
    expect(layersReached(undefined).text).toBe('Not checked.')
  })
})

describe('where a diagnostic is printed', () => {
  const rendered = new Set(['', '/rules', '/rules/0', '/rules/0/description', '/outcomes'])

  it('prints an exact match on that block', () => {
    const [only] = anchor(
      { diagnostics: [{ instancePath: '/rules/0/description', message: 'x' }] },
      rendered
    )
    expect(only!.anchor).toBe('/rules/0/description')
    expect(only!.approximate).toBe(false)
  })

  it('prints a missing member on its nearest rendered ancestor, naming it verbatim', () => {
    // The runtime reports a missing required member at the pointer **including
    // the absent name** (validator.go:319), so `/rules/0/when` is an address
    // for a `when` that is not there. It anchors on the rule's own card.
    const [only] = anchor(
      { diagnostics: [{ instancePath: '/rules/0/when', message: 'Required member is missing.' }] },
      rendered
    )
    expect(only!.anchor).toBe('/rules/0')
    expect(only!.named).toBe('/rules/0/when')
    expect(only!.approximate).toBe(true)
  })

  it('sends a root failure to the document strip', () => {
    // validator.go:188 — the document root is not an object, and the
    // instancePath is empty.
    const [only] = anchor(
      { diagnostics: [{ instancePath: '', message: 'The document root must be an object.' }] },
      rendered
    )
    expect(only!.anchor).toBe('')
    expect(only!.approximate).toBe(false)
  })

  it('falls back to the strip where no ancestor is rendered either', () => {
    const [only] = anchor({ diagnostics: [{ instancePath: '/sources/3/locator' }] }, new Set(['']))
    expect(only!.anchor).toBe('')
    expect(only!.named).toBe('/sources/3/locator')
    expect(only!.approximate).toBe(true)
  })

  it('collects the diagnostics one block carries', () => {
    const anchored = anchor(
      {
        diagnostics: [
          { instancePath: '/rules/0/when' },
          { instancePath: '/rules/0/outcome' },
          { instancePath: '/outcomes' }
        ]
      },
      rendered
    )
    expect(diagnosticsFor(anchored, '/rules/0')).toHaveLength(2)
    expect(diagnosticsFor(anchored, '/outcomes')).toHaveLength(1)
  })

  it('shows a diagnostic anchored under the selected member, not only on it', () => {
    // The live case: the runtime names `/rules/0/when/value`, that block is
    // rendered so the diagnostic anchors exactly there, and the reader selects
    // the rule card. Equality alone answered "No other diagnostic names this
    // member." over a rule the runtime had just refused.
    const deep = new Set(['', '/rules/0', '/rules/0/when', '/rules/0/when/value'])
    const anchored = anchor({ diagnostics: [{ instancePath: '/rules/0/when/value' }] }, deep)
    expect(anchored[0]!.anchor).toBe('/rules/0/when/value')
    expect(diagnosticsFor(anchored, '/rules/0/when/value')).toHaveLength(1)
    expect(diagnosticsFor(anchored, '/rules/0')).toHaveLength(1)
    expect(diagnosticsFor(anchored, '')).toHaveLength(1)
  })

  it('does not read one rule\u2019s pointer as a prefix of another\u2019s', () => {
    // `/rules/10` begins with `/rules/1` and is a different rule, which is why
    // the descendant test is on `pointer + "/"` and not on a bare prefix.
    const many = new Set(['', '/rules/1', '/rules/10', '/rules/10/outcome'])
    const anchored = anchor({ diagnostics: [{ instancePath: '/rules/10/outcome' }] }, many)
    expect(diagnosticsFor(anchored, '/rules/10')).toHaveLength(1)
    expect(diagnosticsFor(anchored, '/rules/1')).toHaveLength(0)
  })
})

describe('a check that describes other bytes', () => {
  it('is stale, and nothing is re-anchored onto the bytes now on screen', () => {
    // Deleting `rules[0]` moves every `/rules/N`. An anchor computed from the
    // old text would print a real diagnostic on the wrong rule.
    expect(isStale('{"a":1}', '{"a":2}')).toBe(true)
    expect(isStale('{"a":1}', '{"a":1}')).toBe(false)
    expect(isStale(undefined, '{"a":1}')).toBe(false)
  })
})

describe('a list the runtime cut', () => {
  it('says the list was cut rather than that nothing else was found', () => {
    expect(truncationNote({ diagnostics: [], diagnosticsTruncated: true })).toContain('100')
    expect(truncationNote({ diagnostics: [], diagnosticsTruncated: false })).toBeUndefined()
    expect(truncationNote(undefined)).toBeUndefined()
  })
})
