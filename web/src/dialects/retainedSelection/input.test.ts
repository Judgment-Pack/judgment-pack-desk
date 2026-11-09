import {DIALECT_RETAINED_COLUMN_DRAFT} from '@judgment-pack/harness/dialects'
import {describe, expect, it} from 'vitest'
import type {ExactRetainedInput} from './client'
import {readRetainedInput, RetainedInputError, type InputCursor, type InputSpan, type RetainedInputExpectation, type RetainedInputView} from './input'

// These are handwritten codec vectors, not captured output or Gateway/B3 evidence.
// Quoting embeds the handwritten exact custody documents; it never reconstructs them.
const q = (text: string) => JSON.stringify(text)
const pack = 'sha256:' + 'a'.repeat(64), mappingDigest = 'sha256:' + 'b'.repeat(64)
const preparationId = 'prep_' + 'c'.repeat(32), runId = 'research_' + 'd'.repeat(32)
const tuple = JSON.stringify(DIALECT_RETAINED_COLUMN_DRAFT)
const roles = (generated: boolean) => q('document-validation') + ',"preview","preparation"' + (generated ? ',"producer-carrier"' : '')
const selector = (generated: boolean) => tuple.slice(0, -1) + ',"roles":[' + roles(generated) + ']}'
const assertedCase = String.raw`{
 "values":{"first":"","last":"  kept  "},
 "prepared":{"first":"","opaque":{"\ud800":"\udfff"},"numbers":[-0,0,1e3,1E+03,1.0,1.2300e+2,9007199254740993,-9007199254740993],"texts":["-0.00","0.000000000000000001","  kept  "]},
 "origin":{"preparationId":PREP,"recordId":"row-1","sourceId":"source-original","sourceDigest":PACK,"row":37,"importedAt":"2026-10-01T00:00:00Z","importRecipeVersion":2,"recipeVersion":7,"packDigest":PACK,"originalValues":{"first":"","nullCell":null,"last":"  kept  "},"conversions":{"first":{"kind":"text"}}},
 "retained":{"contractVersion":"1","origin":"asserted","values":{"first":"","nullCell":null,"last":"  kept  "},"research":null}
}`.replaceAll('PREP', q(preparationId)).replaceAll('PACK', q(pack))
const snapshot = String.raw`{
 "version":1,"class":"generated","preparationId":PREP,
 "record":{"id":"row-1","class":"generated","sourceId":"original-run","row":19,"values":{"first":"","last":"  kept  "},"research":{"runId":RUN,"recipeVersion":3,"entity":{"fields":{"first":{"status":"supported","value":""},"unavailable":{"status":"missing"},"nullValue":{"status":"inaccessible","value":null},"stale":{"status":"stale","value":-0},"historical":{"status":"supported","value":{"\ud800":"\udfff","n":9007199254740993}}}}}},
 "recipeVersion":7,"qualification":{"digest":PACK,"conversionContract":TUPLE,"kind":"pack","key":"bound","pack":"exact pack text","mapping":{"version":2}},
 "prepared":{"unavailable":null},"values":{"first":"","last":"  kept  "},
 "research":{"id":RUN,"recipeVersion":3,"recipe":{"fields":[{"name":"first","type":"text","description":"original"},{"name":"unavailable","type":"text"}]},"intent":"kept","createdAt":"2026-10-01T00:00:00Z","approvedLimits":{"requests":2},"budgetApprovals":[],"workflowApproval":{"state":"accepted"},"modelResponses":[{"id":"attempt-1","requestDigest":"original"}],"sources":[{"proof":{"kept":true}}],"historical":{"\ud800":"\udfff","different":1E+03}},
 "reviews":[{"decision":"accepted","record":{"values":{"first":""}}}]
}`.replaceAll('PREP', q(preparationId)).replaceAll('PACK', q(pack)).replaceAll('RUN', q(runId)).replaceAll('TUPLE', tuple)
const frame = String.raw`{"contractVersion":"1","origin":"generated","values":{"first":"","last":"  kept  "},"research":{"recordRecipeRevision":"original-revision","declaration":{"recipeRevision":"original-revision","fields":[{"name":"first","fieldJson":"{\"name\":\"first\",\"type\":\"text\"}"},{"name":"unavailable","fieldJson":"{\"raw\":\"\\ud800\"}"}]},"fields":{"first":{"status":"supported","valueJson":"\"\""},"unavailable":{"status":"missing"},"nullValue":{"status":"inaccessible","valueJson":"null"},"stale":{"status":"stale","valueJson":"-0"},"historical":{"status":"supported","valueJson":"{\"k\":\"\\ud800\"}"}}}}`
function response(original = snapshot, retained = frame): string {
 return '{"result":{"contractVersion":"1","originalSnapshotJson":' + q(original) + ',"originalSnapshotSha256":' + q('sha256:' + 'e'.repeat(64)) + ',"retained":' + retained + '},"receipt":{"signature":"kept","extra":{"raw":true}},"salts":{"first":"kept"}}'
}
function preparation(generated: boolean): string {
 return '{"conversionContract":' + selector(generated) + ',"version":2,"verifiedAt":"2026-10-01T12:30:00Z","mappingDigest":' + q(mappingDigest) + ',"verification":"owner-defined verification","cites":[{"first":"citation"},{"last":null}],"lineage":[{"target":"first","columnTransformation":{"input":{"raw":false},"ref":{"source":"research","pointer":"/retained"}},"futureDetail":{"keep":[1,2]}}],"outcomes":[{"first":false},{"last":"kept"}]}'
}
const defaultFacts = String.raw`{"__proto__":null,"a/b":{"~key":[false,0,-0,""," ",{},[]]},"":{"dot.key":"<script>&\u2028😀\uD83D\uDE00\u0000"},"constructor":{},"prototype":[],"a.b":"literal","\u0065scaped":"BMP é"}`
function asserted(custody = assertedCase, facts = defaultFacts, evidence = ',"evidence":null'): string {
 return '{"facts":' + facts + evidence + ',"source":{"conversionContract":' + selector(false) + ',"mapping":{"version":2,"case":{"facts":[],"evidence":[]}},"mappingDigest":' + q(mappingDigest) + ',"retainedCaseJson":' + q(custody) + '},"preparation":' + preparation(false) + '}'
}
function generated(signed = response(), facts = defaultFacts): string {
 return '{"facts":' + facts + ',"source":{"conversionContract":' + selector(true) + ',"mapping":{"version":2,"sources":[{"name":"research","kind":"operation","profile":"desk-reviewed-research-v2","profileDigest":"independent-profile","read":{"copy":{"facts":[],"evidence":[]}}}]},"mappingDigest":' + q(mappingDigest) + ',"case":{"grant":"retained-grant","digest":' + q(pack) + '},"sources":{"research":{"retainedResponseJson":' + q(signed) + '}}},"preparation":' + preparation(true) + '}'
}
function expectation(origin: 'asserted' | 'generated' = 'asserted'): RetainedInputExpectation {
 return {header: {wireVersion: 1, viewId: 'rv_' + 'f'.repeat(48), preparationId, revision: '1'.repeat(64), packDigest: pack, conversionContract: {...DIALECT_RETAINED_COLUMN_DRAFT, spec: {...DIALECT_RETAINED_COLUMN_DRAFT.spec}}, origin, rows: 2, maxInputBytes: 37748736, selectionSha256: '2'.repeat(64)}, row: {index: 0, id: 'row-1', inputBytes: 1, inputSha256: '3'.repeat(64), offset: 100}}
}
function exact(text: string, expected = expectation()): ExactRetainedInput {
 return {text, sourceRevision: expected.header.revision, rowId: expected.row.id, index: expected.row.index, inputSha256: expected.row.inputSha256}
}
function read(text = asserted(), expected = expectation()) { return readRetainedInput(exact(text, expected), expected) }
function at(view: RetainedInputView, root: InputSpan, pointer: string): InputSpan {
 const presence = view.lookup(root, pointer)
 expect(presence.kind).toBe('present')
 if (presence.kind !== 'present') throw Error('Test pointer missing')
 return presence.span
}
function raw(view: RetainedInputView, root: InputSpan, pointer: string) { return view.raw(at(view, root, pointer)) }
function refusal(action: () => unknown, kind: 'invalid-input' | 'identity-mismatch' | 'invalid-argument' = 'invalid-input') {
 let thrown: unknown
 try { action() } catch (error) { thrown = error }
 expect(thrown).toBeInstanceOf(RetainedInputError)
 expect(thrown).toMatchObject({kind, message: `Retained input ${kind}.`})
}

describe('inactive exact retained Input reader', () => {
 it('retains asserted siblings and source identities without a root revision or index substitution', () => {
  const text = asserted(), view = read(text)
  expect(Object.isFrozen(view)).toBe(true); expect(Object.isFrozen(view.sections)).toBe(true)
  expect(view.inputText).toBe(text); expect(view.sourceText('input')).toBe(text)
  expect(view.sources.map(s => s.id)).toEqual(['input', 'case'])
  const source = view.sources[1]!
  expect(view.decodeString(source.parent!, 'scalar')).toBe(assertedCase)
  expect(view.sourceText('case')).toBe(assertedCase)
  expect(view.raw(view.sections.custody)).toBe(assertedCase)
  expect(raw(view, view.sections.origin!, '/row')).toBe('37')
  expect(view.identity.index).toBe(0); expect(view.identity.provenanceChecked).toEqual([])
  for (const pointer of ['/values/first', '/prepared/first', '/origin/conversions/first/kind', '/retained/values/last']) expect(view.lookup(view.sections.custody, pointer).kind).toBe('present')
  for (const pointer of ['/cites/1/last', '/lineage/0/futureDetail/keep/1', '/outcomes/1/last', '/verification']) expect(view.lookup(view.sections.preparation, pointer).kind).toBe('present')
  expect(raw(view, view.sections.originalValues, '/nullCell')).toBe('null')
  expect(view.sections.evidence).toMatchObject({kind: 'present', span: {kind: 'null'}})
  expect(view.sections.root.source).toBe('input'); expect(view.sections.frame.source).toBe('case')
 })
 it('retains generated response, original snapshot, frame, declaration, history and opaque siblings', () => {
  const view = read(generated(), expectation('generated'))
  expect(view.sources.map(s => s.id)).toEqual(['input', 'response', 'snapshot'])
  expect(view.sourceText('snapshot')).toBe(snapshot)
  expect(view.decodeString(view.sources[2]!.parent!, 'scalar')).toBe(snapshot)
  expect(raw(view, view.sections.response!, '/receipt/signature')).toBe('"kept"')
  expect(raw(view, view.sections.response!, '/salts/first')).toBe('"kept"')
  expect(raw(view, view.sections.snapshot!, '/research/modelResponses/0/id')).toBe('"attempt-1"')
  expect(raw(view, view.sections.snapshot!, '/qualification/pack')).toBe('"exact pack text"')
  expect(raw(view, view.sections.history!, '/0/record/values/first')).toBe('""')
  expect(raw(view, view.sections.frame, '/research/fields/stale/valueJson')).toBe('"-0"')
  expect(view.lookup(view.sections.frame, '/research/fields/unavailable/valueJson')).toEqual({kind: 'missing'})
  expect(view.decodeString(at(view, view.sections.frame, '/research/fields/nullValue/valueJson'), 'scalar')).toBe('null')
  expect(view.decodeString(at(view, view.sections.frame, '/research/fields/historical/valueJson'), 'scalar')).toBe(String.raw`{"k":"\ud800"}`)
  expect(view.decodeString(at(view, view.sections.declaration!, '/fields/1/fieldJson'), 'scalar')).toBe(String.raw`{"raw":"\ud800"}`)
  expect(view.sections.evidence).toEqual({kind: 'missing'})
  expect(raw(view, view.sections.lineage, '/0/columnTransformation/input/raw')).toBe('false')
  expect(raw(view, view.sections.source, '/case/grant')).toBe('"retained-grant"')
 })
 it('preserves every number spelling and whitespace-bearing string without coercion', () => {
  const view = read(), numbers = at(view, view.sections.custody, '/prepared/numbers')
  expect(view.elements(numbers, 20).entries.map(span => view.raw(span))).toEqual(['-0', '0', '1e3', '1E+03', '1.0', '1.2300e+2', '9007199254740993', '-9007199254740993'])
  const texts = at(view, view.sections.custody, '/prepared/texts')
  expect(view.elements(texts, 20).entries.map(span => view.decodeString(span, 'raw'))).toEqual(['-0.00', '0.000000000000000001', '  kept  '])
 })
 it('distinguishes presence, null, empty/blank, false, signed zero and empty containers', () => {
  const view = read(), facts = view.sections.facts, array = at(view, facts, '/a~1b/~0key')
  expect(view.lookup(facts, '/missing')).toEqual({kind: 'missing'})
  expect(raw(view, facts, '/__proto__')).toBe('null')
  expect(view.elements(array, 20).entries.map(span => view.raw(span))).toEqual(['false', '0', '-0', '""', '" "', '{}', '[]'])
  expect(raw(view, facts, '/a.b')).toBe('"literal"')
  expect(raw(view, facts, '/constructor')).toBe('{}'); expect(raw(view, facts, '/prototype')).toBe('[]')
  expect(raw(view, facts, '/escaped')).toBe('"BMP é"')
  expect(view.decodeString(at(view, facts, '//dot.key'), 'scalar')).toBe('<script>&\u2028😀😀\u0000')
 })
 it('keeps opaque lone-surrogate tokens exact and restricts scalar metadata only', () => {
  const view = read(), opaque = at(view, view.sections.custody, '/prepared/opaque')
  const entry = view.members(opaque, 1).entries[0]!
  expect(view.raw(entry.key)).toBe(String.raw`"\ud800"`); expect(view.raw(entry.value)).toBe(String.raw`"\udfff"`)
  expect(view.decodeString(entry.key, 'raw')).toBe('\ud800')
  refusal(() => view.decodeString(entry.key, 'scalar'))
  const generatedView = read(generated(), expectation('generated'))
  expect(raw(generatedView, generatedView.sections.research!, '/historical/\ud800')).toBe(String.raw`"\udfff"`)
  // A JSON-looking arbitrary string is not a custody document.
  expect(read(asserted(assertedCase, '{"text":"{broken json"}')).sections.facts.kind).toBe('object')
  for (const text of ['\ud800', '\udfff']) refusal(() => read(asserted(assertedCase, '{"raw":' + q(text).replace(/\\u[dD][89a-fA-F][0-9a-fA-F]{2}/, text) + '}')))
  refusal(() => read(asserted(assertedCase.replace('source-original', String.raw`source-\ud800`))))
  refusal(() => read(asserted().replace(q(assertedCase), String.raw`"\ud800"`)))
  refusal(() => read(generated().replace(q(response()), String.raw`"\ud800"`), expectation('generated')))
  refusal(() => read(asserted(assertedCase.replace('"last":"  kept  "', String.raw`"last":"\ud800"`).replace('"last":"  kept  "', String.raw`"last":"\ud800"`).replace('"last":"  kept  "', String.raw`"last":"\ud800"`))))
  refusal(() => read(generated(response(snapshot.replace('"status":"missing"', String.raw`"status":"\ud800"`))), expectation('generated')))
  refusal(() => read(generated(response(snapshot.replace('"description":"original"', String.raw`"description":"\ud800"`))), expectation('generated')))
 })
 it('checks decoded duplicate names in root, skipped details and every custody document', () => {
  const badFacts = [String.raw`{"a":1,"a":2}`, String.raw`{"a":1,"\u0061":2}`, String.raw`{"😀":1,"\uD83D\uDE00":2}`, String.raw`{"outer":{"skip":[{"x":1,"\u0078":2}]}}`]
  for (const facts of badFacts) refusal(() => read(asserted(assertedCase, facts)))
  refusal(() => read(asserted().replace('{"facts":', '{"facts":{},"facts":')))
  refusal(() => read(asserted(assertedCase.replace('"first":"","opaque"', '"first":"","first":null,"opaque"'))))
  refusal(() => read(generated(response(snapshot.replace('"historical":{"\\ud800":"\\udfff","different":1E+03}', '"historical":{"a":1,"\\u0061":2}'))), expectation('generated')))
  refusal(() => read(generated(response().replace('"signature":"kept"', '"signature":"kept","\\u0073ignature":"alias"')), expectation('generated')))
  const distinct = read(asserted(assertedCase, String.raw`{"é":1,"é":2,"\ud800":3,"\ud801":4}`))
  expect(distinct.members(distinct.sections.facts, 10).entries.map(e => distinct.raw(e.value))).toEqual(['1', '2', '3', '4'])
 })
 it('rejects incomplete documents at every cut, trailing data, BOM and malformed grammar', () => {
  // Every character cut includes every token boundary, including inside custody strings.
  for (const origin of ['asserted', 'generated'] as const) {
   const text = origin === 'asserted' ? asserted(assertedCase, '{}', '') : generated(response(), '{}')
   const expected = expectation(origin)
   for (let end = 0; end < text.length; end++) refusal(() => read(text.slice(0, end), expected))
   for (const extra of ['{}', 'x', '\ufeff']) refusal(() => read(text + extra, expected))
   refusal(() => read('\ufeff' + text, expected))
  }
  for (const value of ['+1', '01', '1.', '1e', 'NaN', 'Infinity', '-Infinity', '-', 'truex', '[1,]', '{"a":}', '{"a" 1}', '{"a":1 "b":2}', '{"a":1,}', '"\\x"', '"\\u0z00"', '"unterminated', '"raw\u0000control"']) refusal(() => read(asserted(assertedCase, '{"v":' + value + '}')))
  refusal(() => read(asserted(assertedCase + '{}')))
  refusal(() => read(generated(response(snapshot + ' false')), expectation('generated')))
  expect(read(' \t\r\n' + asserted() + '\n ').sourceText('input')[0]).toBe(' ')
 })
 it('binds each external envelope value and installed tuple scalar', () => {
  const expected = expectation(), input = exact(asserted(), expected)
  for (const change of [{sourceRevision: 'changed'}, {rowId: 'other'}, {index: 1}, {inputSha256: 'changed'}]) refusal(() => readRetainedInput({...input, ...change}, expected), 'identity-mismatch')
  for (const change of [{index: -1}, {index: 0.5}, {index: 2}]) refusal(() => readRetainedInput(input, {...expected, row: {...expected.row, ...change}}), 'invalid-argument')
  for (const key of ['contract', 'profile'] as const) {
   const altered = {...expected, header: {...expected.header, conversionContract: {...expected.header.conversionContract, [key]: 'wrong'}}}
   refusal(() => readRetainedInput(input, altered), 'identity-mismatch')
  }
  for (const key of ['version', 'revision', 'schemaSha256', 'corpusSha256'] as const) {
   const altered = {...expected, header: {...expected.header, conversionContract: {...expected.header.conversionContract, spec: {...expected.header.conversionContract.spec, [key]: 'wrong'}}}}
   refusal(() => readRetainedInput(input, altered), 'identity-mismatch')
  }
  refusal(() => readRetainedInput(input, {...expected, header: {...expected.header, origin: 'generated'}}))
  for (const field of ['contract', 'profile', 'version', 'revision', 'schemaSha256', 'corpusSha256'] as const) {
   const old = field === 'contract' || field === 'profile' ? DIALECT_RETAINED_COLUMN_DRAFT[field] : DIALECT_RETAINED_COLUMN_DRAFT.spec[field]
   refusal(() => read(asserted().replace(q(field) + ':' + q(old), q(field) + ':"wrong"')), 'identity-mismatch')
   // The preparation copy is independently bound, too.
   const marker = '"preparation":', text = asserted(), split = text.indexOf(marker)
   refusal(() => read(text.slice(0, split) + text.slice(split).replace(q(field) + ':' + q(old), q(field) + ':"wrong"')), 'identity-mismatch')
  }
 })
 it('binds asserted preparation/record/pack, ordered roles and mapping digests', () => {
  for (const [before, after] of [[q(preparationId), '"other-preparation"'], ['"recordId":"row-1"', '"recordId":"other"'], ['"packDigest":' + q(pack), '"packDigest":"other-pack"'], ['"origin":"asserted"', '"origin":"generated"'], ['"contractVersion":"1"', '"contractVersion":"2"']]) refusal(() => read(asserted(assertedCase.replace(before!, after!))), 'identity-mismatch')
  const expected = expectation()
  for (const roleText of ['', '"preview","document-validation","preparation"', '"document-validation","preview"', '"document-validation","preview","preparation","extra"', '"document-validation","unknown","preparation"']) refusal(() => read(asserted().replace(roles(false), roleText)), 'identity-mismatch')
  refusal(() => read(asserted().replace('"mappingDigest":' + q(mappingDigest), '"mappingDigest":' + q('sha256:' + '0'.repeat(64)))), 'identity-mismatch')
  refusal(() => read(asserted().replaceAll(q(mappingDigest), '"bad"')))
  const provenance = {origin: 'asserted' as const, sourceId: 'source-original', sourceDigest: pack, row: '37', importRecipeVersion: '2', recipeVersion: '7'}
  expect(read(asserted(), {...expected, provenance}).identity.provenanceChecked).toEqual(['sourceId', 'sourceDigest', 'row', 'importRecipeVersion', 'recipeVersion'])
  for (const key of ['sourceId', 'sourceDigest', 'row', 'importRecipeVersion', 'recipeVersion'] as const) refusal(() => read(asserted(), {...expected, provenance: {...provenance, [key]: 'different'}}), 'identity-mismatch')
  expect(read(asserted(), {...expected, provenance: {origin: 'asserted'}}).identity.provenanceChecked).toEqual([])
 })
 it('binds generated original identities and exact integer provenance separately from collection revision', () => {
  const expected = expectation('generated')
  refusal(() => read(generated().replace(roles(true), roles(false)), expected), 'identity-mismatch')
  const prepStart = generated().indexOf('"preparation":')
  refusal(() => read(generated().slice(0, prepStart) + generated().slice(prepStart).replace(roles(true), '"preview","document-validation","preparation","producer-carrier"'), expected), 'identity-mismatch')
  const changes: readonly [string, string][] = [[q(preparationId), '"other-preparation"'], ['"id":"row-1"', '"id":"other"'], ['"class":"generated"', '"class":"asserted"'], ['"digest":' + q(pack), '"digest":"other"'], ['"runId":' + q(runId), '"runId":"other"'], ['"recipeVersion":3', '"recipeVersion":4']]
  for (const [before, after] of changes) refusal(() => read(generated(response(snapshot.replace(before, after))), expected), 'identity-mismatch')
  refusal(() => read(generated(response(snapshot.replace('"record":{"id":"row-1","class":"generated"', '"record":{"id":"row-1","class":"asserted"'))), expected), 'identity-mismatch')
  refusal(() => read(generated(response(snapshot, frame.replace('"recipeRevision":"original-revision"', '"recipeRevision":"other"'))), expected), 'identity-mismatch')
  refusal(() => read(generated(response(snapshot.replace('"recipeVersion":3', '"recipeVersion":3.0'))), expected))
  refusal(() => read(generated(response(snapshot.replace('"profile":' + q(DIALECT_RETAINED_COLUMN_DRAFT.profile), '"profile":"other"'))), expected), 'identity-mismatch')
  const provenance = {origin: 'generated' as const, runId, originalRecipeVersion: '3', recipeVersion: '7'}
  expect(read(generated(), {...expected, provenance}).identity.provenanceChecked).toEqual(['runId', 'originalRecipeVersion', 'recipeVersion'])
  for (const key of ['runId', 'originalRecipeVersion', 'recipeVersion'] as const) refusal(() => read(generated(), {...expected, provenance: {...provenance, [key]: 'different'}}), 'identity-mismatch')
  refusal(() => read(generated(), {...expected, provenance: {origin: 'asserted'}}), 'identity-mismatch')
  // No Number conversion: neighboring integers beyond binary64 stay distinct.
  const large = snapshot.replaceAll('"recipeVersion":3', '"recipeVersion":9007199254740993')
  expect(read(generated(response(large)), {...expected, provenance: {origin: 'generated', originalRecipeVersion: '9007199254740993'}}).identity.provenanceChecked).toEqual(['originalRecipeVersion'])
  refusal(() => read(generated(response(large)), {...expected, provenance: {origin: 'generated', originalRecipeVersion: '9007199254740992'}}), 'identity-mismatch')
 })
 it('rejects wrong and unknown closed shapes without filtering detail siblings', () => {
  for (const text of ['{}', '[]', 'null', '{"input":' + asserted() + '}', asserted().replace('{"facts":', '{"unknown":null,"facts":'), asserted().replace('"source":', '"absentSource":'), asserted().replace('"preparation":', '"absentPreparation":'), asserted().replace('"mapping":{"version":2', '"unknown":null,"mapping":{"version":2'), asserted().replace('"cites":[', '"unknown":null,"cites":[')]) refusal(() => read(text))
  for (const [name, value] of [
   ['cites', '[{"first":"citation"},{"last":null}]'],
   ['lineage', '[{"target":"first","columnTransformation":{"input":{"raw":false},"ref":{"source":"research","pointer":"/retained"}},"futureDetail":{"keep":[1,2]}}]'],
   ['outcomes', '[{"first":false},{"last":"kept"}]'],
  ]) refusal(() => read(asserted().replace(q(name!) + ':' + value!, q(name!) + ':null')))
  for (const bad of ['null', '[]', '"wrong"']) {
   refusal(() => read(asserted().replace('"conversionContract":' + selector(false), '"conversionContract":' + bad)))
   const spec = JSON.stringify(DIALECT_RETAINED_COLUMN_DRAFT.spec)
   refusal(() => read(asserted().replace('"spec":' + spec, '"spec":' + bad)))
  }
  for (const bad of ['null', '"wrong"']) refusal(() => read(asserted().replace('"roles":[' + roles(false) + ']', '"roles":' + bad)))
  refusal(() => read(asserted().replace('"retainedCaseJson":', '"sources":{},"retainedCaseJson":')))
  refusal(() => read(generated().replace('"case":{"grant":', '"retainedCaseJson":"{}","case":{"grant":'), expectation('generated')))
  refusal(() => read(asserted().replace('2026-10-01T12:30:00Z', '2026-02-30T12:30:00Z')))
  refusal(() => read(asserted().replace('2026-10-01T12:30:00Z', '2026-10-01T12:30:00.000Z')))
  refusal(() => read(asserted().replace('owner-defined verification', '')))
 })
 it('requires the full origin-specific custody metadata while preserving detail bodies', () => {
  for (const name of ['values', 'prepared', 'origin', 'retained']) refusal(() => read(asserted(assertedCase.replace(q(name) + ':', q('missing-' + name) + ':'))))
  for (const name of ['preparationId', 'recordId', 'sourceId', 'sourceDigest', 'row', 'importedAt', 'importRecipeVersion', 'recipeVersion', 'packDigest', 'originalValues', 'conversions']) refusal(() => read(asserted(assertedCase.replace(q(name) + ':', q('missing-' + name) + ':'))))
  refusal(() => read(asserted(assertedCase.replace('"research":null', '"research":{}'))))
  refusal(() => read(asserted(assertedCase.replace('"origin":"asserted"', '"extra":false,"origin":"asserted"'))))
  refusal(() => read(asserted(assertedCase.replace('"nullCell":null', '"nullCell":false'))))
  const expected = expectation('generated')
  for (const name of ['version', 'class', 'preparationId', 'record', 'recipeVersion', 'qualification', 'prepared', 'values', 'research', 'reviews']) refusal(() => read(generated(response(snapshot.replace(q(name) + ':', q('missing-' + name) + ':'))), expected))
  for (const before of ['"result":', '"receipt":', '"salts":']) refusal(() => read(generated(response().replace(before, '"missing-' + before.slice(1))), expected))
  for (const before of ['"contractVersion":', '"originalSnapshotJson":', '"originalSnapshotSha256":', '"retained":']) refusal(() => read(generated(response().replace(before, '"missing-' + before.slice(1))), expected))
  refusal(() => read(generated(response().replace('"salts":', '"unknown":null,"salts":')), expected))
  refusal(() => read(generated(response().replace('"originalSnapshotSha256":', '"unknown":null,"originalSnapshotSha256":')), expected))
  for (const badFrame of [
   frame.replace('"recordRecipeRevision":', '"missingRevision":'),
   frame.replace('"declaration":', '"missingDeclaration":'),
   frame.replace('"fields":[', '"fields":null,"discarded":['),
   frame.replace('"status":"missing"', '"status":"unknown"'),
   frame.replace('"valueJson":"null"', '"valueJson":null'),
   frame.replace('"fieldJson":', '"missingFieldJson":'),
   frame.replace('"name":"first"', '"name":""'),
  ]) refusal(() => read(generated(response(snapshot, badFrame)), expected))
  for (const status of ['supported', 'interpretation', 'missing', 'conflicting', 'inaccessible', 'stale']) {
   const view = read(generated(response(snapshot.replace('"status":"missing"', '"status":' + q(status)), frame.replace('"status":"missing"', '"status":' + q(status)))), expected)
   expect(raw(view, view.sections.frame, '/research/fields/unavailable/status')).toBe(q(status))
  }
  // An optional evidence token has its own presence and type, with no inferred object gate.
  for (const value of ['null', 'false', '0', '""', '[]', '{}']) {
   const view = read(asserted(assertedCase, '{}', ',"evidence":' + value))
   expect(raw(view, view.sections.root, '/evidence')).toBe(value)
  }
 })
 it('offers complete lazy windows with bound cursors, exact pointer keys and stable argument errors', () => {
  const view = read(), facts = view.sections.facts
  let cursor: InputCursor | undefined, names: string[] = []
  do {
   const window = view.members(facts, 2, cursor)
   names = [...names, ...window.entries.map(entry => entry.name)]
   expect(Object.isFrozen(window.entries)).toBe(true)
   cursor = window.next ?? undefined
   if (window.done) break
  } while (cursor)
  expect(names).toEqual(['__proto__', 'a/b', '', 'constructor', 'prototype', 'a.b', 'escaped'])
  const array = at(view, facts, '/a~1b/~0key'), items: string[] = []
  cursor = undefined
  do {
   const window = view.elements(array, 1, cursor)
   items.push(...window.entries.map(span => view.raw(span))); cursor = window.next ?? undefined
   if (window.done) break
  } while (cursor)
  expect(items).toEqual(['false', '0', '-0', '""', '" "', '{}', '[]'])
  const empty = at(view, array, '/6')
  expect(view.elements(empty, 1)).toEqual({entries: [], next: null, done: true})
  const first = view.members(facts, 1), again = view.members(facts, 1)
  expect(first.entries[0]!.name).toBe('__proto__'); expect(again.entries[0]!.name).toBe('__proto__')
  expect(view.members(facts, 1, first.next!).entries[0]!.name).toBe('a/b')
  expect(view.members(facts, 1, first.next!).entries[0]!.name).toBe('a/b')
  expect(raw(view, view.sections.custody, '/origin/recipeVersion')).toBe('7')
  for (const size of [0, -1, 1.5, Infinity, NaN]) refusal(() => view.members(facts, size), 'invalid-argument')
  refusal(() => view.elements(facts, 1), 'invalid-argument')
  refusal(() => view.members(facts, 1, {} as InputCursor), 'invalid-argument')
  refusal(() => view.elements(array, 1, first.next!), 'invalid-argument')
  const other = read()
  refusal(() => other.members(other.sections.facts, 1, first.next!), 'invalid-argument')
  refusal(() => view.raw(other.sections.facts), 'invalid-argument')
  refusal(() => view.raw({...facts}), 'invalid-argument')
  refusal(() => view.sourceText('unknown'), 'invalid-argument')
  refusal(() => view.decodeString(facts, 'raw'), 'invalid-argument')
  for (const pointer of ['facts', '/bad~', '/bad~2']) refusal(() => view.lookup(facts, pointer), 'invalid-argument')
  expect(view.lookup(array, '/01')).toEqual({kind: 'missing'})
  expect(view.lookup(facts, '/a.b/deeper')).toEqual({kind: 'missing'})
  expect(view.lookup(facts, '')).toEqual({kind: 'present', span: facts})
 })
 it('does not impose a rendering-count or recursive-call depth acceptance cap', () => {
  const facts = '{' + Array.from({length: 1201}, (_, index) => q('field-' + index) + ':false').join(',') + ',"deep":' + '['.repeat(2500) + 'null' + ']'.repeat(2500) + '}'
  const view = read(asserted(assertedCase, facts)), names: string[] = []
  let cursor: InputCursor | undefined
  do {
   const page = view.members(view.sections.facts, 17, cursor)
   names.push(...page.entries.map(entry => entry.name)); cursor = page.next ?? undefined
   if (page.done) break
  } while (cursor)
  expect(names.length).toBe(1202); expect(names[0]).toBe('field-0'); expect(names[1200]).toBe('field-1200'); expect(names[1201]).toBe('deep')
  expect(raw(view, view.sections.facts, '/field-1200')).toBe('false')
  expect(raw(view, view.sections.facts, '/deep')).toBe('['.repeat(2500) + 'null' + ']'.repeat(2500))
 })
})
