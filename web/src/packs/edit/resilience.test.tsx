/**
 * What the page does when something about it is wrong — and the two chords and
 * one measurement that have to keep working while it is.
 *
 * The editor is the thing that put a file into most of these states, so it is
 * the thing that has to survive them: bytes shaped like nothing the desk
 * expects, bytes the runtime refuses outright, focus resting where no handler
 * is listening. None of these is an error page.
 */
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { chassis, drawPack, forgetSlot, served, CLEAN_REPORT, PACK_DIGEST, PACK_PATH } from './editHarness'
import { declaredIds } from './editingContext'
import { forgetAuthorBridge } from '../../shell/authorBridge'
import type { ToolHandler } from '../../testing/harness'

const PACK_TEXT = readFileSync(
  join(import.meta.dirname, '..', '__fixtures__', 'full.pack.json'),
  'utf8'
)

const EDIT = '/packs/vendor-onboarding?edit=1'
const JSON_MODE = '/packs/vendor-onboarding?edit=1&shape=json'

afterEach(() => {
  cleanup()
  forgetSlot()
  forgetAuthorBridge()
  vi.unstubAllGlobals()
})

async function editableBytes(): Promise<HTMLTextAreaElement> {
  const area = (await screen.findByLabelText("The document's bytes")) as HTMLTextAreaElement
  await waitFor(() => expect(area.readOnly).toBe(false))
  return area
}

describe('bytes shaped like nothing this desk expects', () => {
  it('reads every list as whatever it is', () => {
    // The buffer is a document somebody is in the middle of writing. `?? []`
    // catches null and undefined and nothing else, so an object here threw out
    // of a memo the route computes in **every** mode.
    const ids = declaredIds({
      rules: { oops: true },
      outcomes: 5,
      evidenceRequirements: 'nope',
      sources: [{ id: 'handbook' }, null, { id: '' }, 'x']
    } as never)
    expect(ids.rules).toEqual([])
    expect(ids.outcomes).toEqual([])
    expect(ids.evidence).toEqual([])
    expect(ids.sources).toEqual(['handbook'])
  })

  it('holds a pasted object where an array was, without taking the page down', async () => {
    // **Valid JSON, wrong shape**, which is the whole case: bytes that do not
    // scan withhold form mode and never reach the readers below, so a paste
    // that merely broke the syntax would pass either way. The JSON view is the
    // one mode whose purpose is to hold bytes the desk cannot otherwise read,
    // and the route element unmounting takes the unsaved buffer with it —
    // there is nowhere else it exists.
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editableBytes()
    const parsed = JSON.parse(PACK_TEXT) as Record<string, unknown>
    parsed.rules = { oops: true }
    const wrong = `${JSON.stringify(parsed, null, 2)}\n`
    fireEvent.change(raw, { target: { value: wrong } })
    await waitFor(() => expect(raw.value).toBe(wrong))
    expect(screen.getByLabelText("The document's bytes")).toBeTruthy()
    expect(screen.getByRole('toolbar', { name: 'Editing' })).toBeTruthy()
  })
})

/** `get_pack` refusing the file the editor can still read off the disk. */
function refusing(): Record<string, ToolHandler> {
  return {
    get_pack: () => ({
      text: 'pack document is not acceptable JSON: Input is not valid JSON at line 1, column 3',
      isError: true
    }),
    list_packs: () => ({
      text: JSON.stringify({ packs: [{ id: 'vendor-onboarding', path: PACK_PATH }] })
    }),
    validate: () => ({ text: CLEAN_REPORT })
  }
}

describe('a pack the runtime will not serve', () => {
  const BROKEN = '{ this is not json'

  it('keeps the bytes on screen and says why the runtime refused them', async () => {
    // Save is not gated on the check, which is right — and phase 2 is what
    // supplies the writer that can reach this state. A refusal that replaced
    // the whole route left the author with a JSON view they could not reach
    // and nothing on screen saying where to go.
    chassis({ content: BROKEN, sha256: PACK_DIGEST })
    drawPack(refusing(), { path: JSON_MODE })
    const raw = await editableBytes()
    expect(raw.value).toBe(BROKEN)
    expect(screen.getByText(/pack document is not acceptable JSON/)).toBeTruthy()
    // And the toolbar, so the bytes can be repaired and written again.
    expect(screen.getByRole('toolbar', { name: 'Editing' })).toBeTruthy()
  })

  it('offers the way in from read mode, where nothing can be drawn', async () => {
    chassis({ content: BROKEN, sha256: PACK_DIGEST })
    drawPack(refusing(), { path: '/packs/vendor-onboarding' })
    const raw = (await screen.findByLabelText("The document's bytes")) as HTMLTextAreaElement
    // Read means read: the bytes are shown and not editable until the mode is.
    expect(raw.readOnly).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    await waitFor(() =>
      expect((screen.getByLabelText("The document's bytes") as HTMLTextAreaElement).readOnly).toBe(
        false
      )
    )
  })
})

describe('Mod+S belongs to the mode, not to a subtree', () => {
  it('saves and claims the chord with focus resting on the body', async () => {
    // Where focus sits the moment edit mode opens, because the Edit button
    // unmounts itself. From here the chord used to reach nothing, and the
    // browser's own "Save page as…" opened over unsaved work.
    const log = chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editableBytes()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    await waitFor(() => expect(screen.getByText('unsaved')).toBeTruthy())
    ;(document.activeElement as HTMLElement | null)?.blur()
    expect(document.activeElement).toBe(document.body)
    // `fireEvent` answers false where the event was cancelled, which is the
    // half that keeps the browser out of it.
    expect(fireEvent.keyDown(document.body, { key: 's', ctrlKey: true })).toBe(false)
    await waitFor(() => expect(log.writes).toHaveLength(1))
  })

  it('leaves the chord alone in read mode', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: '/packs/vendor-onboarding' })
    await screen.findByRole('navigation', { name: 'Members' })
    // Nothing is being edited, so nothing is being saved and the chord is the
    // browser's.
    expect(fireEvent.keyDown(document.body, { key: 's', ctrlKey: true })).toBe(true)
  })
})

describe('a deep link into a form', () => {
  it('moves focus to the control the pointer names', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: `${EDIT}#/rules/0/description` })
    await screen.findByRole('navigation', { name: 'Members' })
    await waitFor(() => {
      const focused = document.activeElement as HTMLElement | null
      expect(focused?.tagName).toBe('TEXTAREA')
      expect(focused?.closest('[data-pointer]')?.getAttribute('data-pointer')).toBe(
        '/rules/0/description'
      )
    })
  })
})

describe('the rule-move chord', () => {
  it('fires again from the card the last move focused', async () => {
    // The move focuses the card, so the card is where the next press comes
    // from. Bound one level in, the chord worked exactly once and then went
    // dead — and nothing said so.
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    const first = within(document.getElementById('/rules/0')!).getByDisplayValue('screen-first')
    fireEvent.keyDown(first, { key: 'ArrowDown', altKey: true })
    await waitFor(() =>
      expect(document.activeElement?.getAttribute('data-pointer')).toBe('/rules/1')
    )
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp', altKey: true })
    await waitFor(() =>
      expect(document.activeElement?.getAttribute('data-pointer')).toBe('/rules/0')
    )
    // Down and back up: the array is exactly the bytes it started as, which is
    // also what says the second press did something.
    fireEvent.click(screen.getByRole('radio', { name: 'JSON' }))
    const raw = (await screen.findByLabelText("The document's bytes")) as HTMLTextAreaElement
    expect(raw.value).toBe(PACK_TEXT)
  })
})

describe('where Try it opens, measured', () => {
  let unmeasure: (() => void) | undefined
  /** The frame at one width, and the column at the width it has once the pane is beside it. */
  const measured = (frame: number, column: number) => {
    const original = HTMLElement.prototype.getBoundingClientRect
    HTMLElement.prototype.getBoundingClientRect = function rect(this: HTMLElement) {
      const width = String(this.className).includes('column') ? column : frame
      return {
        width,
        height: 800,
        top: 0,
        left: 0,
        right: width,
        bottom: 800,
        x: 0,
        y: 0,
        toJSON: () => ({})
      } as DOMRect
    }
    unmeasure = () => {
      HTMLElement.prototype.getBoundingClientRect = original
    }
  }
  afterEach(() => {
    unmeasure?.()
    unmeasure = undefined
  })

  it('asks the frame, whose width the placement does not change', async () => {
    // 1000 of workspace leaves 592 for the editor once the pane's 392 and the
    // gap are taken. The column is 600 *because the pane is there* — reading
    // the decision off it makes the predicate's input depend on its own
    // output, and between those two numbers neither answer is a fixed point.
    measured(1000, 600)
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    const { revealed } = drawPack(served(PACK_TEXT), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    fireEvent.click(screen.getByRole('button', { name: 'Try it' }))
    const pane = await screen.findByRole('complementary', { name: 'Try it' })
    expect(String(pane.parentElement?.className)).toContain('pane')
    // Nothing was asked to move: the Inspector is not opened for a pane that
    // fits beside the editor.
    expect(revealed).toEqual([])
  })
})

describe('the outline while the author types', () => {
  it('does not rebuild its observer on every keystroke', async () => {
    // The rendered set re-walks every `[data-pointer]` and the spy disconnects
    // and re-observes, dropping its answer back to the `?at` selection. Keyed
    // on the live buffer that happened once per character.
    const built: string[] = []
    class Counting {
      constructor() {
        built.push('built')
      }
      observe() {}
      unobserve() {}
      disconnect() {}
      takeRecords() {
        return []
      }
    }
    vi.stubGlobal('IntersectionObserver', Counting)
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    const title = await screen.findByDisplayValue('Vendor onboarding')
    await waitFor(() => expect(built.length).toBeGreaterThan(0))
    const before = built.length
    fireEvent.change(title, { target: { value: 'Vendor onboarding, revised' } })
    expect(screen.getByDisplayValue('Vendor onboarding, revised')).toBeTruthy()
    expect(built.length).toBe(before)
  })
})

describe('the Inspector’s provenance', () => {
  it('stops claiming the editor holds the file once it does not', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), {
      path: '/packs/vendor-onboarding?edit=1&at=%2Ftitle',
      inspector: true,
      tab: 'member'
    })
    await screen.findByRole('navigation', { name: 'Members' })
    await waitFor(() =>
      expect(screen.getByText('matches the file the editor holds')).toBeTruthy()
    )
    fireEvent.change(await screen.findByDisplayValue('Vendor onboarding'), {
      target: { value: 'Vendor onboarding, revised' }
    })
    await waitFor(() =>
      expect(screen.queryByText('matches the file the editor holds')).toBeNull()
    )
    // The figures are still printed — they are facts about the file — and what
    // they are about is now said.
    expect(screen.getByText(/These figures are the file on disk/)).toBeTruthy()
    expect(screen.getByText(PACK_DIGEST)).toBeTruthy()
  })
})
