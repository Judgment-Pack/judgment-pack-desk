/**
 * The builder, rendered: which control an operand gets, and what the form
 * refuses (nothing).
 *
 * The claim these cases hold is that the form **shapes and never refuses**. An
 * empty `in` list, an unquoted `5000` and an id nothing declares are all
 * writable, and the runtime is what names them — because a form with an opinion
 * the runtime does not share would refuse the intermediate states every edit
 * passes through.
 */
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { useMemo, useState } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import { ConditionBuilder } from './ConditionBuilder'
import {
  EditingContext,
  declaredIds,
  type EditingSession,
  type PendingText
} from './editingContext'
import { buffered, type Buffered } from './writes'

const PACK = readFileSync(
  join(import.meta.dirname, '..', '__fixtures__', 'full.pack.json'),
  'utf8'
)

afterEach(cleanup)

/** The builder over one buffer, with the buffer readable by the case. */
function Harness({ at, text, onText }: { at: string; text: string; onText: (next: string) => void }) {
  const [held, setHeld] = useState(text)
  // The unwritten operands live with the session in the route, so they live
  // with the session here: a harness that held them in the field would be
  // holding a different rule from the page.
  const [drafts, setDrafts] = useState<ReadonlyMap<string, PendingText>>(new Map())
  const read: Buffered = useMemo(() => buffered(held), [held])
  const session: EditingSession = {
    editing: true,
    buffer: read,
    write: (edit) => {
      const next = edit(read).text
      setHeld(next)
      onText(next)
    },
    diagnosticsAt: () => [],
    ids: declaredIds((read.index.value ?? {}) as never, ['/request/amount']),
    pending: drafts,
    hold: (pointer, draft) =>
      setDrafts((was) => {
        const next = new Map(was)
        if (draft === null) next.delete(pointer)
        else next.set(pointer, draft)
        return next
      })
  }
  return (
    <EditingContext.Provider value={session}>
      <ConditionBuilder at={at} />
    </EditingContext.Provider>
  )
}

function draw(at: string, text = PACK) {
  const bytes = { current: text }
  const view = render(<Harness at={at} text={text} onText={(next) => (bytes.current = next)} />)
  return { ...view, bytes }
}

const WHEN = '/rules/1/when'

describe('the operand control switches on the operator', () => {
  it('offers a decimal string for an ordered comparison', () => {
    draw(WHEN)
    const operand = screen.getByDisplayValue('5000') as HTMLInputElement
    expect(operand.tagName).toBe('INPUT')
    const field = document.getElementById(`${WHEN}/conditions/0/value`)!
    expect(within(field).getByText(/a decimal string/)).toBeTruthy()
  })

  it('writes an ordered comparison’s operand as a string, quotes and all', async () => {
    const { bytes } = draw(WHEN)
    fireEvent.change(screen.getByDisplayValue('5000'), { target: { value: '7500' } })
    await waitFor(() => expect(bytes.current).toContain('"value": "7500"'))
    expect(bytes.current).not.toContain('"value": 7500')
  })

  it('offers arbitrary JSON for an equality, as the bytes on disk', () => {
    // `/rules/1/when/conditions/1/conditions/0` is `equals "data-access"`. The
    // control shows the document's own bytes — quotes included — because
    // `"5"`, `5` and `5.0` are three different documents.
    draw(WHEN)
    expect(screen.getByDisplayValue('"data-access"')).toBeTruthy()
  })
})

describe('what the form will not refuse', () => {
  it('writes an empty in list and lets validate name it', async () => {
    const { bytes } = draw(WHEN)
    // Change the operator first, which keeps the author's operand.
    const operator = document.getElementById(`${WHEN}/conditions/0/operator`)!
    fireEvent.click(within(operator).getByRole('combobox'))
    fireEvent.click(await screen.findByRole('option', { name: 'in' }))
    await waitFor(() => expect(bytes.current).toContain('"operator": "in"'))
    // The operand is still the author's string, untouched by the change.
    expect(bytes.current).toContain('"value": "5000"')
    const operand = await screen.findByDisplayValue('"5000"')
    fireEvent.change(operand, { target: { value: '[]' } })
    // `minItems: 1` is the schema's; the form writes what was typed.
    await waitFor(() => expect(bytes.current).toContain('"value": []'))
  })

  it('writes an unquoted number where the schema asks for a string', async () => {
    const { bytes } = draw(WHEN)
    // Through the JSON operand of the equality, which takes any JSON at all.
    fireEvent.change(screen.getByDisplayValue('"data-access"'), { target: { value: '5000' } })
    await waitFor(() => expect(bytes.current).toContain('"value": 5000'))
  })

  it('holds text that is not JSON without writing it, and says so', async () => {
    const { bytes } = draw(WHEN)
    const before = bytes.current
    const operand = screen.getByDisplayValue('"data-access"')
    fireEvent.change(operand, { target: { value: '{"a":' } })
    await waitFor(() => expect(screen.getByText(/Not written yet/)).toBeTruthy())
    // Nothing unscannable reached the buffer — which is what keeps a keystroke
    // from withholding form mode mid-word.
    expect(bytes.current).toBe(before)
    expect(screen.getByDisplayValue('{"a":')).toBeTruthy()
  })
})

describe('the kinds', () => {
  it('recurses through the schema’s five and names each group', () => {
    draw(WHEN)
    expect(screen.getByRole('group', { name: `all of — ${WHEN}` })).toBeTruthy()
    expect(
      screen.getByRole('group', { name: `any of — ${WHEN}/conditions/1` })
    ).toBeTruthy()
    expect(
      screen.getByRole('group', { name: `not — ${WHEN}/conditions/1/conditions/1` })
    ).toBeTruthy()
    expect(
      screen.getByRole('group', {
        name: `literal — ${WHEN}/conditions/1/conditions/1/condition`
      })
    ).toBeTruthy()
  })

  it('offers an evidence-present node the requirements the document declares', () => {
    draw('/rules/0/when')
    const field = document.getElementById('/rules/0/when/evidenceRequirement')!
    expect(within(field).getByRole('combobox').textContent).toContain('screening-report')
  })

  it('prints a kind it has never seen and offers no controls for it', () => {
    const strange = PACK.replace(
      '"op": "evidence-present",\n        "evidenceRequirement": "screening-report"',
      '"op": "between",\n        "low": 1'
    )
    expect(strange).not.toBe(PACK)
    draw('/rules/0/when', strange)
    expect(screen.getByText(/This desk has no controls for this condition/)).toBeTruthy()
    expect(screen.queryByRole('group')).toBeNull()
  })
})

describe('the group controls', () => {
  it('adds, wraps and removes through the writer', async () => {
    const { bytes } = draw(WHEN)
    const group = screen.getByRole('group', { name: `all of — ${WHEN}` })
    fireEvent.click(within(group).getAllByRole('button', { name: 'Add' })[0]!)
    await waitFor(() => expect(bytes.current).toContain('"op": "literal"'))
    const child = screen.getByRole('group', { name: `fact — ${WHEN}/conditions/0` })
    fireEvent.click(within(child).getAllByRole('button', { name: 'Remove' })[0]!)
    await waitFor(() => expect(bytes.current).not.toContain('"/request/amount"'))
  })

  it('collapses a nested group and says how many conditions it holds', async () => {
    draw(WHEN)
    const nested = screen.getByRole('group', { name: `any of — ${WHEN}/conditions/1` })
    fireEvent.click(within(nested).getByRole('button', { name: 'Collapse' }))
    await waitFor(() => expect(screen.getByText('collapsed · 2 conditions')).toBeTruthy())
    expect(
      screen.queryByRole('group', { name: `not — ${WHEN}/conditions/1/conditions/1` })
    ).toBeNull()
  })
})
