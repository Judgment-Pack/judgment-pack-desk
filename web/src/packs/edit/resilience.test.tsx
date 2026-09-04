/**
 * What the page does when something about it is wrong — and the two chords and
 * one measurement that have to keep working while it is.
 *
 * The editor is the thing that put a file into most of these states, so it is
 * the thing that has to survive them: bytes shaped like nothing the desk
 * expects, bytes the runtime refuses outright, focus resting where no handler
 * is listening. None of these is an error page.
 */
import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
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

  it('draws a rule that is not an object as its bytes, not as a card', async () => {
    // The list is a list — the member-level guard has nothing to say about it —
    // and one element of it is `null`. `RuleCard` reads `rule.id`.
    const parsed = JSON.parse(PACK_TEXT) as { rules: unknown[] }
    parsed.rules = [parsed.rules[0], null]
    const wrong = `${JSON.stringify(parsed, null, 2)}\n`
    chassis({ content: wrong, sha256: PACK_DIGEST })
    drawPack(served(wrong), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    const said = await screen.findByText(/not the shape the form edits/)
    expect(said.textContent).toContain('/rules/1')
    expect(document.getElementById('/rules/1')).toBeTruthy()
    // The rule beside it is still a card with fields in it.
    expect(screen.getByDisplayValue('screen-first')).toBeTruthy()
  })

  it('says which member is not the shape the form edits, rather than crashing', async () => {
    // **Form availability asked whether the two readings agreed and nothing
    // else.** `"rules": {}` agrees perfectly — it is valid JSON both readings
    // read the same way — and selecting Form reached `rules.map` and took the
    // route down with it.
    const parsed = JSON.parse(PACK_TEXT) as Record<string, unknown>
    parsed.rules = { oops: true }
    const wrong = `${JSON.stringify(parsed, null, 2)}\n`
    chassis({ content: wrong, sha256: PACK_DIGEST })
    drawPack(served(wrong), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    const said = await screen.findByText(/not the shape the form edits/)
    expect(said.textContent).toContain('/rules')
    // The block is addressed, so a diagnostic about it still lands here and the
    // outline entry still reaches it.
    expect(document.getElementById('/rules')).toBeTruthy()
    // And the rest of the document is still drawn.
    expect(screen.getByDisplayValue('Vendor onboarding')).toBeTruthy()
  })

  it('states a member that is present and not an object, instead of writing nowhere', async () => {
    // `"locator": null` drew every field of a locator, took a keystroke, and
    // moved no bytes: `writes.place` splices into a container and there is
    // none. Nothing on screen said so.
    const parsed = JSON.parse(PACK_TEXT) as { sources: Record<string, unknown>[] }
    parsed.sources[0]!.locator = null
    const wrong = `${JSON.stringify(parsed, null, 2)}\n`
    chassis({ content: wrong, sha256: PACK_DIGEST })
    drawPack(served(wrong), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    const said = await screen.findAllByText(/not the shape this form edits/)
    expect(said.length).toBeGreaterThan(0)
  })

  it('draws the buffer’s own bytes and never the served document', async () => {
    // `drawn` fell back to the served pack whenever the buffer did not scan, so
    // saving bytes the runtime refuses and returning to Read put the **old**
    // document on screen — over a file that no longer holds it, with the JSON
    // view the only place the truth was.
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editableBytes()
    fireEvent.change(raw, { target: { value: '{ this is not json' } })
    fireEvent.click(screen.getByRole('radio', { name: 'Form' }))
    await waitFor(() =>
      expect((screen.getByLabelText("The document's bytes") as HTMLTextAreaElement).value).toBe(
        '{ this is not json'
      )
    )
    // Not the served document's title, which is what a fallback would draw.
    expect(screen.queryByText('Vendor onboarding')).toBeNull()
    expect(screen.getByText(/not a document this desk can edit as a form/)).toBeTruthy()
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

  it('fits at the shell’s own maximum, and not one pixel below it', async () => {
    // **The number in the predicate and the width the pane takes were eight
    // pixels apart.** The shell's `60rem` box less its `1.5rem` padding either
    // side is exactly 912, which fits 384 + 16 + 512 — and the page said it did
    // not, so the side-by-side branch was unreachable on every ordinary screen.
    measured(912, 512)
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    const { revealed } = drawPack(served(PACK_TEXT), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    fireEvent.click(screen.getByRole('button', { name: 'Try it' }))
    const pane = await screen.findByRole('complementary', { name: 'Try it' })
    expect(String(pane.parentElement?.className)).toContain('pane')
    expect(revealed).toEqual([])
  })

  it('takes the Inspector’s place one pixel below it', async () => {
    measured(911, 511)
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    const { revealed } = drawPack(served(PACK_TEXT), { path: EDIT, inspector: true })
    await screen.findByRole('navigation', { name: 'Members' })
    fireEvent.click(screen.getByRole('button', { name: 'Try it' }))
    const pane = await screen.findByRole('complementary', { name: 'Try it' })
    // Published into the slot rather than placed beside the editor, and the
    // pane is asked to open because a closed one has nowhere to publish into.
    expect(String(pane.parentElement?.className)).not.toContain('pane')
    expect(revealed).toEqual(['reveal'])
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

describe('a file that moved after it was loaded', () => {
  it('says which revision is on screen, and offers the one on disk', async () => {
    // **A watcher refetch moves the file query and deliberately not the base**
    // — that is what makes a stale write a 409 instead of a silent overwrite.
    // So a clean buffer can be a *previous* revision, and the page called it
    // "the bytes of packs/vendor-onboarding.pack.json" while the Inspector said
    // it matched a file it had never read.
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    const { queryClient } = drawPack(served(PACK_TEXT), {
      path: '/packs/vendor-onboarding?edit=1&at=%2Ftitle',
      inspector: true,
      tab: 'member'
    })
    await screen.findByRole('navigation', { name: 'Members' })
    await waitFor(() =>
      expect(screen.getByText('matches the file the editor holds')).toBeTruthy()
    )

    // Somebody else wrote the file. This is what the watcher's invalidation
    // does: the query answers with the new revision and the editor keeps the
    // one it loaded.
    const moved = `${PACK_TEXT}\n`
    act(() => {
      queryClient.setQueryData(['desk-file', PACK_PATH], {
        path: PACK_PATH,
        bytes: moved.length,
        sha256: 'd1d1d1'.padEnd(64, '0'),
        content: moved
      })
    })

    await waitFor(() =>
      expect(screen.getByText(/The file on disk has changed since this was loaded/)).toBeTruthy()
    )
    // The binding is withdrawn, and what is on screen is named as the revision
    // it is rather than as the file.
    expect(screen.queryByText('matches the file the editor holds')).toBeNull()
    expect(screen.getByText(/showing the revision it loaded/)).toBeTruthy()
    expect(screen.getByText(/the bytes you loaded from/)).toBeTruthy()
    // And the way to the bytes that are there now is offered rather than taken.
    expect(screen.getByRole('button', { name: 'Reload' })).toBeTruthy()
  })
})

describe('when both answers move and the editor does not', () => {
  it('withdraws the binding, which is the only thing that can', async () => {
    // **The two digests agreeing is not the question.** A file written by
    // somebody else invalidates both answers, so `get_pack` and the file read
    // agree with each other about revision two — while the editor is holding
    // revision one. Comparing those two alone said "matches the file the editor
    // holds" over bytes the editor had never read.
    let text = PACK_TEXT
    let digest = PACK_DIGEST
    const answers: Record<string, ToolHandler> = {
      get_pack: () => ({
        text,
        structured: { path: PACK_PATH, bytes: text.length, sha256: digest }
      }),
      list_packs: () => ({
        text: JSON.stringify({ packs: [{ id: 'vendor-onboarding', path: PACK_PATH }] })
      }),
      validate: () => ({ text: CLEAN_REPORT })
    }
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    const { queryClient } = drawPack(answers, {
      path: '/packs/vendor-onboarding?edit=1&at=%2Ftitle',
      inspector: true,
      tab: 'member'
    })
    await screen.findByRole('navigation', { name: 'Members' })
    await waitFor(() =>
      expect(screen.getByText('matches the file the editor holds')).toBeTruthy()
    )

    text = `${PACK_TEXT}\n`
    digest = 'd2d2d2'.padEnd(64, '0')
    act(() => {
      queryClient.setQueryData(['desk-file', PACK_PATH], {
        path: PACK_PATH,
        bytes: text.length,
        sha256: digest,
        content: text
      })
    })
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['get_pack'] })
    })

    await waitFor(() =>
      expect(screen.queryByText('matches the file the editor holds')).toBeNull()
    )
    expect(screen.getByText(/showing the revision it loaded/)).toBeTruthy()
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
