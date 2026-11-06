import {cleanup, fireEvent, render, screen, within} from '@testing-library/react'
import {Tooltip} from 'radix-ui'
import {afterEach, describe, expect, it} from 'vitest'
import {DIALECT_RETAINED_COLUMN_DRAFT} from '@judgment-pack/harness/dialects'
import {InputDetails, ExactText, shortText, textWindow} from './InputDetails'
import {readRetainedInput, type RetainedInputView} from './input'
import type {RetainedInputExpectation} from './input'

afterEach(cleanup)
const q = (value: string) => JSON.stringify(value)
const pack = 'sha256:' + 'a'.repeat(64), digest = 'sha256:' + 'b'.repeat(64)
const tuple = JSON.stringify(DIALECT_RETAINED_COLUMN_DRAFT)
const selected = (generated: boolean) => tuple.slice(0, -1) + ',"roles":["document-validation","preview","preparation"' + (generated ? ',"producer-carrier"' : '') + ']}'
// Hand-authored documents; quoting embeds exact source text, not reader output.
function fixture(facts = '{"unsafe":9007199254740993,"negative":-0,"decimal":1.0,"exponent":1E+03,"null":null,"false":false,"empty":"","escaped":"\\ud800","html":"<script>globalThis.pwned=true</script>"}', generated = false, evidence = ''): RetainedInputView {
 const expected: RetainedInputExpectation = {header: {wireVersion: 1, viewId: 'rv_' + '1'.repeat(48), preparationId: 'prep', revision: 'saved', packDigest: pack, conversionContract: DIALECT_RETAINED_COLUMN_DRAFT, origin: generated ? 'generated' : 'asserted', rows: 1, maxInputBytes: 37748736, selectionSha256: '2'.repeat(64)}, row: {index: 0, id: 'row', inputBytes: 1, inputSha256: '3'.repeat(64), offset: 0}}
 const custody = '{"values":{},"prepared":{"future":false},"origin":{"preparationId":"prep","recordId":"row","sourceId":"original","sourceDigest":' + q(pack) + ',"row":1,"importedAt":"kept","importRecipeVersion":1,"recipeVersion":1,"packDigest":' + q(pack) + ',"originalValues":{"blank":"","missing":null},"conversions":{}},"retained":{"contractVersion":"1","origin":"asserted","values":{"blank":"","missing":null},"research":null},"unknownSibling":{"kept":true}}'
 const snapshot = '{"version":1,"class":"generated","preparationId":"prep","record":{"id":"row","class":"generated","values":{"blank":""},"research":{"runId":"run","recipeVersion":1,"entity":{"fields":{"a":{"status":"missing"}}}}},"recipeVersion":2,"qualification":{"digest":' + q(pack) + ',"conversionContract":' + tuple + '},"prepared":{},"values":{},"research":{"id":"run","recipeVersion":1,"recipe":{"fields":[{"name":"a","type":"text"}]},"unknownSibling":"research tail"},"reviews":[{"unknownSibling":"history tail"}],"future":"snapshot tail"}'
 const frame = '{"contractVersion":"1","origin":"generated","values":{"blank":""},"research":{"recordRecipeRevision":"v1","declaration":{"recipeRevision":"v1","fields":[{"name":"a","fieldJson":"null"}]},"fields":{"a":{"status":"missing"}}}}'
 const response = '{"result":{"contractVersion":"1","originalSnapshotJson":' + q(snapshot) + ',"originalSnapshotSha256":' + q(digest) + ',"retained":' + frame + '},"receipt":{"unknownSibling":"receipt tail"},"salts":{}}'
 const source = '{"conversionContract":' + selected(generated) + ',"mapping":{"future":"mapping tail"},"mappingDigest":' + q(digest) + (generated ? ',"case":{"grant":"grant","digest":"digest"},"sources":{"research":{"retainedResponseJson":' + q(response) + '}}' : ',"retainedCaseJson":' + q(custody)) + '}'
 const preparation = '{"conversionContract":' + selected(generated) + ',"version":2,"verifiedAt":"2026-10-01T00:00:00Z","mappingDigest":' + q(digest) + ',"verification":"kept","cites":[],"lineage":[],"outcomes":[]}'
 const text = '{"facts":' + facts + evidence + ',"source":' + source + ',"preparation":' + preparation + '}'
 return readRetainedInput({text, sourceRevision: 'saved', rowId: 'row', index: 0, inputSha256: expected.row.inputSha256}, expected)
}
function show(reader = fixture()) {return render(<Tooltip.Provider><InputDetails reader={reader}/></Tooltip.Provider>)}
function select(label: string, option: string) {fireEvent.click(screen.getByRole('combobox', {name: label})); fireEvent.click(screen.getByRole('option', {name: option}))}

describe('exact sequential text windows', () => {
 it('covers every original unit once at surrogate boundaries, with bounded DOM', () => {
  for (const text of ['', 'x'.repeat(4094) + '😀' + 'tail', 'x'.repeat(8189) + '😀' + 'tail', '😀'.repeat(6000), String.raw`"\ud800\udfff"`]) {
   let combined = '', page = 0, next = true, end = 0
   while (next) {
    const part = textWindow(text, page++)
    expect(part.from).toBe(end); expect(part.text.length).toBeLessThanOrEqual(4096)
    combined += part.text; end = part.to; next = part.next
   }
   expect(combined).toBe(text)
  }
 })
 it('traverses a bounded token range forward and back without rendering hidden tails', () => {
  const text = 'prefix' + 'x'.repeat(4094) + '😀TAIL' + 'suffix'
  render(<ExactText text={text} start={6} end={text.length - 6}/>)
  expect(screen.getByLabelText('Exact text').textContent).toBe('x'.repeat(4094) + '😀')
  fireEvent.click(screen.getByRole('button', {name: 'Next page'}))
  expect(screen.getByLabelText('Exact text').textContent).toBe('TAIL')
  fireEvent.click(screen.getByRole('button', {name: 'Previous page'}))
  expect(screen.getByLabelText('Exact text').textContent).toHaveLength(4096)
 })
})
it('preserves lexical values and escapes HTML with no data translation', () => {
 const {container} = show()
 for (const text of ['9007199254740993', '-0', '1.0', '1E+03', 'null', 'false', '""', String.raw`"\ud800"`, '"<script>globalThis.pwned=true</script>"']) expect(screen.getByText(text, {selector: 'code', exact: true})).toBeTruthy()
 expect(container.querySelector('script')).toBeNull()
 select('Section', 'Evidence availability'); expect(screen.getByText('Not supplied')).toBeTruthy()
 cleanup(); show(fixture('{}', false, ',"evidence":null')); select('Section', 'Evidence availability')
 expect(screen.getByLabelText('Exact text').textContent).toBe('null')
})
it('replaces 50-entry pages and reaches long names and token tails', () => {
 const longName = 'key'.repeat(2000), longValue = 'x'.repeat(9000) + 'TAIL'
 show(fixture('{' + Array.from({length: 51}, (_, i) => q('k' + i) + ':' + i).join(',') + ',' + q(longName) + ':' + q(longValue) + '}'))
 expect(screen.queryByText('k50')).toBeNull()
 fireEvent.click(screen.getByRole('button', {name: 'Next page'}))
 expect(screen.queryByText('k0')).toBeNull(); expect(screen.getByText('k50')).toBeTruthy()
 expect(screen.getByText(shortText(longName))).toBeTruthy()
 fireEvent.click(screen.getByRole('button', {name: 'Full name'}))
 expect(screen.getByLabelText('Exact text').textContent!.length).toBeLessThanOrEqual(4096)
 fireEvent.click(screen.getAllByRole('button', {name: 'View full value'})[1])
 const exact = screen.getByLabelText('Exact text').closest('details')!
 fireEvent.click(within(exact).getByRole('button', {name: 'Next page'})); fireEvent.click(within(exact).getByRole('button', {name: 'Next page'}))
 expect(screen.getByLabelText('Exact text').textContent).toContain('TAIL')
 fireEvent.click(screen.getByRole('button', {name: 'First page'})); expect(screen.getByText('k0')).toBeTruthy(); expect(screen.queryByText('k50')).toBeNull()
})
it('navigates arbitrary depth iteratively through parent and root without a growing DOM trail', () => {
 show(fixture('{"nest":'.repeat(90) + 'false' + '}'.repeat(90)))
 for (let i = 0; i < 89; i++) fireEvent.click(screen.getByRole('button', {name: 'Open'}))
 expect(screen.getAllByRole('navigation')).toHaveLength(1)
 expect(screen.getByRole('navigation').textContent!.length).toBeLessThan(140)
 for (let i = 0; i < 89; i++) fireEvent.click(screen.getByRole('button', {name: 'Parent'}))
 expect((screen.getByRole('button', {name: 'Parent'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button', {name: 'Open'})); fireEvent.click(screen.getByRole('button', {name: 'Root'}))
 expect((screen.getByRole('button', {name: 'Parent'}) as HTMLButtonElement).disabled).toBe(true)
})
it('offers every generated section and every exact source, including unknown siblings', async () => {
 const reader = fixture('{}', true)
 show(reader)
 fireEvent.click(screen.getByRole('combobox', {name: 'Section'}))
 for (const label of ['Facts', 'Evidence availability', 'Original values', 'Conversion rules', 'Input lineage', 'Citations', 'Outcomes', 'Source', 'Preparation', 'Custody', 'Frame', 'Input', 'Response', 'Carrier', 'Snapshot', 'Record', 'Research', 'Declaration', 'History']) expect(screen.getByRole('option', {name: label})).toBeTruthy()
 fireEvent.keyDown(screen.getByRole('listbox'), {key: 'Escape'})
 select('Section', 'Research'); expect(screen.getByText('"research tail"')).toBeTruthy()
 select('Section', 'History'); fireEvent.click(screen.getByRole('button', {name: 'Open'})); expect(screen.getByText('"history tail"')).toBeTruthy()
 fireEvent.click(screen.getByText('Technical details'))
 await screen.findByRole('combobox', {name: 'Source'})
 select('Source', 'Decoded snapshot JSON')
 expect(screen.getByLabelText('Exact text').textContent).toBe(reader.sourceText('snapshot'))
 select('Source', 'Decoded response JSON'); expect(screen.getByLabelText('Exact text').textContent).toContain('receipt tail')
 select('Source', 'Exact input JSON'); expect(screen.getByLabelText('Exact text').textContent).toBe(textWindow(reader.inputText, 0).text)
})
