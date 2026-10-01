import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { Evaluation, UnknownCause } from '../mcp/types'
import { EvaluationView } from './EvaluationView'
import mismatch from './__fixtures__/evaluation-findings/type-mismatch.result.json'
import unknownFacts from './__fixtures__/evaluation-findings/unknown-facts.result.json'
import unknownEvidence from './__fixtures__/evaluation-findings/unknown-evidence.result.json'
import unmet from './__fixtures__/evaluation-findings/unmet-evidence.result.json'

afterEach(cleanup)
const payload = (value: unknown) => value as Evaluation
const warning = 'Fact /flag is text; the comparison uses true or false. These JSON types cannot be equal.'

it('shows the Runtime type warning beside an ordinary outcome and in its trace without changing the answer', () => {
 const {container} = render(<EvaluationView payload={payload(mismatch)} />)
 expect(screen.getByRole('heading', {name: 'Input findings'})).toBeTruthy()
 expect(screen.getAllByText(warning)).toHaveLength(2)
 expect(within(container.querySelector('.trace-entry') as HTMLElement).getByText(warning).className).toContain('note-warn')
 expect(container.querySelector('.disposition-main')?.textContent).toContain('hold')
})
it('shows each Runtime unknown fact cause beside the unresolved result and in its trace', () => {
 const {container} = render(<EvaluationView payload={payload(unknownFacts)} />)
 for (const text of ['No value was supplied for fact /missing.', 'Fact /amount is number; ordered comparisons require a decimal string.']) {
  expect(screen.getAllByText(text)).toHaveLength(2)
  expect(within(container.querySelector('.trace-entry') as HTMLElement).getByText(text)).toBeTruthy()
 }
 expect(container.querySelector('.disposition-main')?.textContent).toContain('unresolved')
})
it('names the Runtime evidence requirement with unknown availability', () => {
 render(<EvaluationView payload={payload(unknownEvidence)} />)
 expect(screen.getAllByText('Evidence approval has unknown availability.')).toHaveLength(2)
})
it('places each unmet Runtime requirement beside its own absent or unknown state, even without a trace', () => {
 const {container} = render(<EvaluationView payload={payload(unmet)} />)
 expect(screen.getByRole('heading', {name: 'Unmet required evidence'})).toBeTruthy()
 const rows = [...container.querySelectorAll('li')]
 expect(rows.map(row => row.textContent)).toEqual(['approval — Required evidence is absent.', 'receipt — Required evidence availability is unknown.'])
 expect(screen.getByText(/carries no trace entries/)).toBeTruthy()
})
it('keeps type mismatch warnings accurate for not-equals and multiple operand types, including root and collection pointers', () => {
 const entry = {stage: 'rule', id: 'different-types', condition: 'true', typeMismatches: [{path: '', within: '', operator: 'not-equals', factType: 'number', operandTypes: ['string', 'boolean']}]}
 const {container} = render(<EvaluationView payload={{...payload(mismatch), trace: [entry]}} />)
 expect(screen.getAllByText('Fact the whole document is number; the comparison uses text, true or false. These JSON types cannot be equal. Within collection the whole document.')).toHaveLength(2)
 expect(container.querySelector('.trace-entry .verdict')?.textContent).toBe('Met')
})
it.each([
 [{path: '/items', cause: 'not-an-array', factType: 'object'}, 'Fact /items is object; this condition requires a list.'],
 [{path: '', within: '/items', cause: 'absent'}, 'No value was supplied for fact the whole document. Within collection /items.'],
 [{cause: 'unsupported'}, 'The evaluator does not support this condition.'],
 [{cause: 'future-cause'}, 'Unknown cause: future-cause.'],
] satisfies [UnknownCause, string][])('renders only the reported unknown cause %j', (cause, text) => {
 render(<EvaluationView payload={{...payload(unknownFacts), trace: [{stage: 'rule', condition: 'unknown', unknownCauses: [cause]}]}} />)
 expect(screen.getAllByText(text)).toHaveLength(2)
})
it('keeps ignored unknown conditions in the trace without presenting them as an unresolved result', () => {
 render(<EvaluationView payload={{...payload(mismatch), trace: payload(unknownFacts).trace}} />)
 expect(screen.queryByRole('heading', {name: 'Input findings'})).toBeNull()
 expect(screen.getAllByText('No value was supplied for fact /missing.')).toHaveLength(1)
})
it('does not invent diagnostics when Runtime omits them', () => {
 render(<EvaluationView payload={{...payload(mismatch), trace: [{stage: 'rule', condition: 'false'}]}} />)
 expect(screen.queryByRole('heading', {name: 'Input findings'})).toBeNull()
 expect(screen.queryByRole('heading', {name: 'Unmet required evidence'})).toBeNull()
 expect(screen.queryByText(/JSON types/)).toBeNull()
})
