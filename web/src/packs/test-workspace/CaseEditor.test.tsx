import { useState } from 'react'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import { afterEach, it, expect, vi } from 'vitest'
import { CaseEditor } from './CaseEditor'
import { matrix, newCase, type TestCase } from './model'
afterEach(cleanup)
function Editor() {
  const [value, setValue] = useState<TestCase>(() => ({
    ...newCase(),
    name: 'Example',
    row: { id: 'c', facts: { amount: 3, items: [] } },
  }))
  return (
    <>
      <CaseEditor
        owner="fixture"
        document={{
          rules: [
            { condition: { op: 'fact', path: '/amount', operator: 'greaterThan', value: 0 } },
            { condition: { op: 'fact', path: '/items', operator: 'equals', value: [] } },
          ],
          outcomes: [{ id: 'accept', label: 'Accept' }],
        }}
        value={value}
        onChange={setValue}
        onSave={vi.fn()}
        onDiscard={vi.fn()}
        onAddSource={vi.fn()}
        onAI={vi.fn()}
        onRun={vi.fn()}
        busy={false}
        error=""
        dirty={true}
      />
      <output data-testid="stored">{JSON.stringify(value.row.facts)}</output>
    </>
  )
}
it('retains an incomplete number without switching it to unknown or silently saving the last number', () => {
  render(<Editor />)
  const field = screen.getByRole('textbox', { name: 'amount' })
  fireEvent.change(field, { target: { value: '-' } })
  expect((field as HTMLInputElement).value).toBe('-')
  expect((screen.getByRole('button', { name: 'Save case' }) as HTMLButtonElement).disabled).toBe(true)
  expect(JSON.parse(screen.getByTestId('stored').textContent!).amount).toBe(3)
  fireEvent.change(field, { target: { value: '-2.5' } })
  expect(JSON.parse(screen.getByTestId('stored').textContent!).amount).toBe(-2.5)
  expect((screen.getByRole('button', { name: 'Save case' }) as HTMLButtonElement).disabled).toBe(false)
})
it('does not clear an invalid JSON input when a separate valid field changes', () => {
  render(<Editor />)
  fireEvent.change(screen.getByRole('textbox', { name: 'items' }), { target: { value: '[' } })
  fireEvent.change(screen.getByRole('textbox', { name: 'amount' }), { target: { value: '4' } })
  expect((screen.getByRole('button', { name: 'Save case' }) as HTMLButtonElement).disabled).toBe(true)
  expect((screen.getByRole('textbox', { name: 'items' }) as HTMLTextAreaElement).value).toBe('[')
  fireEvent.change(screen.getByRole('textbox', { name: 'items' }), {
    target: { value: '[1,2]' },
  })
  expect((screen.getByRole('button', { name: 'Save case' }) as HTMLButtonElement).disabled).toBe(false)
})

function HandoffEditor({ assertion = {}, handoff = true }: {
  assertion?: Partial<TestCase['row']>, handoff?: boolean,
}) {
  const [value, setValue] = useState<TestCase>(() => ({
    ...newCase(), id: 'handoff', name: 'Review request',
    row: { id: 'handoff', facts: {}, expectedDisposition: {
      kind: 'unresolved', reasons: ['unknown'],
      handoff: handoff ? { state: 'requested', triggeredBy: ['unknown'] } : { state: 'none' },
    }, ...assertion },
  }))
  const [saved, setSaved] = useState<unknown>()
  return <>
    <CaseEditor owner="fixture" document={{ outcomes: [{ id: 'accept', label: 'Accept' }] }}
      value={value} onChange={setValue} onSave={() => setSaved(matrix([value]))}
      onDiscard={vi.fn()} onAddSource={vi.fn()} onAI={vi.fn()} onRun={vi.fn()}
      busy={false} error="" dirty />
    <output data-testid="saved">{JSON.stringify(saved)}</output>
  </>
}
async function choose(label: string, option: string) {
  fireEvent.click(screen.getByRole('combobox', { name: label }))
  fireEvent.click(await screen.findByRole('option', { name: option }))
}
function saveRow() {
  fireEvent.click(screen.getByRole('button', { name: 'Save case' }))
  const saved = JSON.parse(screen.getByTestId('saved').textContent!)
  expect(saved.matrixVersion).toBe('3')
  return saved.cases[0]
}
it.each([
  ['absent', {}],
  ['null', { expectedHandoffTarget: null }],
  ['named', { expectedHandoffTarget: { kind: 'human-role', name: 'Review team' } }],
])('round-trips the %s target through an expected-result edit that retains a handoff', async (_, assertion) => {
  render(<HandoffEditor assertion={assertion as Partial<TestCase['row']>} />)
  await choose('Expected result', 'Not applicable')
  fireEvent.change(screen.getByRole('textbox', { name: 'Case name' }), { target: { value: 'Edited review' } })
  const row = saveRow()
  expect(row.expectedDisposition.handoff).toEqual({ state: 'requested', triggeredBy: ['not-applicable'] })
  expect(Object.hasOwn(row, 'expectedHandoffTarget')).toBe(Object.hasOwn(assertion, 'expectedHandoffTarget'))
  expect(row.expectedHandoffTarget).toEqual((assertion as Partial<TestCase['row']>).expectedHandoffTarget)
})
it('writes an exact named target, requiring both fields before save', async () => {
  render(<HandoffEditor />)
  await choose('Expected handoff target', 'Expect a named target')
  expect((screen.getByRole('button', { name: 'Save case' }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(screen.getByRole('textbox', { name: 'Target kind' }), { target: { value: 'queue' } })
  expect((screen.getByRole('button', { name: 'Save case' }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(screen.getByRole('textbox', { name: 'Target name' }), { target: { value: ' Review Queue ' } })
  expect(saveRow().expectedHandoffTarget).toEqual({ kind: 'queue', name: ' Review Queue ' })
})
it('distinguishes explicit no-target from no assertion', async () => {
  render(<HandoffEditor />)
  await choose('Expected handoff target', 'Expect no target')
  expect(saveRow().expectedHandoffTarget).toBeNull()
  await choose('Expected handoff target', 'Do not check the target')
  expect(Object.hasOwn(saveRow(), 'expectedHandoffTarget')).toBe(false)
})
it('explains removing a named target when the handoff is removed', async () => {
  render(<HandoffEditor assertion={{ expectedHandoffTarget: { kind: 'queue', name: 'Review' } }} />)
  await choose('Expected handoff', 'No handoff')
  expect(screen.getByText(/assertion was removed/).getAttribute('role')).toBe('status')
  expect(Object.hasOwn(saveRow(), 'expectedHandoffTarget')).toBe(false)
})
it.each(['Expected refusal', 'Exploratory · no expectation'])('explains removal when selecting %s', async (result) => {
  render(<HandoffEditor assertion={{ expectedHandoffTarget: null }} />)
  await choose('Expected result', result)
  expect(screen.getByText(/assertion was removed/).getAttribute('role')).toBe('status')
  // Refusals remain executable; exploratory rows are intentionally not exported.
  if (result === 'Expected refusal') expect(Object.hasOwn(saveRow(), 'expectedHandoffTarget')).toBe(false)
})
it('preserves imported null when no handoff is expected', async () => {
  render(<HandoffEditor handoff={false} assertion={{ expectedHandoffTarget: null }} />)
  await choose('Expected result', 'Accept')
  expect(saveRow().expectedHandoffTarget).toBeNull()
})
