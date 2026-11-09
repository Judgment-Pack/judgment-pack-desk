import {DIALECT_RETAINED_COLUMN_DRAFT} from '@judgment-pack/harness/dialects'
import type {ExactRetainedInput} from './client'
import type {RetainedSelectionDescriptor, RetainedSelectionHeader} from './wire'

export type RetainedInputFailure = 'invalid-input' | 'identity-mismatch' | 'invalid-argument'
export class RetainedInputError extends Error {
 constructor(readonly kind: RetainedInputFailure) { super(`Retained input ${kind}.`); this.name = 'RetainedInputError' }
}
const fail = (kind: RetainedInputFailure = 'invalid-input'): never => { throw new RetainedInputError(kind) }
export type TokenKind = 'object' | 'array' | 'string' | 'number' | 'boolean' | 'null'
/** Half-open UTF-16 offsets in the indicated exact source, never byte offsets. */
export interface InputSpan {readonly source: string; readonly start: number; readonly end: number; readonly kind: TokenKind}
export type InputPresence = Readonly<{kind: 'missing'} | {kind: 'present'; span: InputSpan}>
export interface InputSource {readonly id: string; readonly root: InputSpan; readonly parent?: InputSpan}
declare const cursorBrand: unique symbol
export interface InputCursor {readonly [cursorBrand]: true}
export interface InputMember {readonly name: string; readonly key: InputSpan; readonly value: InputSpan}
export interface InputWindow<T> {readonly entries: readonly T[]; readonly next: InputCursor | null; readonly done: boolean}
export type RetainedInputProvenance =
 | {readonly origin: 'asserted'; readonly sourceId?: string; readonly sourceDigest?: string; readonly row?: string; readonly importRecipeVersion?: string; readonly recipeVersion?: string}
 | {readonly origin: 'generated'; readonly runId?: string; readonly originalRecipeVersion?: string; readonly recipeVersion?: string}
export interface RetainedInputExpectation {
 readonly header: Readonly<RetainedSelectionHeader>
 readonly row: Readonly<RetainedSelectionDescriptor>
 /** Only exact frozen-owner values; decimal integer tokens are strings. */
 readonly provenance?: RetainedInputProvenance
}
export interface RetainedInputView {
 readonly inputText: string
 readonly identity: Readonly<{sourceRevision: string; rowId: string; index: number; inputSha256: string; origin: 'asserted' | 'generated'; provenanceChecked: readonly string[]}>
 readonly sources: readonly InputSource[]
 readonly sections: Readonly<{
  root: InputSpan; facts: InputSpan; evidence: InputPresence; source: InputSpan; mapping: InputSpan; preparation: InputSpan
  cites: InputSpan; lineage: InputSpan; outcomes: InputSpan; custody: InputSpan; frame: InputSpan; originalValues: InputSpan
  origin?: InputSpan; response?: InputSpan; carrier?: InputSpan; snapshot?: InputSpan; record?: InputSpan; research?: InputSpan; declaration?: InputSpan; history?: InputSpan
 }>
 sourceText(source: string): string
 raw(span: InputSpan): string
 decodeString(span: InputSpan, policy: 'scalar' | 'raw'): string
 lookup(span: InputSpan, pointer: string): InputPresence
 members(span: InputSpan, size: number, cursor?: InputCursor): InputWindow<InputMember>
 elements(span: InputSpan, size: number, cursor?: InputCursor): InputWindow<InputSpan>
}
const missing: InputPresence = Object.freeze({kind: 'missing'})
const ws = (c: string | undefined) => c === ' ' || c === '\t' || c === '\n' || c === '\r'
function space(text: string, at: number): number { while (ws(text[at])) at++; return at }
function scalar(text: string): boolean {
 for (let i = 0; i < text.length; i++) {
  const c = text.charCodeAt(i)
  if (c >= 0xd800 && c <= 0xdbff) { const next = text.charCodeAt(++i); if (!(next >= 0xdc00 && next <= 0xdfff)) return false }
  else if (c >= 0xdc00 && c <= 0xdfff) return false
 }
 return true
}
function hex(c: number): number { return c >= 48 && c <= 57 ? c - 48 : c >= 65 && c <= 70 ? c - 55 : c >= 97 && c <= 102 ? c - 87 : fail() }
/** Validate a string token without decoding/copying its value. Escaped lone units are JSON data. */
function stringEnd(text: string, start: number): number {
 let at = start + 1
 while (at < text.length) {
  const c = text.charCodeAt(at++)
  if (c === 34) return at
  if (c < 32) return fail()
  if (c === 92) {
   const escape = text[at++]
   if (escape === 'u') { for (let n = 0; n < 4; n++) hex(text.charCodeAt(at++)) }
   else if (escape === undefined || !'"\\/bfnrt'.includes(escape)) return fail()
  } else if (c >= 0xd800 && c <= 0xdbff) {
   const low = text.charCodeAt(at++)
   if (!(low >= 0xdc00 && low <= 0xdfff)) return fail()
  } else if (c >= 0xdc00 && c <= 0xdfff) return fail()
 }
 return fail()
}
function decode(text: string, start: number, end: number, policy: 'scalar' | 'raw'): string {
 let result = '', from = start + 1
 for (let at = from; at < end - 1; at++) {
  if (text[at] !== '\\') continue
  result += text.slice(from, at)
  const c = text[++at]
  if (c === 'u') {
   let unit = 0
   for (let i = 0; i < 4; i++) unit = unit * 16 + hex(text.charCodeAt(++at))
   result += String.fromCharCode(unit)
  } else {
   const escapes: Readonly<Record<string, string>> = {'"': '"', '\\': '\\', '/': '/', b: '\b', f: '\f', n: '\n', r: '\r', t: '\t'}
   result += escapes[c!]!
  }
  from = at + 1
 }
 result += text.slice(from, end - 1)
 if (policy === 'scalar' && !scalar(result)) return fail()
 return result
}
interface ScanFrame {kind: 'object' | 'array'; state: 'first' | 'key' | 'colon' | 'value' | 'after'; keys?: Set<string>}
interface Token {start: number; end: number; kind: TokenKind}
function primitive(text: string, start: number): Token {
 const c = text[start]
 if (c === '"') return {start, end: stringEnd(text, start), kind: 'string'}
 for (const [literal, kind] of [['true', 'boolean'], ['false', 'boolean'], ['null', 'null']] as const) {
  if (text.startsWith(literal, start)) return {start, end: start + literal.length, kind}
 }
 let at = start
 if (text[at] === '-') at++
 if (text[at] === '0') at++
 else {
  if (!(text[at]! >= '1' && text[at]! <= '9')) return fail()
  do { at++ } while (text[at]! >= '0' && text[at]! <= '9')
 }
 if (text[at] === '.') {
  at++; const digits = at
  while (text[at]! >= '0' && text[at]! <= '9') at++
  if (at === digits) return fail()
 }
 if (text[at] === 'e' || text[at] === 'E') {
  at++; if (text[at] === '+' || text[at] === '-') at++
  const digits = at
  while (text[at]! >= '0' && text[at]! <= '9') at++
  if (at === digits) return fail()
 }
 return {start, end: at, kind: 'number'}
}
/** Complete iterative grammar pass. Only live object key sets are retained. */
function validate(text: string): Token {
 let at = space(text, 0), root: Token | undefined
 const stack: ScanFrame[] = []
 const value = () => {
  const start = at, c = text[at]
  if (c === '{' || c === '[') {
   const kind = c === '{' ? 'object' : 'array'
   if (!root) root = {start, end: -1, kind}
   stack.push({kind, state: 'first', keys: kind === 'object' ? new Set() : undefined}); at++
  } else { const token = primitive(text, at); at = token.end; if (!root) root = token }
 }
 value()
 while (stack.length) {
  at = space(text, at)
  const frame = stack[stack.length - 1]!, c = text[at]
  if (frame.state === 'first' && c === (frame.kind === 'object' ? '}' : ']') || frame.state === 'after' && c === (frame.kind === 'object' ? '}' : ']')) {
   at++; stack.pop(); continue
  }
  if (frame.state === 'after') {
   if (c !== ',') return fail()
   at++; frame.state = frame.kind === 'object' ? 'key' : 'value'; continue
  }
  if (frame.kind === 'object' && (frame.state === 'first' || frame.state === 'key')) {
   if (c !== '"') return fail()
   const end = stringEnd(text, at), key = decode(text, at, end, 'raw')
   if (frame.keys!.has(key)) return fail()
   frame.keys!.add(key); at = end; frame.state = 'colon'; continue
  }
  if (frame.state === 'colon') { if (c !== ':') return fail(); at++; frame.state = 'value'; continue }
  frame.state = 'after'; value()
 }
 const end = at
 if (space(text, at) !== text.length) return fail()
 return {...root!, end}
}
/** Skip a previously validated token, with no descendant index or recursion. */
function tokenAt(text: string, start: number): Token {
 const c = text[start]
 if (c !== '{' && c !== '[') return primitive(text, start)
 let depth = 1, at = start + 1
 while (depth) {
  const ch = text[at]
  if (ch === '"') at = stringEnd(text, at)
  else { if (ch === '{' || ch === '[') depth++; else if (ch === '}' || ch === ']') depth--; at++ }
 }
 return {start, end: at, kind: c === '{' ? 'object' : 'array'}
}
interface Document {text: string; descriptor: InputSource}
interface CursorState {span: InputSpan; at: number; index: number; mode: 'object' | 'array'}
class Reader {
 readonly documents = new Map<string, Document>()
 private readonly spans = new WeakSet<InputSpan>()
 private readonly cursors = new WeakMap<InputCursor, CursorState>()
 span(source: string, token: Token): InputSpan { const span = Object.freeze({source, ...token}); this.spans.add(span); return span }
 add(id: string, text: string, parent?: InputSpan): InputSpan {
  const root = this.span(id, validate(text))
  this.documents.set(id, {text, descriptor: Object.freeze({id, root, ...(parent ? {parent} : {})})})
  return root
 }
 text = (source: string): string => { const doc = this.documents.get(source); return doc ? doc.text : fail('invalid-argument') }
 check(span: InputSpan): string { if (!span || !this.spans.has(span)) return fail('invalid-argument'); return this.text(span.source) }
 raw = (span: InputSpan): string => this.check(span).slice(span.start, span.end)
 string = (span: InputSpan, policy: 'scalar' | 'raw'): string => {
  const text = this.check(span)
  if (span.kind !== 'string' || policy !== 'scalar' && policy !== 'raw') return fail('invalid-argument')
  return decode(text, span.start, span.end, policy)
 }
 *children(span: InputSpan): Generator<InputMember | InputSpan> {
  const text = this.check(span)
  if (span.kind !== 'array' && span.kind !== 'object') return fail('invalid-argument')
  let at = space(text, span.start + 1)
  while (at < span.end - 1) {
   const next = this.child(span, at)
   yield next.entry
   at = next.at
  }
 }
 private child(span: InputSpan, at: number): {entry: InputMember | InputSpan; at: number} {
  const text = this.text(span.source)
  let key: InputSpan | undefined, name: string | undefined
  if (span.kind === 'object') {
   key = this.span(span.source, tokenAt(text, at)); name = this.string(key, 'raw')
   at = space(text, space(text, key.end) + 1)
  }
  const value = this.span(span.source, tokenAt(text, at))
  at = space(text, value.end)
  if (text[at] === ',') at = space(text, at + 1)
  return {entry: key ? Object.freeze({name: name!, key, value}) : value, at}
 }
 lookup = (span: InputSpan, pointer: string): InputPresence => {
  this.check(span)
  if (typeof pointer !== 'string' || pointer !== '' && pointer[0] !== '/') return fail('invalid-argument')
  if (pointer === '') return Object.freeze({kind: 'present', span})
  const parts = pointer.slice(1).split('/').map(part => { if (/~(?:[^01]|$)/.test(part)) return fail('invalid-argument'); return part.replace(/~1/g, '/').replace(/~0/g, '~') })
  let current = span
  for (const part of parts) {
   let found: InputSpan | undefined
   if (current.kind === 'object') {
    for (const entry of this.children(current)) { const member = entry as InputMember; if (member.name === part) { found = member.value; break } }
   } else if (current.kind === 'array' && /^(0|[1-9][0-9]*)$/.test(part)) {
    let index = 0
    for (const entry of this.children(current)) { if (String(index++) === part) { found = entry as InputSpan; break } }
   }
   if (!found) return missing
   current = found
  }
  return Object.freeze({kind: 'present', span: current})
 }
 window<T extends InputMember | InputSpan>(span: InputSpan, size: number, mode: 'object' | 'array', cursor?: InputCursor): InputWindow<T> {
  const text = this.check(span)
  if (span.kind !== mode || !Number.isSafeInteger(size) || size < 1) return fail('invalid-argument')
  let state: CursorState
  if (cursor === undefined) state = {span, at: space(text, span.start + 1), index: 0, mode}
  else {
   const held = cursor && this.cursors.get(cursor)
   if (!held || held.span.source !== span.source || held.span.start !== span.start || held.span.end !== span.end || held.mode !== mode) return fail('invalid-argument')
   state = {...held}
  }
  const entries: T[] = []
  while (state.at < span.end - 1 && entries.length < size) {
   const next = this.child(span, state.at); entries.push(next.entry as T); state.at = next.at; state.index++
  }
  const done = state.at >= span.end - 1
  let next: InputCursor | null = null
  if (!done) { next = Object.freeze({}) as InputCursor; this.cursors.set(next, state) }
  return Object.freeze({entries: Object.freeze(entries), next, done})
 }
 members = (span: InputSpan, size: number, cursor?: InputCursor): InputWindow<InputMember> => this.window(span, size, 'object', cursor)
 elements = (span: InputSpan, size: number, cursor?: InputCursor): InputWindow<InputSpan> => this.window(span, size, 'array', cursor)
 required(span: InputSpan, pointer: string, kind?: TokenKind): InputSpan {
  const found = this.lookup(span, pointer)
  if (found.kind !== 'present' || kind && found.span.kind !== kind) return fail()
  return found.span
 }
 str(span: InputSpan, pointer = ''): string { return this.string(this.required(span, pointer, 'string'), 'scalar') }
 closed(span: InputSpan, required: readonly string[], optional: readonly string[] = []): void {
  if (span.kind !== 'object') return fail()
  const seen = new Set<string>(), allowed = new Set([...required, ...optional])
  for (const entry of this.children(span)) {
   const member = entry as InputMember
   if (!allowed.has(member.name)) return fail()
   seen.add(member.name)
  }
  if (required.some(name => !seen.has(name))) return fail()
 }
 scalarTree(span: InputSpan): void {
  // Used only for the selected declaration domain, whose owner validates all escapes.
  const text = this.check(span)
  for (let at = span.start; at < span.end;) {
   if (text[at] === '"') { const end = stringEnd(text, at); decode(text, at, end, 'scalar'); at = end } else at++
  }
 }
 integer(span: InputSpan, pointer: string): string {
  const value = this.required(span, pointer, 'number'), raw = this.raw(value)
  if (!/^[1-9][0-9]*$/.test(raw)) return fail()
  return raw
 }
 cells(span: InputSpan, nullable: boolean): void {
  if (span.kind !== 'object') return fail()
  for (const entry of this.children(span)) {
   const member = entry as InputMember
   if (!this.string(member.key, 'scalar')) return fail()
   if (nullable && member.value.kind === 'null') continue
   this.str(member.value)
  }
 }
}
const digest = /^sha256:[a-f0-9]{64}$/
function same(actual: unknown, expected: unknown): void { if (actual !== expected) return fail('identity-mismatch') }
function contract(reader: Reader, span: InputSpan, expected: RetainedSelectionHeader['conversionContract'], roles: readonly string[]): void {
 reader.closed(span, ['contract', 'spec', 'profile', 'roles'])
 const spec = reader.required(span, '/spec', 'object')
 reader.closed(spec, ['version', 'revision', 'schemaSha256', 'corpusSha256'])
 for (const key of ['contract', 'profile'] as const) same(reader.str(span, '/' + key), expected[key])
 for (const key of ['version', 'revision', 'schemaSha256', 'corpusSha256'] as const) same(reader.str(spec, '/' + key), expected.spec[key])
 const roleArray = reader.required(span, '/roles', 'array')
 let index = 0
 for (const entry of reader.children(roleArray)) same(reader.str(entry as InputSpan), roles[index++])
 same(index, roles.length)
}
function timestamp(value: string): boolean {
 // Check the owner's exact RFC3339 seconds-UTC domain, without accepting Date normalization.
 if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(value)) return false
 const date = new Date(value)
 return !isNaN(date.getTime()) && date.toISOString() === value.slice(0, -1) + '.000Z'
}
function statuses(reader: Reader, span: InputSpan, frame: boolean): void {
 if (span.kind !== 'object') return fail()
 for (const entry of reader.children(span)) {
  const member = entry as InputMember
  if (!reader.string(member.key, 'scalar')) return fail()
  reader.required(member.value, '', 'object')
  if (frame) reader.closed(member.value, ['status'], ['valueJson'])
  if (!['supported', 'interpretation', 'missing', 'conflicting', 'inaccessible', 'stale'].includes(reader.str(member.value, '/status'))) return fail()
  const rawValue = reader.lookup(member.value, '/valueJson')
  if (frame && rawValue.kind === 'present') reader.str(rawValue.span)
 }
}
/** Inactive presentation reader. The client/controller must own SHA/EOF, readiness
 * and mounted-row lifetime. This is no receipt verifier, truth check or admission. */
export function readRetainedInput(input: ExactRetainedInput, expected: RetainedInputExpectation): RetainedInputView {
 const h = expected.header, row = expected.row, installed = DIALECT_RETAINED_COLUMN_DRAFT
 if (!Number.isSafeInteger(row.index) || row.index < 0 || !Number.isSafeInteger(h.rows) || row.index >= h.rows || h.origin !== 'asserted' && h.origin !== 'generated') return fail('invalid-argument')
 const c = h.conversionContract
 if (!c || !c.spec || Object.keys(c).length !== 3 || Object.keys(c.spec).length !== 4) return fail('identity-mismatch')
 for (const key of ['contract', 'profile'] as const) same(c[key], installed[key])
 for (const key of ['version', 'revision', 'schemaSha256', 'corpusSha256'] as const) same(c.spec[key], installed.spec[key])
 same(input.sourceRevision, h.revision); same(input.rowId, row.id); same(input.index, row.index); same(input.inputSha256, row.inputSha256)
 if (typeof input.text !== 'string') return fail()
 const r = new Reader(), root = r.add('input', input.text)
 r.closed(root, ['facts', 'source', 'preparation'], ['evidence'])
 const facts = r.required(root, '/facts', 'object'), source = r.required(root, '/source', 'object'), preparation = r.required(root, '/preparation', 'object')
 const generated = h.origin === 'generated', roles = ['document-validation', 'preview', 'preparation', ...(generated ? ['producer-carrier'] : [])]
 r.closed(source, generated ? ['conversionContract', 'mapping', 'mappingDigest', 'case', 'sources'] : ['conversionContract', 'mapping', 'mappingDigest', 'retainedCaseJson'])
 r.closed(preparation, ['conversionContract', 'version', 'verifiedAt', 'mappingDigest', 'verification', 'cites', 'lineage', 'outcomes'])
 contract(r, r.required(source, '/conversionContract'), c, roles); contract(r, r.required(preparation, '/conversionContract'), c, roles)
 same(r.raw(r.required(preparation, '/version', 'number')), '2')
 if (!timestamp(r.str(preparation, '/verifiedAt')) || !r.str(preparation, '/verification')) return fail()
 const mappingDigest = r.str(source, '/mappingDigest')
 if (!digest.test(mappingDigest)) return fail()
 same(r.str(preparation, '/mappingDigest'), mappingDigest)
 const sections: RetainedInputView['sections'] = {
  root, facts, evidence: r.lookup(root, '/evidence'), source, mapping: r.required(source, '/mapping', 'object'), preparation,
  cites: r.required(preparation, '/cites', 'array'), lineage: r.required(preparation, '/lineage', 'array'), outcomes: r.required(preparation, '/outcomes', 'array'),
  // Filled only after the complete custody chain has been checked below.
  custody: root, frame: root, originalValues: root,
 }
 let custody: InputSpan, frame: InputSpan, originalValues: InputSpan
 let extras: Partial<RetainedInputView['sections']>
 const provenance = expected.provenance, checked: string[] = []
 if (provenance) same(provenance.origin, h.origin)
 const bind = (name: string, actual: string, wanted: string | undefined) => { if (wanted !== undefined) { if (typeof wanted !== 'string' || !scalar(wanted)) return fail('invalid-argument'); same(actual, wanted); checked.push(name) } }
 if (!generated) {
  const token = r.required(source, '/retainedCaseJson', 'string')
  custody = r.add('case', r.str(token), token)
  r.required(custody, '', 'object')
  for (const name of ['values', 'prepared', 'origin', 'retained']) r.required(custody, '/' + name, 'object')
  const origin = r.required(custody, '/origin', 'object')
  same(r.str(origin, '/preparationId'), h.preparationId); same(r.str(origin, '/recordId'), row.id); same(r.str(origin, '/packDigest'), h.packDigest)
  const sourceId = r.str(origin, '/sourceId'), sourceDigest = r.str(origin, '/sourceDigest'), importedRow = r.integer(origin, '/row')
  if (!digest.test(sourceDigest)) return fail()
  r.str(origin, '/importedAt')
  const importVersion = r.integer(origin, '/importRecipeVersion'), recipeVersion = r.integer(origin, '/recipeVersion')
  r.required(origin, '/conversions')
  originalValues = r.required(origin, '/originalValues', 'object'); r.cells(originalValues, true)
  frame = r.required(custody, '/retained', 'object')
  r.closed(frame, ['contractVersion', 'origin', 'values', 'research'])
  same(r.str(frame, '/contractVersion'), '1'); same(r.str(frame, '/origin'), 'asserted'); r.required(frame, '/research', 'null')
  r.cells(r.required(frame, '/values', 'object'), true)
  if (provenance?.origin === 'asserted') {
   bind('sourceId', sourceId, provenance.sourceId); bind('sourceDigest', sourceDigest, provenance.sourceDigest); bind('row', importedRow, provenance.row)
   bind('importRecipeVersion', importVersion, provenance.importRecipeVersion); bind('recipeVersion', recipeVersion, provenance.recipeVersion)
  }
  extras = {origin}
 } else {
  const args = r.required(source, '/case', 'object'); r.closed(args, ['grant', 'digest']); r.str(args, '/grant'); r.str(args, '/digest')
  const sources = r.required(source, '/sources', 'object'); r.closed(sources, ['research'])
  const researchSource = r.required(sources, '/research', 'object'); r.closed(researchSource, ['retainedResponseJson'])
  const token = r.required(researchSource, '/retainedResponseJson', 'string')
  const response = r.add('response', r.str(token), token); r.closed(response, ['result', 'receipt', 'salts'])
  const carrier = r.required(response, '/result', 'object'); r.closed(carrier, ['contractVersion', 'originalSnapshotJson', 'originalSnapshotSha256', 'retained'])
  same(r.str(carrier, '/contractVersion'), '1')
  if (!digest.test(r.str(carrier, '/originalSnapshotSha256'))) return fail()
  const originalToken = r.required(carrier, '/originalSnapshotJson', 'string')
  const snapshot = r.add('snapshot', r.str(originalToken), originalToken); r.required(snapshot, '', 'object')
  for (const name of ['version', 'class', 'preparationId', 'record', 'recipeVersion', 'qualification', 'prepared', 'values', 'research', 'reviews']) r.required(snapshot, '/' + name)
  same(r.raw(r.required(snapshot, '/version', 'number')), '1'); same(r.str(snapshot, '/class'), 'generated'); same(r.str(snapshot, '/preparationId'), h.preparationId)
  const record = r.required(snapshot, '/record', 'object'), research = r.required(snapshot, '/research', 'object'), qualification = r.required(snapshot, '/qualification', 'object')
  same(r.str(record, '/id'), row.id); same(r.str(record, '/class'), 'generated'); same(r.str(qualification, '/digest'), h.packDigest)
  const qualified = r.required(qualification, '/conversionContract', 'object'); r.closed(qualified, ['contract', 'spec', 'profile'])
  const qspec = r.required(qualified, '/spec', 'object'); r.closed(qspec, ['version', 'revision', 'schemaSha256', 'corpusSha256'])
  for (const key of ['contract', 'profile'] as const) same(r.str(qualified, '/' + key), c[key])
  for (const key of ['version', 'revision', 'schemaSha256', 'corpusSha256'] as const) same(r.str(qspec, '/' + key), c.spec[key])
  const run = r.str(research, '/id'), originalVersion = r.integer(research, '/recipeVersion'), currentVersion = r.integer(snapshot, '/recipeVersion')
  same(r.str(record, '/research/runId'), run); same(r.integer(record, '/research/recipeVersion'), originalVersion)
  const originalDeclaration = r.required(research, '/recipe/fields', 'array'); r.scalarTree(originalDeclaration)
  for (const entry of r.children(originalDeclaration)) { const field = entry as InputSpan; if (!r.str(field, '/name')) return fail(); r.str(field, '/type') }
  originalValues = r.required(record, '/values', 'object'); r.cells(originalValues, false)
  statuses(r, r.required(record, '/research/entity/fields', 'object'), false)
  frame = r.required(carrier, '/retained', 'object'); r.closed(frame, ['contractVersion', 'origin', 'values', 'research'])
  same(r.str(frame, '/contractVersion'), '1'); same(r.str(frame, '/origin'), 'generated'); r.cells(r.required(frame, '/values', 'object'), false)
  const frameResearch = r.required(frame, '/research', 'object'); r.closed(frameResearch, ['recordRecipeRevision', 'declaration', 'fields'])
  const declaration = r.required(frameResearch, '/declaration', 'object'); r.closed(declaration, ['recipeRevision', 'fields'])
  same(r.str(frameResearch, '/recordRecipeRevision'), r.str(declaration, '/recipeRevision'))
  const declarations = r.required(declaration, '/fields', 'array')
  for (const entry of r.children(declarations)) { const field = entry as InputSpan; r.closed(field, ['name', 'fieldJson']); if (!r.str(field, '/name')) return fail(); r.str(field, '/fieldJson') }
  statuses(r, r.required(frameResearch, '/fields', 'object'), true)
  if (provenance?.origin === 'generated') { bind('runId', run, provenance.runId); bind('originalRecipeVersion', originalVersion, provenance.originalRecipeVersion); bind('recipeVersion', currentVersion, provenance.recipeVersion) }
  custody = response; extras = {response, carrier, snapshot, record, research, declaration, history: r.required(snapshot, '/reviews')}
 }
 return Object.freeze({
  inputText: input.text,
  identity: Object.freeze({sourceRevision: input.sourceRevision, rowId: input.rowId, index: input.index, inputSha256: input.inputSha256, origin: h.origin, provenanceChecked: Object.freeze(checked)}),
  sources: Object.freeze([...r.documents.values()].map(doc => doc.descriptor)),
  sections: Object.freeze({...sections, ...extras, custody, frame, originalValues}),
  sourceText: r.text, raw: r.raw, decodeString: r.string, lookup: r.lookup, members: r.members, elements: r.elements,
 })
}
