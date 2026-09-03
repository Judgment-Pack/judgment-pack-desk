/**
 * The primitives, asserted by role, label and accessible name — never by class
 * name.
 *
 * Vitest runs with `css: false`, so a CSS module's exports are a stub rather
 * than the hashed names a build produces. An assertion on `styles.button`
 * would therefore be an assertion about the test runner, and it would pass
 * just as happily against a component whose stylesheet had been deleted. What
 * these cases hold is behaviour; `convention.test.ts` holds the styling rules,
 * by reading the source.
 */
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Button } from './Button'
import { Field } from './Field'
import { Input } from './Input'
import { Select } from './Select'

afterEach(cleanup)

describe('Button', () => {
  it('is a button carrying the name it was given', () => {
    render(<Button>Create pack</Button>)
    const button = screen.getByRole('button', { name: 'Create pack' })
    expect(button.tagName).toBe('BUTTON')
  })

  it('honours disabled', () => {
    const onClick = vi.fn()
    render(
      <Button disabled onClick={onClick}>
        Create pack
      </Button>
    )
    const button = screen.getByRole('button', { name: 'Create pack' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    fireEvent.click(button)
    expect(onClick).not.toHaveBeenCalled()
  })

  it('submits its form as a primary and does not as a secondary', () => {
    // A button in a form submits it unless told otherwise, so the default is
    // `button` and the one control that means to submit says `type="submit"`.
    // Without that, Cancel creates the pack.
    const submitted = vi.fn((event: { preventDefault: () => void }) => event.preventDefault())
    render(
      <form onSubmit={submitted}>
        <Button variant="secondary">Cancel</Button>
        <Button variant="primary" type="submit">
          Create pack
        </Button>
      </form>
    )
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(submitted).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Create pack' }))
    expect(submitted).toHaveBeenCalledTimes(1)
  })
})

describe('Field', () => {
  it('associates its label with the control it wires', () => {
    render(
      <Field label="Name">{(wiring) => <Input {...wiring} defaultValue="vendor" />}</Field>
    )
    const input = screen.getByLabelText('Name') as HTMLInputElement
    expect(input.value).toBe('vendor')
  })

  it('describes the control with the hint and the error, in that order', () => {
    render(
      <Field label="Name" hint="id: vendor-onboarding" error="That name is already used.">
        {(wiring) => <Input {...wiring} />}
      </Field>
    )
    const input = screen.getByLabelText('Name')
    const described = (input.getAttribute('aria-describedby') ?? '').split(' ')
    expect(described).toHaveLength(2)
    const text = described.map((id) => document.getElementById(id)?.textContent)
    expect(text).toEqual(['id: vendor-onboarding', 'That name is already used.'])
    expect(input.getAttribute('aria-invalid')).toBe('true')
  })

  it('names no id for a description it did not render', () => {
    // Pointing at an element that is not there makes a reader announce an
    // empty description, which is worse than saying less.
    render(<Field label="Name" hint="id: vendor">{(wiring) => <Input {...wiring} />}</Field>)
    const input = screen.getByLabelText('Name')
    const described = (input.getAttribute('aria-describedby') ?? '').split(' ')
    expect(described).toHaveLength(1)
    expect(document.getElementById(described[0]!)?.textContent).toBe('id: vendor')
    expect(input.getAttribute('aria-invalid')).toBeNull()
  })

  it('says nothing at all when there is nothing to say', () => {
    render(<Field label="Name">{(wiring) => <Input {...wiring} />}</Field>)
    expect(screen.getByLabelText('Name').getAttribute('aria-describedby')).toBeNull()
  })
})

describe('Select', () => {
  function Harness() {
    const [value, setValue] = useState('empty')
    return (
      <Field label="Template">
        {(wiring) => (
          <Select
            {...wiring}
            value={value}
            onValueChange={setValue}
            options={[
              { value: 'minimal-expense-approval', label: 'minimal-expense-approval' },
              { value: 'condition-branches', label: 'condition-branches' },
              { value: 'empty', label: 'Empty pack' }
            ]}
          />
        )}
      </Field>
    )
  }

  it('reports the current choice on its trigger, and hides its options until opened', () => {
    render(<Harness />)
    expect(screen.getByLabelText('Template').textContent).toContain('Empty pack')
    expect(screen.queryAllByRole('option')).toHaveLength(0)
  })

  it('opens on the trigger and exposes every option in the order given', async () => {
    render(<Harness />)
    fireEvent.click(screen.getByLabelText('Template'))
    const options = await screen.findAllByRole('option')
    expect(options.map((option) => option.textContent)).toEqual([
      'minimal-expense-approval',
      'condition-branches',
      'Empty pack'
    ])
  })

  it('selects with the keyboard and reports the new choice on the trigger', async () => {
    // Opening focuses the *selected* item, and an arrow key moves that focus
    // inside a `setTimeout` — so the move has to be awaited rather than
    // asserted on the next line. `testing/radixGround.test.tsx` records both.
    render(<Harness />)
    const trigger = screen.getByLabelText('Template')
    fireEvent.keyDown(trigger, { key: 'Enter' })
    await screen.findAllByRole('option')
    expect(document.activeElement?.textContent).toBe('Empty pack')

    fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' })
    await waitFor(() => expect(document.activeElement?.textContent).toBe('condition-branches'))

    fireEvent.keyDown(document.activeElement!, { key: 'Enter' })
    expect(trigger.textContent).toContain('condition-branches')
  })
})
