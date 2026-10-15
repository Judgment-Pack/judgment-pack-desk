import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { createHash, webcrypto } from 'node:crypto'
import { afterAll, beforeAll, expect, it, vi } from 'vitest'
import { canonicalize, canonicalPreparation, dialectDigest, digestBytes, parseJSON, preparationDigest } from './canon'
import { canonicalText as oldCanonicalText } from '../research/verify/canon'

beforeAll(() => vi.stubGlobal('crypto', webcrypto))
afterAll(() => vi.unstubAllGlobals())
const directory = resolve('src/dialect/__fixtures__/dialect-vectors')
const read = (name: string) => readFileSync(`${directory}/${name}`, 'utf8')
const manifest = JSON.parse(read('manifest.json')) as { vectors: { name: string; files: Record<string, string> }[] }
const sha = (text: string) => 'sha256:' + createHash('sha256').update(text).digest('hex')

it('pins the vendored manifest to the specification at a550d41', () => {
  // conformance/dialect/manifest.json, runnerFiles["vectors/manifest.json"].
  expect(sha(read('manifest.json'))).toBe('sha256:db6487fadcb9f6161c243f1aae18dc4627614c5c6093d988e2f07aace12dfbeb')
  expect(manifest.vectors).toHaveLength(10)
  expect(readdirSync(directory, {withFileTypes:true}).filter(entry => entry.isDirectory()).map(entry => entry.name).sort())
    .toEqual(manifest.vectors.map(vector => vector.name).sort())
})

it.each(manifest.vectors)('$name: retains the upstream bytes and round-trips both canonical forms', async ({name, files}) => {
  for (const file of ['mapping.json', 'preparation.json', 'preparation.sha256']) expect(sha(read(`${name}/${file}`))).toBe(files[file])
  const mappingText = read(`${name}/mapping.json`), preparationText = read(`${name}/preparation.json`)
  const mapping = parseJSON(mappingText), preparation = parseJSON(preparationText)
  // Upstream mapping.json uses Go struct order, not §1 order. Its *canonical*
  // bytes must agree with the existing gateway writer, with no member added.
  const canonicalMapping = canonicalize(mapping)
  expect(canonicalMapping).toBe(new TextDecoder().decode(oldCanonicalText(mappingText)))
  expect(canonicalize(parseJSON(canonicalMapping))).toBe(canonicalMapping)
  expect(await dialectDigest(mapping)).toBe(JSON.parse(preparationText).mappingDigest)
  expect(canonicalPreparation(preparation)).toBe(preparationText)
  expect(await preparationDigest(preparation)).toBe(read(`${name}/preparation.sha256`))
})

it.each([
  ['1e2', /no fraction or exponent/], ['1.0', /no fraction or exponent/],
  ['9007199254740992', /2\^53/], ['-9007199254740992', /2\^53/],
  ['{"x":1,"x":2}', /duplicate member/], ['{"x":1,"\\u0078":2}', /duplicate member/],
  ['"\\ud800"', /Unicode/], ['"\udfff"', /Unicode/],
  ['{}{}', /one RFC 8259/], ['[1,]', /one RFC 8259/], ['{"x":true,}', /one RFC 8259/],
  ['NaN', /one RFC 8259/], ['\ufeff{}', /one RFC 8259/],
] as const)('refuses %s before its spelling can be lost', (text, rule) => {
  expect(() => parseJSON(text)).toThrow(rule)
})

it('distinguishes §1 and §9.5 without losing safe integer literals, nesting, or string escapes', () => {
  const value = parseJSON(' {"𐀀": [true, false, null, -0, 9007199254740991, -9007199254740991], "\ue000": "A&B <x> café ☃/\u007f\u2028\u2029\\b\\t\\n\\f\\r\\u0000\\u001f\\\"\\\\"} ')
  const text = canonicalize(value)
  expect(text.startsWith('{"\ue000":')).toBe(true) // Code points, not UTF-16 units.
  expect(text).toContain('[true,false,null,0,9007199254740991,-9007199254740991]')
  expect(text).toContain('A&B <x> café ☃/\u007f\u2028\u2029\\b\\t\\n\\f\\r\\u0000\\u001f\\\"\\\\')
  const preparation = canonicalPreparation(value)
  expect(preparation).toContain('A\\u0026B \\u003cx\\u003e café ☃/\u007f\\u2028\\u2029')
  expect(preparation).toContain('[true,false,null,-0,9007199254740991,-9007199254740991]')
  expect(canonicalPreparation(parseJSON(preparation))).toBe(preparation)
})

it('counts containers from the root and accepts 64, refusing 65', () => {
  const nested = (count: number) => '['.repeat(count) + '0' + ']'.repeat(count)
  expect(canonicalize(parseJSON(nested(64)))).toBe(nested(64))
  expect(() => parseJSON(nested(65))).toThrow(/§12/)
  expect(() => canonicalize(JSON.parse(nested(65)))).toThrow(/§12/)
})

it('does not interpret prototype names or invoke object serialization hooks', () => {
  const text = '{"__proto__":{"polluted":true},"constructor":null}'
  expect(canonicalize(parseJSON(text))).toBe(text)
  expect(({} as Record<string, unknown>).polluted).toBeUndefined()
  for (const value of [undefined, NaN, Infinity, 1.1, 9007199254740992, 1n, '\ud800', new Date(), [,], {x:undefined}, {toJSON(){ return {} }}]) {
    expect(() => canonicalize(value)).toThrow(/§1/)
    expect(() => canonicalPreparation(value)).toThrow(/§1/)
  }
  const getter = vi.fn(() => 1)
  expect(() => canonicalize(Object.defineProperty({}, 'x', {enumerable:true, get:getter}))).toThrow(/accessor/)
  expect(getter).not.toHaveBeenCalled()
})

it('names SHA-256 digests with 64 lowercase hexadecimal characters', async () => {
  expect(await digestBytes(new Uint8Array())).toBe('sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
})
