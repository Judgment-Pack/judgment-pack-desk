/**
 * Edit mode: one buffer, two views of it, and the diagnostics that land on the
 * field.
 *
 * The claim under all of it is that **the page and the form are over the same
 * bytes**. Phase 1 drew the served document; a form over that while the buffer
 * moved underneath would write at pointers computed from a revision the reader
 * is not looking at — the digest-binding failure, one component further in. So
 * the cases below drive the JSON view and assert the reading document moved,
 * drive a form field and assert the bytes moved, and hold the withholding rule
 * for bytes neither reading can take.
 */
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { chassis, drawPack, forgetSlot, served, PACK_DIGEST, PACK_PATH } from './editHarness'

const FIXTURES = join(import.meta.dirname, '..', '__fixtures__')
const PACK_TEXT = readFileSync(join(FIXTURES, 'full.pack.json'), 'utf8')
const DUPLICATE = readFileSync(join(FIXTURES, 'duplicate-member.pack.json'), 'utf8')

afterEach(() => {
  cleanup()
  forgetSlot()
  vi.unstubAllGlobals()
})

/**
 * The raw textarea, once the chassis has answered.
 *
 * It renders before the file is read — showing the runtime's served bytes,
 * read-only — so a case that typed straight into it would be typing into text
 * the load is about to replace.
 */
async function editableBytes(): Promise<HTMLTextAreaElement> {
  const area = (await screen.findByLabelText("The document's bytes")) as HTMLTextAreaElement
  await waitFor(() => expect(area.readOnly).toBe(false))
  return area
}

const EDIT = '/packs/vendor-onboarding?edit=1'
const JSON_MODE = '/packs/vendor-onboarding?edit=1&shape=json'

describe('the mode is the address', () => {
  it('draws the toolbar in edit mode and not in read mode', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    expect(screen.getByRole('toolbar', { name: 'Editing' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy()
  })

  it('keeps the selection and the mount when the mode is toggled', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    const { router } = drawPack(served(PACK_TEXT), {
      path: '/packs/vendor-onboarding?at=%2Frules%2F1'
    })
    await screen.findByRole('navigation', { name: 'Members' })
    fireEvent.click(screen.getByRole('radio', { name: 'Edit' }))
    await waitFor(() =>
      expect(router.state.location.search).toContain('edit=1')
    )
    // **The pathname never moves**, which is the entire reason the mode is a
    // search parameter: `useDirtyGuard`'s predicate is the pathname alone, so
    // a mode written into the path would prompt on every toggle.
    expect(router.state.location.pathname).toBe('/packs/vendor-onboarding')
    expect(router.state.location.search).toContain('at=%2Frules%2F1')
  })

  it('suppresses the standing Try it link in edit mode', async () => {
    // Two controls called "Try it" on one page is one of them being about
    // something else. The link goes to `/evaluate`, the toolbar's button runs
    // the draft; in edit mode only the second is offered.
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    expect(screen.queryByRole('link', { name: 'Try it' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Try it' })).toBeTruthy()
  })
})

describe('both views are one buffer', () => {
  it('moves the reading document when the JSON view is typed into', async () => {
    // **The single riskiest claim in the design.** The document on the page is
    // `indexDocument(buffer).value`, so a keystroke in the raw bytes is a
    // keystroke in the document above them.
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editableBytes()
    const edited = PACK_TEXT.replace('"Vendor onboarding"', '"Vendor onboarding, revised"')
    fireEvent.change(raw, { target: { value: edited } })
    fireEvent.click(screen.getByRole('radio', { name: 'Form' }))
    await waitFor(() => expect(screen.getByDisplayValue('Vendor onboarding, revised')).toBeTruthy())
  })

  it('moves the bytes when a form field is typed into', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    const title = await screen.findByDisplayValue('Vendor onboarding')
    fireEvent.change(title, { target: { value: 'Vendor onboarding, revised' } })
    fireEvent.click(screen.getByRole('radio', { name: 'JSON' }))
    const raw = (await screen.findByLabelText("The document's bytes")) as HTMLTextAreaElement
    expect(raw.value).toContain('"title": "Vendor onboarding, revised"')
    // Every byte outside the spliced span survives: the splice is the whole
    // mechanism, and a re-serialization would have re-indented the file.
    expect(raw.value.replace('Vendor onboarding, revised', 'Vendor onboarding')).toBe(PACK_TEXT)
  })

  it('marks the buffer unsaved on a byte change and clean after a discard', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editableBytes()
    expect(screen.getByText('saved')).toBeTruthy()
    // Whitespace alone is a change to the file, because a save writes bytes.
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    await waitFor(() => expect(screen.getByText('unsaved')).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))
    await waitFor(() => expect(screen.getByText('saved')).toBeTruthy())
  })
})

describe('bytes the desk cannot read as a document', () => {
  it('keeps JSON available, withholds Form, and says where the scan stopped', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editableBytes()
    fireEvent.change(raw, { target: { value: '{"specVersion": ' } })
    await waitFor(() =>
      expect(screen.getByText(/not a document this desk can edit as a form/)).toBeTruthy()
    )
    // The position is a line and a column, because that is what the gutter
    // beside the text is numbered in.
    expect(screen.getByText(/line 1, column 17/)).toBeTruthy()
    expect(screen.getByRole('radio', { name: 'Form' }).getAttribute('disabled')).not.toBeNull()
  })

  it('withholds form mode over a document the two readings disagree about', async () => {
    // `JSON.parse` keeps a duplicated member's last occurrence, this desk's
    // scanner keeps the first, and the runtime refuses the document. A form
    // that wrote through a reading nobody shares would edit a document nobody
    // has.
    chassis({ content: DUPLICATE, sha256: PACK_DIGEST })
    drawPack(served(DUPLICATE), { path: EDIT })
    // Twice: the strip says it, and the JSON view says why the form is
    // withheld. Both are about the same member and neither is the other's.
    await waitFor(() => expect(screen.getAllByText(/appears more than once/).length).toBe(2))
    expect(screen.getByRole('radio', { name: 'Form' }).getAttribute('disabled')).not.toBeNull()
    expect(screen.getByLabelText("The document's bytes")).toBeTruthy()
  })
})

describe('a diagnostic reaches the field it is about', () => {
  const REFUSED = JSON.stringify({
    outputVersion: '2',
    status: 'invalid',
    layers: [
      { name: 'carrier', status: 'passed' },
      { name: 'structural', status: 'failed' }
    ],
    diagnostics: [
      {
        code: 'JPS-STRUCTURE-DECIMAL-OPERAND',
        codeStability: 'provisional',
        layer: 'structural',
        severity: 'error',
        instancePath: '/rules/1/when/conditions/0/value',
        message: 'An ordered comparison takes a decimal string.'
      }
    ],
    diagnosticsTruncated: false
  })

  it('describes the control by aria-describedby, in the runtime’s own words', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT, REFUSED), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    const operand = await screen.findByDisplayValue('5000')
    const described = operand.getAttribute('aria-describedby')
    expect(described).toBeTruthy()
    const spoken = described!
      .split(' ')
      .map((id) => document.getElementById(id)?.textContent ?? '')
      .join(' ')
    expect(spoken).toContain('JPS-STRUCTURE-DECIMAL-OPERAND')
    expect(spoken).toContain('An ordered comparison takes a decimal string.')
    expect(operand.getAttribute('aria-invalid')).toBe('true')
  })

  it('anchors a missing required member on the field that is not there yet', async () => {
    // The runtime reports one at the pointer **including the absent name**, so
    // `/exceptions/0/onUnknown` names a member the document omits — and the
    // form draws that field whether or not the document carries it.
    const missing = JSON.stringify({
      outputVersion: '2',
      status: 'invalid',
      layers: [{ name: 'carrier', status: 'passed' }, { name: 'structural', status: 'failed' }],
      diagnostics: [
        {
          code: 'JPS-STRUCTURE-REQUIRED',
          layer: 'structural',
          severity: 'error',
          instancePath: '/exceptions/0/onUnknown',
          message: 'onUnknown is required.'
        }
      ],
      diagnosticsTruncated: false
    })
    const without = PACK_TEXT.replace('      "onUnknown": "escalate",\n      "sourceRefs"', '      "sourceRefs"')
    expect(without).not.toBe(PACK_TEXT)
    chassis({ content: without, sha256: PACK_DIGEST })
    drawPack(served(without, missing), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    await waitFor(() => expect(screen.getByText('onUnknown is required.')).toBeTruthy())
    const field = document.getElementById('/exceptions/0/onUnknown')
    expect(field).toBeTruthy()
    expect(within(field!).getByText('onUnknown is required.')).toBeTruthy()
  })
})

describe('rule order, which is what the pack decides', () => {
  it('moves a rule through the writer, follows it with focus, and announces where it landed', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    const down = await screen.findAllByRole('button', { name: 'Move this rule down' })
    fireEvent.click(down[0]!)
    await waitFor(() => expect(screen.getByText('Moved to position 2 of 2.')).toBeTruthy())
    // Focus follows the card to its new address, which is `/rules/1`.
    await waitFor(() => expect(document.activeElement?.getAttribute('data-pointer')).toBe('/rules/1'))
    fireEvent.click(screen.getByRole('radio', { name: 'JSON' }))
    const raw = (await screen.findByLabelText("The document's bytes")) as HTMLTextAreaElement
    expect(raw.value.indexOf('"approve-when-clear"')).toBeLessThan(raw.value.indexOf('"screen-first"'))
  })

  it('marks the check stale after a move rather than re-anchoring a diagnostic', async () => {
    const REFUSED = JSON.stringify({
      outputVersion: '2',
      status: 'invalid',
      layers: [{ name: 'carrier', status: 'passed' }, { name: 'semantic', status: 'failed' }],
      diagnostics: [
        {
          code: 'JPS-SEMANTIC-UNREACHABLE-RULE',
          layer: 'semantic',
          severity: 'error',
          instancePath: '/rules/0',
          message: 'This rule can never fire.'
        }
      ],
      diagnosticsTruncated: false
    })
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    // A diagnostic about a whole rule anchors on the card, and a card is not a
    // field — the panel is where it is printed, in both modes.
    drawPack(served(PACK_TEXT, REFUSED), {
      path: '/packs/vendor-onboarding?edit=1&at=%2Frules%2F0',
      inspector: true,
      tab: 'checks'
    })
    await screen.findByRole('navigation', { name: 'Members' })
    await waitFor(() => expect(screen.getByText('This rule can never fire.')).toBeTruthy())
    fireEvent.click((await screen.findAllByRole('button', { name: 'Move this rule down' }))[0]!)
    await waitFor(() =>
      expect(screen.getByText(/ran over bytes the editor has moved past/)).toBeTruthy()
    )
    // Not moved to the other rule, not kept where it was: nothing at all.
    expect(screen.queryByText('This rule can never fire.')).toBeNull()
  })
})

describe('what an omitted member offers', () => {
  it('writes the member at the position the schema gives it', async () => {
    const without = PACK_TEXT.replace(/\n  "fallbackOutcome": "decline",/, '')
    expect(without).not.toBe(PACK_TEXT)
    chassis({ content: without, sha256: PACK_DIGEST })
    drawPack(served(without), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    fireEvent.click(await screen.findByRole('button', { name: 'Declare it' }))
    fireEvent.click(screen.getByRole('radio', { name: 'JSON' }))
    const raw = (await screen.findByLabelText("The document's bytes")) as HTMLTextAreaElement
    expect(raw.value).toContain('"fallbackOutcome"')
    // Between `exceptions` and `escalation`, which is the schema's own order.
    expect(raw.value.indexOf('"fallbackOutcome"')).toBeGreaterThan(raw.value.indexOf('"exceptions"'))
    expect(raw.value.indexOf('"fallbackOutcome"')).toBeLessThan(raw.value.indexOf('"escalation"'))
  })
})

describe('the file the editor holds', () => {
  it('reads the file once and never rebases on a watcher answer', async () => {
    const log = chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    await screen.findByLabelText("The document's bytes")
    await waitFor(() => expect(log.reads).toBeGreaterThan(0))
    expect(screen.getByText(PACK_PATH)).toBeTruthy()
  })
})
