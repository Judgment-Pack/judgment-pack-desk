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
import { useEffect, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Alert } from './Alert'
import { AlertPanel } from './AlertPanel'
import { Button } from './Button'
import { CodeArea } from './CodeArea'
import { Dialog } from './Dialog'
import { Field } from './Field'
import { Input } from './Input'
import { Select } from './Select'
import { SegmentedControl } from './SegmentedControl'
import { SuggestInput } from './SuggestInput'

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

  /**
   * The regression this primitive exists to hold.
   *
   * Inside a form, Radix mirrors the value into a hidden native `<select>` and
   * dispatches `change` on it whenever the value changes. That select's options
   * are the items that have registered, and items mount only while the list is
   * open — so a controlled value that changes while it is closed is set against
   * a select with no such option, lands as `""`, and is reported straight back
   * through `onValueChange`. Without the guard the caller stores `""`, the
   * choice is lost and the trigger goes blank; the create dialog hit exactly
   * this the moment its default moved to the runtime's first example.
   */
  it('keeps a value that changes while closed, inside a form', async () => {
    const seen: string[] = []
    function LateDefault() {
      const [options, setOptions] = useState([{ value: 'empty', label: 'Empty pack' }])
      const [choice, setChoice] = useState<string | undefined>(undefined)
      useEffect(() => {
        setOptions([
          { value: 'minimal-expense-approval', label: 'minimal-expense-approval' },
          { value: 'empty', label: 'Empty pack' }
        ])
      }, [])
      const value = choice ?? options[0]!.value
      return (
        <form>
          <Field label="Template">
            {(wiring) => (
              <Select
                {...wiring}
                value={value}
                onValueChange={(next) => {
                  seen.push(next)
                  setChoice(next)
                }}
                options={options}
              />
            )}
          </Field>
        </form>
      )
    }
    render(<LateDefault />)
    await waitFor(() =>
      expect(screen.getByLabelText('Template').textContent).toContain('minimal-expense-approval')
    )
    // And it stays: nothing reported a choice nobody made.
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(screen.getByLabelText('Template').textContent).toContain('minimal-expense-approval')
    expect(seen).toEqual([])
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

describe('Dialog', () => {
  it('is a dialog named by its title', () => {
    render(
      <Dialog open onOpenChange={() => {}} title="Create a pack">
        <p>fields</p>
      </Dialog>
    )
    expect(screen.getByRole('dialog', { name: 'Create a pack' })).toBeTruthy()
  })

  it('describes itself with the description it was given', () => {
    render(
      <Dialog open onOpenChange={() => {}} title="Create a pack" description="Three questions.">
        <p>fields</p>
      </Dialog>
    )
    const described = screen.getByRole('dialog').getAttribute('aria-describedby')
    expect(described).toBeTruthy()
    expect(document.getElementById(described!)?.textContent).toBe('Three questions.')
  })

  it('names no description it did not render', () => {
    // The mirror of the Field case above, and the same rule: this used to
    // render an empty `<Description />` "to mean there is none", which left
    // every dialog with no description pointing a reader at an empty
    // paragraph. Radix omits the attribute when no Description was rendered,
    // which is what "there is none" actually looks like on the wire.
    render(
      <Dialog open onOpenChange={() => {}} title="Create a pack">
        <p>fields</p>
      </Dialog>
    )
    expect(screen.getByRole('dialog').getAttribute('aria-describedby')).toBeNull()
  })
})

describe('Alert', () => {
  it('announces itself, and keeps the reason its own element', () => {
    // Two statements: what the desk says, and what the failure itself said.
    // Keeping them apart is what lets the second be read on its own rather
    // than as a tail of the first.
    render(<Alert reason="could not stage the write">The pack could not be created.</Alert>)
    const alert = screen.getByRole('alert')
    expect(alert.textContent).toBe('The pack could not be created. could not stage the write')
    expect(screen.getByText('could not stage the write')).not.toBe(alert)
  })

  it('renders no empty element for a reason it was not given', () => {
    render(<Alert>The pack could not be created.</Alert>)
    expect(screen.getByRole('alert').textContent).toBe('The pack could not be created.')
    expect(screen.getByRole('alert').querySelector('span')).toBeNull()
  })
})

describe('SegmentedControl', () => {
  it('never empties when the current choice is pressed again', () => {
    // Radix reports a deselect as `""`, which would leave the toolbar showing
    // neither Edit nor Read. There is no such mode.
    const seen: string[] = []
    render(
      <SegmentedControl
        label="Mode"
        value="edit"
        onValueChange={(next) => seen.push(next)}
        segments={[
          { value: 'edit', label: 'Edit' },
          { value: 'read', label: 'Read' }
        ]}
      />
    )
    fireEvent.click(screen.getByRole('radio', { name: 'Edit' }))
    expect(seen).toEqual([])
    fireEvent.click(screen.getByRole('radio', { name: 'Read' }))
    expect(seen).toEqual(['read'])
  })

  it('carries the group name and says which segment is on', () => {
    render(
      <SegmentedControl
        label="Shape"
        value="json"
        onValueChange={() => {}}
        segments={[
          { value: 'form', label: 'Form' },
          { value: 'json', label: 'JSON' }
        ]}
      />
    )
    // A single-value ToggleGroup is a radiogroup, and each segment a radio.
    expect(screen.getByRole('radiogroup', { name: 'Shape' })).toBeTruthy()
    expect(screen.getByRole('radio', { name: 'JSON' }).getAttribute('data-state')).toBe('on')
  })
})

describe('CodeArea', () => {
  it('numbers the lines the text has, hidden from a reader that has the text', () => {
    render(<CodeArea aria-label="Document" value={'{\n  "a": 1\n}'} onChange={() => {}} />)
    const gutter = screen.getByTestId('code-gutter')
    expect(gutter.getAttribute('aria-hidden')).toBe('true')
    expect(gutter.textContent).toBe('123')
  })

  it('does not open a line for a trailing newline', () => {
    // `"a\n"` is one line. Numbering the empty tail 2 puts a number beside
    // nothing, and a parse error reported at line 2 would then point at it.
    render(<CodeArea aria-label="Document" value={'a\n'} onChange={() => {}} />)
    expect(screen.getByTestId('code-gutter').textContent).toBe('1')
  })

  it('scrolls the gutter with the text', () => {
    render(<CodeArea aria-label="Document" value={'a\nb\nc'} onChange={() => {}} />)
    const area = screen.getByLabelText('Document')
    const gutter = screen.getByTestId('code-gutter')
    area.scrollTop = 40
    fireEvent.scroll(area)
    expect(gutter.scrollTop).toBe(40)
  })
})

describe('SuggestInput', () => {
  it('keeps a value that is in no list it offered', () => {
    // The suggestions are what this project already consults, and a rule about
    // a fact nothing consults yet is the ordinary case for a new rule.
    const seen: string[] = []
    render(
      <SuggestInput
        aria-label="Fact path"
        suggestions={['/request/amount']}
        value=""
        onChange={(event) => seen.push(event.target.value)}
      />
    )
    fireEvent.change(screen.getByLabelText('Fact path'), { target: { value: '/vendor/tier' } })
    expect(seen).toEqual(['/vendor/tier'])
  })

  it('offers the suggestions through a list the input names', () => {
    const { container } = render(
      <SuggestInput aria-label="Fact path" suggestions={['/request/amount', '/request/type']} value="" onChange={() => {}} />
    )
    const list = screen.getByLabelText('Fact path').getAttribute('list')
    expect(list).toBeTruthy()
    const datalist = container.querySelector(`#${CSS.escape(list!)}`)
    expect([...datalist!.querySelectorAll('option')].map((option) => option.getAttribute('value'))).toEqual([
      '/request/amount',
      '/request/type'
    ])
  })
})

describe('AlertPanel', () => {
  it('is one block-level announcement carrying its detail behind a disclosure', () => {
    render(
      <AlertPanel
        heading="This file changed since you opened it. Nothing was written."
        detailLabel="digests"
        detail={<span>sha256 aaa</span>}
        actions={<Button>Reload</Button>}
      >
        Your draft is intact.
      </AlertPanel>
    )
    const alert = screen.getByRole('alert')
    expect(alert.tagName).toBe('DIV')
    expect(alert.textContent).toContain('This file changed since you opened it.')
    // A `<details>` and a button inside a `<p>` is markup the browser
    // re-parents, which is why this is not `Alert`.
    expect(alert.querySelector('details')).toBeTruthy()
    expect(alert.querySelector('button')).toBeTruthy()
    expect(screen.getByText('digests')).toBeTruthy()
  })

  it('renders no disclosure where it was given no detail', () => {
    render(<AlertPanel heading="Not saved.">Nothing was written.</AlertPanel>)
    expect(screen.getByRole('alert').querySelector('details')).toBeNull()
  })
})
