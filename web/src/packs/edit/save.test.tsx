/**
 * Saving: what is sent, what is compared, and what is never allowed to decide.
 *
 * **The check does not gate the save.** The chassis writes bytes and the
 * runtime judges them, in that order. A desk that refused to write a document
 * with an outstanding diagnostic would be deciding what may exist on the user's
 * disk — and an author halfway through fixing one could not save their work.
 *
 * **The base moves only where the viewer acts.** A watcher refetch that
 * rebased would make Save overwrite bytes nobody saw, without the 409 that
 * exists to prevent exactly that.
 */
import { act, cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { chassis, drawPack, forgetSlot, served, PACK_DIGEST, PACK_PATH } from './editHarness'
import { forgetAuthorBridge } from '../../shell/authorBridge'

const PACK_TEXT = readFileSync(
  join(import.meta.dirname, '..', '__fixtures__', 'full.pack.json'),
  'utf8'
)

const JSON_MODE = '/packs/vendor-onboarding?edit=1&shape=json'

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
      layer: 'structural',
      severity: 'error',
      instancePath: '/rules/1/when/conditions/0/value',
      message: 'An ordered comparison takes a decimal string.'
    }
  ],
  diagnosticsTruncated: false
})

afterEach(() => {
  cleanup()
  forgetSlot()
  forgetAuthorBridge()
  vi.unstubAllGlobals()
})

async function editable(): Promise<HTMLTextAreaElement> {
  const area = (await screen.findByLabelText("The document's bytes")) as HTMLTextAreaElement
  await waitFor(() => expect(area.readOnly).toBe(false))
  return area
}

describe('what a save sends', () => {
  it('writes the buffer against the digest the editor loaded', async () => {
    const log = chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))
    expect(log.writes[0]).toMatchObject({
      path: PACK_PATH,
      content: `${PACK_TEXT}\n`,
      baseSha256: PACK_DIGEST,
      override: false
    })
  })

  it('is not gated on the check — an outstanding diagnostic stays on screen', async () => {
    const log = chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT, REFUSED), { path: JSON_MODE })
    const raw = await editable()
    await waitFor(() =>
      expect(screen.getByText(/An ordered comparison takes a decimal string/)).toBeTruthy()
    )
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    const save = await screen.findByRole('button', { name: 'Save' })
    expect(save.hasAttribute('disabled')).toBe(false)
    fireEvent.click(save)
    await waitFor(() => expect(log.writes).toHaveLength(1))
    // Still there afterwards: the runtime's opinion of the bytes did not stop
    // them being written and is not withdrawn by their having been.
    await waitFor(() =>
      expect(screen.getByText(/An ordered comparison takes a decimal string/)).toBeTruthy()
    )
  })

  it('re-asks the runtime about the pack once the bytes have landed', async () => {
    const log = chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    const { calls } = drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    const before = calls.filter((call) => call.name === 'get_pack').length
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))
    // `get_pack` and `list_packs` are both answers about a file that has just
    // changed; leaving them cached is a page describing a revision that is gone.
    await waitFor(() =>
      expect(calls.filter((call) => call.name === 'get_pack').length).toBeGreaterThan(before)
    )
    expect(calls.some((call) => call.name === 'list_packs')).toBe(true)
  })

  it('goes clean once the read-back matches what was submitted', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    await waitFor(() => expect(screen.getByText('unsaved')).toBeTruthy())
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.getByText('saved')).toBeTruthy())
  })

  it('says what the save did, in the chassis’ own figures', async () => {
    // `useFileEditing` has computed this since it was lifted out of
    // `AuthorView` and the pack editor rendered neither half of it: a save that
    // landed said nothing, and a read-back that was **not** what was sent said
    // nothing either.
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.getByText(/Saved, and verified/)).toBeTruthy())
    expect(screen.getByText(/byte for byte what was sent/)).toBeTruthy()
  })

  it('keeps what was typed while the write was in flight', async () => {
    // **The author is free to keep typing during a PUT**, and every keystroke
    // after the request is work the save did not carry. `rebase` replaced the
    // buffer with the landed bytes unconditionally, so the sentence typed while
    // the save was in the air disappeared at the moment the page said the save
    // had succeeded — and the buffer went clean, so nothing offered it back.
    const log = chassis({ content: PACK_TEXT, sha256: PACK_DIGEST, holdWrite: true })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    const submitted = `${PACK_TEXT}\n`
    fireEvent.change(raw, { target: { value: submitted } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))

    // Typed while the PUT is in the air.
    const later = `${PACK_TEXT}\n\n`
    fireEvent.change(screen.getByLabelText("The document's bytes"), { target: { value: later } })
    log.releaseWrite()

    await waitFor(() => expect(screen.getByText(/Saved, and verified/)).toBeTruthy())
    // The later text is still there, and it is dirty against what landed —
    // which is the true state: those bytes are not on disk.
    expect((screen.getByLabelText("The document's bytes") as HTMLTextAreaElement).value).toBe(later)
    expect(screen.getByText('unsaved')).toBeTruthy()
    // And saving again sends the later bytes against the digest that landed.
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(2))
    expect(log.writes[1]!.content).toBe(later)
    expect(log.writes[1]!.baseSha256).not.toBe(PACK_DIGEST)
  })
})

describe('what the page says a save left behind', () => {
  it('does not claim the editor holds what was sent when it holds something else', async () => {
    // Both halves at once: the read-back is not what was sent **and** the author
    // kept typing while the PUT was in the air. The sentence said "what is in
    // this editor is what was sent", which is then true of neither.
    const log = chassis({
      content: PACK_TEXT,
      sha256: PACK_DIGEST,
      holdWrite: true,
      landsAs: (content) => `${content}// touched by something else\n`
    })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))
    fireEvent.change(screen.getByLabelText("The document's bytes"), {
      target: { value: `${PACK_TEXT}\n\n` }
    })
    log.releaseWrite()

    await waitFor(() =>
      expect(screen.getByText(/read-back does not match/)).toBeTruthy()
    )
    expect(screen.getByText(/holds neither of them/)).toBeTruthy()
    expect(screen.queryByText(/What is in this editor is what was sent/)).toBeNull()
  })

  it('says the editor holds what was sent where it does', async () => {
    const log = chassis({
      content: PACK_TEXT,
      sha256: PACK_DIGEST,
      landsAs: (content) => `${content}// touched by something else\n`
    })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))
    await waitFor(() =>
      expect(screen.getByText(/What is in this editor is what was sent/)).toBeTruthy()
    )
  })
})

describe('a save that was refused for some other reason', () => {
  it('says so, rather than stopping saying “Saving…”', async () => {
    // Only `StaleWrite` was rendered. A path that is not writable, a chassis
    // that is not there, a body that did not parse — the button stopped saying
    // "Saving…" and the page said nothing at all.
    chassis({
      content: PACK_TEXT,
      sha256: PACK_DIGEST,
      failWrite: { status: 500, error: 'the chassis could not write the file' }
    })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.getByText(`Could not save ${PACK_PATH}`)).toBeTruthy()
    )
    expect(screen.getByText(/the chassis could not write the file/)).toBeTruthy()
    // The edit is still here: nothing was written and nothing was taken away.
    expect((screen.getByLabelText("The document's bytes") as HTMLTextAreaElement).value).toBe(
      `${PACK_TEXT}\n`
    )
  })

  it('does not let an earlier reload answer over a later one', async () => {
    // Two reads for one file, answered **out of order** — a slow first request
    // and a fast second one is the ordinary shape of a network. The last read
    // asked for is the answer; an earlier one landing afterwards would install
    // bytes the viewer asked to move on from.
    const log = chassis({
      content: PACK_TEXT,
      sha256: PACK_DIGEST,
      staleWith: { sha256: 'e1e1e1'.padEnd(64, '0') }
    })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    const alert = await screen.findByRole('alert')

    // The first Reload is held…
    const releaseFirst = log.hold(PACK_PATH)
    fireEvent.click(within(alert, 'Reload'))
    // …the file moves on disk, and a second Reload is held behind it.
    log.write(PACK_PATH, `${PACK_TEXT}// the newer bytes\n`)
    const releaseSecond = log.hold(PACK_PATH)
    fireEvent.click(within(screen.getByRole('alert'), 'Reload'))

    // The later read answers first, and the earlier one lands after it.
    releaseSecond()
    await act(async () => {})
    releaseFirst()
    await act(async () => {})

    await waitFor(() =>
      expect((screen.getByLabelText("The document's bytes") as HTMLTextAreaElement).value).toContain(
        '// the newer bytes'
      )
    )
  })

  it('says a reload failed, and keeps the conflict on screen', async () => {
    // Reload cleared the conflict before its own read had answered, so a read
    // that failed left the page with no notice, no error and a Save that would
    // 409 again — it had forgotten the one fact the author needed.
    const log = chassis({
      content: PACK_TEXT,
      sha256: PACK_DIGEST,
      staleWith: { sha256: 'e1e1e1'.padEnd(64, '0') }
    })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    const alert = await screen.findByRole('alert')
    log.breakReads()
    fireEvent.click(within(alert, 'Reload'))
    await waitFor(() =>
      expect(screen.getByText(`Could not reload ${PACK_PATH}`)).toBeTruthy()
    )
    // The conflict is still stated, because it is still true — beside the
    // failure, not replaced by it.
    const standing = screen.getAllByRole('alert').map((node) => node.textContent ?? '')
    expect(standing.some((text) => text.includes('Nothing was written'))).toBe(true)
    expect((screen.getByLabelText("The document's bytes") as HTMLTextAreaElement).value).toBe(
      `${PACK_TEXT}\n`
    )
  })
})

describe('a file that moved underneath the edit', () => {
  const ON_DISK = 'e1e1e1'.padEnd(64, '0')

  it('reports the refusal with both digests, and never writes', async () => {
    const log = chassis({
      content: PACK_TEXT,
      sha256: PACK_DIGEST,
      staleWith: { sha256: ON_DISK }
    })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('This file changed since you opened it. Nothing was written.')
    // Both digests, behind the disclosure rather than ahead of the sentence.
    expect(alert.textContent).toContain(PACK_DIGEST.slice(0, 12))
    expect(alert.textContent).toContain(ON_DISK.slice(0, 12))
    // The draft is intact: the buffer still carries the edit.
    expect((screen.getByLabelText("The document's bytes") as HTMLTextAreaElement).value).toBe(
      `${PACK_TEXT}\n`
    )
    expect(log.writes.every((write) => write.override === false)).toBe(true)
  })

  it('carries both digests whole, not only the twelve characters it prints', async () => {
    // Twelve characters do tell these two apart on this page, and they are the
    // right thing to *print* — sixty-four hex characters ahead of "nothing was
    // written" buries the sentence. But a reader comparing against `sha256sum`
    // or a git object needs all sixty-four, and a prefix that exists nowhere
    // else in the document cannot be compared with anything.
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST, staleWith: { sha256: ON_DISK } })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    const alert = await screen.findByRole('alert')
    const titles = [...alert.querySelectorAll('code')].map((code) => code.getAttribute('title'))
    expect(titles).toContain(PACK_DIGEST)
    expect(titles).toContain(ON_DISK)
  })

  it('offers Overwrite as the quiet control and never as the primary one', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST, staleWith: { sha256: ON_DISK } })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    const alert = await screen.findByRole('alert')
    const overwrite = within(alert, 'Overwrite anyway')
    const reload = within(alert, 'Reload')
    // A client whose primary button overwrote would have no concurrency story,
    // only an unstated one.
    expect(overwrite.className).not.toContain('primary')
    expect(reload.className).toContain('primary')
  })

  it('sends override only when the viewer asks for it by name', async () => {
    const log = chassis({
      content: PACK_TEXT,
      sha256: PACK_DIGEST,
      staleWith: { sha256: ON_DISK }
    })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    const alert = await screen.findByRole('alert')
    fireEvent.click(within(alert, 'Overwrite anyway'))
    await waitFor(() => expect(log.writes).toHaveLength(2))
    expect(log.writes[1]!.override).toBe(true)
  })

  it('says what Reload discards, and takes the file on disk', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST, staleWith: { sha256: ON_DISK } })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Reload takes the file on disk and discards these edits')
    fireEvent.click(within(alert, 'Reload'))
    await waitFor(() =>
      expect((screen.getByLabelText("The document's bytes") as HTMLTextAreaElement).value).toBe(
        PACK_TEXT
      )
    )
  })
})

describe('the keyboard, and the guard', () => {
  it('saves on Mod+S from inside the text field the shell suppresses in', async () => {
    const log = chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    await waitFor(() => expect(screen.getByText('unsaved')).toBeTruthy())
    fireEvent.keyDown(raw, { key: 's', ctrlKey: true })
    await waitFor(() => expect(log.writes).toHaveLength(1))
  })

  it('sends one write for a chord pressed twice, and one for a key held down', async () => {
    // **The button disables while saving and the chord read only `dirty`**,
    // which is state and arrives a render later: two presses inside one frame
    // both saw a clean-looking "not saving" and issued two PUTs against one
    // base — the second of which the chassis answers with a 409 this page then
    // explains to the author as somebody else's edit.
    const log = chassis({ content: PACK_TEXT, sha256: PACK_DIGEST, holdWrite: true })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    await waitFor(() => expect(screen.getByText('unsaved')).toBeTruthy())

    fireEvent.keyDown(raw, { key: 's', ctrlKey: true })
    fireEvent.keyDown(raw, { key: 's', ctrlKey: true })
    // A key held down: the operating system saying it is still down, not the
    // author asking again.
    fireEvent.keyDown(raw, { key: 's', ctrlKey: true, repeat: true })
    await waitFor(() => expect(log.writes).toHaveLength(1))
    log.releaseWrite()
    await waitFor(() => expect(screen.getByText('saved')).toBeTruthy())
    expect(log.writes).toHaveLength(1)

    // And the latch is let go when the write settles, so the next save is a
    // save: a latch released only through the mutation's observer stayed shut.
    fireEvent.change(screen.getByLabelText("The document's bytes"), {
      target: { value: `${PACK_TEXT}\n\n` }
    })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(2))
  })

  it('leaves a chord another handler has already answered alone', async () => {
    const log = chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    await waitFor(() => expect(screen.getByText('unsaved')).toBeTruthy())
    const answered = new KeyboardEvent('keydown', {
      key: 's',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    answered.preventDefault()
    raw.dispatchEvent(answered)
    await Promise.resolve()
    expect(log.writes).toHaveLength(0)
  })

  it('does not discard on Escape', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    drawPack(served(PACK_TEXT), { path: JSON_MODE })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    await waitFor(() => expect(screen.getByText('unsaved')).toBeTruthy())
    fireEvent.keyDown(raw, { key: 'Escape' })
    // A key that threw away an edit without asking is a key nobody presses
    // twice.
    expect(screen.getByText('unsaved')).toBeTruthy()
    expect((screen.getByLabelText("The document's bytes") as HTMLTextAreaElement).value).toBe(
      `${PACK_TEXT}\n`
    )
  })

  it('never prompts on a mode toggle, and prompts on leaving the pack', async () => {
    chassis({ content: PACK_TEXT, sha256: PACK_DIGEST })
    const asked: string[] = []
    vi.stubGlobal('confirm', (question: string) => {
      asked.push(question)
      return false
    })
    const { router } = drawPack(served(PACK_TEXT), { path: JSON_MODE, nav: true })
    const raw = await editable()
    fireEvent.change(raw, { target: { value: `${PACK_TEXT}\n` } })
    await waitFor(() => expect(screen.getByText('unsaved')).toBeTruthy())
    // The mode is a search parameter and the blocker's predicate is the
    // pathname: this is the same page.
    fireEvent.click(screen.getByRole('radio', { name: 'Form' }))
    await waitFor(() => expect(router.state.location.search).toContain('edit=1'))
    expect(asked).toEqual([])
    // A pathname change with a dirty buffer is the exit the guard exists for.
    fireEvent.click(screen.getByRole('link', { name: 'go elsewhere' }))
    await waitFor(() => expect(asked).toHaveLength(1))
    expect(asked[0]).toContain('unsaved')
    expect(router.state.location.pathname).toBe('/packs/vendor-onboarding')
  })
})

/** One button inside a panel, by the words on it. */
function within(root: HTMLElement, label: string): HTMLElement {
  const found = [...root.querySelectorAll('button')].find(
    (button) => button.textContent?.trim() === label
  )
  if (found === undefined) throw new Error(`no button labelled ${label}`)
  return found
}
