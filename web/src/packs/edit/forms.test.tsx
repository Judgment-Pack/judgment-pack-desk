/**
 * What a form does about a member that is **not there**, and what it says about
 * text it has not written.
 *
 * Both are the same failure seen twice: a control that takes a gesture and
 * moves no bytes, with nothing on screen saying so. A field whose container is
 * absent has no span to splice into — `writes.place` bails rather than
 * inventing the object around it — and an operand holding text that is not
 * JSON is not in the buffer a save would send. Neither is refused and neither
 * is repaired; both are stated.
 */
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { chassis, drawPack, forgetSlot, served, PACK_DIGEST } from './editHarness'

const EDIT = '/packs/vendor-onboarding?edit=1'

/**
 * A draft with the two required objects left out, and the two condition shapes
 * these cases are about. It is what an author opens the editor to fix.
 */
const DRAFT = `${JSON.stringify(
  {
    specVersion: '0.2.0-draft',
    id: 'https://example.invalid/judgment-packs/draft',
    version: '0.1.0',
    title: 'A draft',
    decision: { intent: 'Decide it.', question: 'Does it proceed?' },
    sources: [{ id: 'handbook', title: 'The handbook' }],
    outcomes: [{ id: 'proceed', label: 'Proceed' }],
    rules: [
      {
        id: 'unless-held',
        description: 'Proceed unless it is held.',
        when: { op: 'not', condition: { op: 'literal', value: true } },
        outcome: 'proceed',
        onUnknown: 'escalate'
      },
      {
        id: 'when-tagged',
        description: 'Proceed when it is tagged.',
        when: { op: 'fact', path: '/request/tag', operator: 'equals', value: 'green' },
        outcome: 'proceed',
        onUnknown: 'escalate'
      }
    ],
    escalation: { triggers: ['no-match'] }
  },
  null,
  2
)}\n`

afterEach(() => {
  cleanup()
  forgetSlot()
  vi.unstubAllGlobals()
})

async function draft(path = EDIT): Promise<void> {
  chassis({ content: DRAFT, sha256: PACK_DIGEST })
  drawPack(served(DRAFT), { path })
  await screen.findByRole('navigation', { name: 'Members' })
}

/** The raw bytes, once the form has been left for the JSON view. */
async function bytes(): Promise<string> {
  fireEvent.click(screen.getByRole('radio', { name: 'JSON' }))
  const raw = (await screen.findByLabelText("The document's bytes")) as HTMLTextAreaElement
  return raw.value
}

describe('a member the document does not carry', () => {
  it('offers to write a source’s locator rather than drawing fields that do nothing', async () => {
    await draft()
    const group = document.getElementById('/sources/0/locator')!
    expect(within(group).getByText('not declared')).toBeTruthy()
    // Not drawn: `locator.kind` and `locator.value` have nothing to splice
    // into while the object is absent, so a control for either would take a
    // keystroke and move no bytes.
    expect(screen.queryByLabelText('locator kind')).toBeNull()
    expect(screen.queryByLabelText('locator')).toBeNull()

    fireEvent.click(within(group).getByRole('button', { name: 'Write a locator' }))
    const value = await screen.findByLabelText('locator')
    fireEvent.change(value, { target: { value: 'https://example.invalid/handbook' } })
    expect(await bytes()).toContain('"value": "https://example.invalid/handbook"')
  })

  it('offers to write the escalation’s target the same way', async () => {
    await draft()
    const group = document.getElementById('/escalation/target')!
    expect(within(group).getByText('not declared')).toBeTruthy()
    expect(screen.queryByLabelText('target name')).toBeNull()
    fireEvent.click(within(group).getByRole('button', { name: 'Write a target' }))
    const name = await screen.findByLabelText('target name')
    fireEvent.change(name, { target: { value: 'risk-desk' } })
    const written = await bytes()
    expect(written).toContain('"name": "risk-desk"')
    // The schema's own required members, and the enum's first word for the one
    // that has to say something.
    expect(written).toContain('"kind": "human-role"')
  })

  it('offers a condition back after a not’s child is removed', async () => {
    // Taking the child out leaves a required member absent. Drawn as an
    // unknown *kind* — an empty `<code>` above "this desk has no controls for
    // this condition" — it describes something that is not what happened, and
    // the form offers no way back.
    await draft()
    const child = document.getElementById('/rules/0/when/condition')!
    fireEvent.click(within(child).getByRole('button', { name: 'Remove' }))
    const absent = await waitFor(() => {
      const found = document.getElementById('/rules/0/when/condition')
      expect(found).toBeTruthy()
      return found!
    })
    expect(within(absent).getByText('not declared')).toBeTruthy()
    expect(within(absent).queryByText(/no controls for this condition/)).toBeNull()
    fireEvent.click(within(absent).getByRole('button', { name: 'Write a condition' }))
    expect(await bytes()).toContain('"condition"')
  })
})

describe('an operand holding text that is not JSON', () => {
  const OPERAND = '/rules/1/when/value'

  it('says so, counts it in the toolbar, and keeps it across the mode toggle', async () => {
    await draft()
    const operand = within(document.getElementById(OPERAND)!).getByDisplayValue('"green"')
    fireEvent.change(operand, { target: { value: '{"shade"' } })
    await waitFor(() => expect(screen.getByText(/Not written yet/)).toBeTruthy())
    // Beside the unsaved dot, because a save sends the buffer and the buffer
    // does not have this in it.
    expect(screen.getByText('1 field is not written yet')).toBeTruthy()

    // The two ways out of a form both unmount the field. Held in the field it
    // went with them, silently.
    fireEvent.click(screen.getByRole('radio', { name: 'JSON' }))
    await screen.findByLabelText("The document's bytes")
    fireEvent.click(screen.getByRole('radio', { name: 'Form' }))
    await waitFor(() =>
      expect(
        within(document.getElementById(OPERAND)!).getByDisplayValue('{"shade"')
      ).toBeTruthy()
    )
    expect(screen.getByText('1 field is not written yet')).toBeTruthy()
  })

  it('writes it and stops counting it the moment it parses', async () => {
    await draft()
    const operand = within(document.getElementById(OPERAND)!).getByDisplayValue('"green"')
    fireEvent.change(operand, { target: { value: '{"shade"' } })
    await waitFor(() => expect(screen.getByText('1 field is not written yet')).toBeTruthy())
    fireEvent.change(screen.getByDisplayValue('{"shade"'), {
      target: { value: '{"shade": "green"}' }
    })
    await waitFor(() => expect(screen.queryByText('1 field is not written yet')).toBeNull())
    expect(await bytes()).toContain('"value": {"shade": "green"}')
  })
})

describe('a deep link to a field with nothing focusable in it', () => {
  it('lands on the group, which is the address the link named', async () => {
    // A draft that declares no requirements gives this field no candidates, so
    // it renders one sentence and no control. In the reading view the same
    // address is a `Block` and takes focus itself; a field group is a plain
    // `div`, and `focus()` on one without a tab index is a no-op — the scroll
    // still happens, so the failure is silent.
    await draft(`${EDIT}#/rules/0/evidenceRequirementRefs`)
    await waitFor(() =>
      expect(document.activeElement?.getAttribute('data-pointer')).toBe(
        '/rules/0/evidenceRequirementRefs'
      )
    )
  })
})

describe('what a field says before the runtime answers', () => {
  it('states the id shape in words rather than printing the pattern', async () => {
    await draft()
    const hint = within(document.getElementById('/rules/0/id')!).getByText(/lowercase letters/)
    expect(hint.textContent).toBe('lowercase letters, digits and hyphens; starts with a letter.')
    // Read aloud, too: the hint is named in `aria-describedby`.
    expect(document.body.textContent).not.toContain('^[a-z]')
  })
})

/**
 * The one rule here that is read rather than rendered.
 *
 * `ui/Select` drops a value it was never offered, and the reason that is safe
 * is written down in its own doc: none of the options is ever `""`. Radix
 * reports `""` back through `onValueChange` when a controlled value changes
 * while the list is closed — `testing/radixGround.test.tsx` holds that finding
 * — so an option actually spelled `""` makes the guard pass a value nobody
 * chose, straight through to a writer that removes the member. The invariant
 * belongs to every call site and can be seen at none of them, which is why it
 * is held over the source in `ui/convention.test.ts`'s idiom.
 */
describe('the blank option', () => {
  it('is never spelled as the empty string anywhere in the app', () => {
    const offenders: string[] = []
    const walk = (directory: string) => {
      for (const entry of readdirSync(directory, { withFileTypes: true })) {
        const path = join(directory, entry.name)
        if (entry.isDirectory()) walk(path)
        else if (entry.name.endsWith('.tsx') && !entry.name.includes('.test.')) {
          const source = readFileSync(path, 'utf8')
          if (/\{\s*value:\s*''\s*,\s*label:/.test(source)) offenders.push(entry.name)
        }
      }
    }
    walk(join(import.meta.dirname, '..', '..'))
    expect(offenders).toEqual([])
  })
})
