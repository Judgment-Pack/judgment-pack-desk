import { useState } from 'react'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import { afterEach, it, expect, vi } from 'vitest'
import { CaseEditor } from './CaseEditor'
import { newCase, type TestCase } from './model'
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
