/**
 * Amending `jpack.json` without re-implementing the runtime's schema.
 *
 * The fixture is the runtime's own graph test project, copied byte for byte
 * from `internal/graph/testdata/project/jpack.json` (v0.19.0). It is a
 * `configVersion "2"` project with a `graphs` member, which is exactly the
 * case a desk that "just wrote the config back out" would quietly break.
 */
import { describe, expect, it } from 'vitest'
import {
  NEW_PACK_VERSION,
  ProjectConfigProblem,
  existingPackKeys,
  existingPackPaths,
  packEntryFor,
  parseProjectConfig,
  serialiseProjectConfig,
  withPack
} from './jpackConfig'

const GRAPH_PROJECT = `{
  "configVersion": "2",
  "packs": {
    "sanctions-screening": {
      "path": "sanctions-screening-0.1.0.pack.json",
      "description": "Synthetic screening decision: clear or hit from the recorded match count."
    },
    "vendor-onboarding": {
      "path": "vendor-onboarding-0.1.0.pack.json",
      "description": "Synthetic onboarding decision fed by the screening decision's outcome."
    }
  },
  "graphs": {
    "onboarding": {
      "path": "onboarding.graph.json",
      "rows": "onboarding.rows.json",
      "description": "The screening decision feeds the onboarding decision, as a fact and as evidence availability."
    }
  }
}
`

describe('parseProjectConfig', () => {
  it('refuses a file that is not JSON, and one that is not an object', () => {
    expect(() => parseProjectConfig('{ nope')).toThrow(ProjectConfigProblem)
    for (const text of ['[]', '"a string"', '4', 'null']) {
      expect(() => parseProjectConfig(text), text).toThrow(/not a JSON object/)
    }
  })

  it('refuses a packs member that is not an object', () => {
    expect(() => parseProjectConfig('{"packs":[]}')).toThrow(/packs member that is not an object/)
  })

  it('decodes nothing else and validates nothing else', () => {
    // A configuration the runtime would refuse is still parsed: this module
    // adds one key and the runtime remains the only judge of the rest.
    const config = parseProjectConfig('{"configVersion":"99","packs":{},"whatever":true}')
    expect(config.configVersion).toBe('99')
    expect(config.whatever).toBe(true)
  })
})

describe('reading what is already there', () => {
  const config = parseProjectConfig(GRAPH_PROJECT)

  it('reports the pack keys and the paths they name', () => {
    expect(existingPackKeys(config)).toEqual(['sanctions-screening', 'vendor-onboarding'])
    expect(existingPackPaths(config)).toEqual([
      'sanctions-screening-0.1.0.pack.json',
      'vendor-onboarding-0.1.0.pack.json'
    ])
  })

  it('reports none where there is no packs member at all', () => {
    expect(existingPackKeys(parseProjectConfig('{}'))).toEqual([])
    expect(existingPackPaths(parseProjectConfig('{}'))).toEqual([])
  })
})

describe('withPack', () => {
  const config = parseProjectConfig(GRAPH_PROJECT)
  const amended = withPack(
    config,
    'expense-approval',
    packEntryFor('packs/expense-approval.pack.json', 'Whether an expense is approved.')
  )

  it('leaves configVersion exactly as it found it', () => {
    // `packs` exists in all three shapes, so amending a "2" project must leave
    // it at "2". Bumping it would break a runtime that reads only the earlier
    // shapes, for a member that needed no bump.
    expect(amended.configVersion).toBe('2')
  })

  it('carries graphs through untouched', () => {
    expect(amended.graphs).toEqual(config.graphs)
  })

  it('keeps every existing pack, and its position', () => {
    expect(Object.keys(amended.packs as object)).toEqual([
      'sanctions-screening',
      'vendor-onboarding',
      'expense-approval'
    ])
    expect((amended.packs as Record<string, unknown>)['vendor-onboarding']).toEqual(
      (config.packs as Record<string, unknown>)['vendor-onboarding']
    )
  })

  it('keeps the top-level key order, with packs where it already was', () => {
    expect(Object.keys(amended)).toEqual(['configVersion', 'packs', 'graphs'])
  })

  it('writes path, description and expectedVersion, and nothing else', () => {
    expect((amended.packs as Record<string, unknown>)['expense-approval']).toEqual({
      path: 'packs/expense-approval.pack.json',
      description: 'Whether an expense is approved.',
      expectedVersion: '0.1.0'
    })
  })

  it('omits a blank description rather than writing an empty string', () => {
    // `description` carries `minLength: 1`, so `""` is refused by the schema.
    for (const blank of ['', '   ', '\n']) {
      expect(packEntryFor('packs/a.pack.json', blank)).toEqual({
        path: 'packs/a.pack.json',
        expectedVersion: '0.1.0'
      })
    }
  })

  it('never writes matrix, facts or evidence', () => {
    const entry = packEntryFor('packs/a.pack.json', 'One line.')
    for (const absent of ['matrix', 'facts', 'evidence']) {
      expect(Object.keys(entry)).not.toContain(absent)
    }
  })

  it('pins the version the document will actually declare', () => {
    // `expectedVersion` is a validated reference: the runtime reports a
    // difference from the document's own `version` as a FAILED check. Both
    // come from this one constant.
    expect(packEntryFor('p', 'd').expectedVersion).toBe(NEW_PACK_VERSION)
  })

  it('adds a packs member to a configuration that somehow has none', () => {
    const amendedFlat = withPack(parseProjectConfig('{"configVersion":"1"}'), 'a-pack', {
      path: 'packs/a-pack.pack.json',
      expectedVersion: '0.1.0'
    })
    expect(Object.keys(amendedFlat.packs as object)).toEqual(['a-pack'])
    expect(amendedFlat.configVersion).toBe('1')
  })

  it('does not modify the configuration it was given', () => {
    expect(Object.keys(config.packs as object)).toEqual([
      'sanctions-screening',
      'vendor-onboarding'
    ])
  })
})

describe('serialiseProjectConfig', () => {
  it('reproduces the source file’s indent and trailing newline', () => {
    const config = parseProjectConfig(GRAPH_PROJECT)
    const text = serialiseProjectConfig(GRAPH_PROJECT, config)
    // Round-tripped with no amendment, the file comes back exactly as it was:
    // the first create must not reformat a hand-authored file wholesale.
    expect(text).toBe(GRAPH_PROJECT)
  })

  it('follows a four-space file rather than imposing two', () => {
    const source = '{\n    "configVersion": "1",\n    "packs": {}\n}\n'
    expect(serialiseProjectConfig(source, parseProjectConfig(source))).toBe(source)
  })

  it('follows a tab-indented file', () => {
    const source = '{\n\t"configVersion": "1",\n\t"packs": {}\n}\n'
    expect(serialiseProjectConfig(source, parseProjectConfig(source))).toBe(source)
  })

  it('follows a CRLF file rather than rewriting every line of it', () => {
    // `JSON.stringify` emits `\n` only. On a Windows checkout — which the
    // chassis supports, and which git's autocrlf makes ordinary — a CRLF
    // jpack.json came back entirely LF, so the maintainer's diff was every
    // line: the exact whole-file rewrite this function exists to prevent,
    // arriving through the one axis the indent does not cover.
    const source = '{\r\n  "configVersion": "1",\r\n  "packs": {}\r\n}\r\n'
    expect(serialiseProjectConfig(source, parseProjectConfig(source))).toBe(source)

    const amended = serialiseProjectConfig(
      source,
      withPack(parseProjectConfig(source), 'a', packEntryFor('packs/a.pack.json', ''))
    )
    expect(amended.includes('\n') && !amended.includes('\r\n')).toBe(false)
    expect(amended.split('\n').length - 1).toBe(amended.split('\r\n').length - 1)
    expect(amended.endsWith('\r\n')).toBe(true)
  })

  it('writes two-space indent where the source has none to measure', () => {
    const source = '{"configVersion":"1","packs":{}}'
    const text = serialiseProjectConfig(source, parseProjectConfig(source))
    expect(text).toBe('{\n  "configVersion": "1",\n  "packs": {}\n}')
    // No trailing newline was there, so none is added.
    expect(text.endsWith('\n')).toBe(false)
  })

  it('adds exactly the new entry to the graph project and changes nothing else', () => {
    const config = parseProjectConfig(GRAPH_PROJECT)
    const text = serialiseProjectConfig(
      GRAPH_PROJECT,
      withPack(config, 'expense-approval', packEntryFor('packs/expense-approval.pack.json', ''))
    )
    // Stated as an insertion into the original file rather than as a second
    // copy of it: what this asserts is that nothing else moved, which a
    // re-typed expectation could hide by being wrong in the same way twice.
    const lastEntryCloses =
      `      "description": "Synthetic onboarding decision fed by the screening decision's outcome."\n` +
      `    }\n`
    expect(GRAPH_PROJECT).toContain(lastEntryCloses)
    const expected = GRAPH_PROJECT.replace(
      lastEntryCloses,
      `      "description": "Synthetic onboarding decision fed by the screening decision's outcome."\n` +
        `    },\n` +
        `    "expense-approval": {\n` +
        `      "path": "packs/expense-approval.pack.json",\n` +
        `      "expectedVersion": "0.1.0"\n` +
        `    }\n`
    )
    expect(text).toBe(expected)
    // Said again from the other side, because it is the whole point: the
    // graph half and the version are character for character what they were.
    expect(text).toContain('"onboarding.rows.json"')
    expect(text).toContain('"configVersion": "2"')
  })
})
