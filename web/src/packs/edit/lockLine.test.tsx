/**
 * The lock line, and the far more important thing beside it: everything this
 * page will not say.
 *
 * **No tool reports lock state.** None of the runtime's answers carries a lock
 * member, the Evaluation payload does not either, and `packs lock` is a CLI
 * verb (ADR-0019). So the desk cannot know whether this pack is in the reviewed
 * set, whether the set is current, or whether saving takes it out — and it must
 * not compute any of the three, because each would be a verdict dressed as a
 * fact.
 *
 * What it can see is that `jpack.lock.json` is in the file listing. That is a
 * fact about the project, and it is all this says.
 */
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LockLine } from './LockLine'
import { chassis, drawPack, forgetSlot, served, PACK_DIGEST, PACK_PATH } from './editHarness'

const PACK_TEXT = readFileSync(
  join(import.meta.dirname, '..', '__fixtures__', 'full.pack.json'),
  'utf8'
)
const EDIT = '/packs/vendor-onboarding?edit=1'
const SENTENCE = /This project keeps a reviewed set/

const listed = (...paths: string[]) =>
  paths.map((path) => ({ path, bytes: 1, sha256: 'a'.repeat(64) }))

afterEach(() => {
  cleanup()
  forgetSlot()
  vi.unstubAllGlobals()
})

describe('the line itself', () => {
  it('appears where the lock file is listed', () => {
    render(<LockLine paths={['jpack.json', 'jpack.lock.json', PACK_PATH]} />)
    expect(screen.getByText(SENTENCE)).toBeTruthy()
  })

  it('appears for a lock file inside a directory', () => {
    render(<LockLine paths={['project/jpack.lock.json']} />)
    expect(screen.getByText(SENTENCE)).toBeTruthy()
  })

  it('says nothing at all where it is not listed', () => {
    // Silence, and not "this project keeps no reviewed set" — which would be a
    // claim about a file that may simply not have been read.
    const { container } = render(<LockLine paths={['jpack.json', PACK_PATH]} />)
    expect(container.textContent).toBe('')
  })

  it('is not fooled by a name that merely ends the same way', () => {
    const { container } = render(<LockLine paths={['not-jpack.lock.json']} />)
    expect(container.textContent).toBe('')
  })
})

describe('the line on the page', () => {
  it('is shown when the project’s listing carries the lock file', async () => {
    chassis({
      content: PACK_TEXT,
      sha256: PACK_DIGEST,
      files: listed(PACK_PATH, 'jpack.json', 'jpack.lock.json')
    })
    drawPack(served(PACK_TEXT), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    await waitFor(() => expect(screen.getByText(SENTENCE)).toBeTruthy())
  })

  it('is absent when it does not', async () => {
    chassis({
      content: PACK_TEXT,
      sha256: PACK_DIGEST,
      files: listed(PACK_PATH, 'jpack.json')
    })
    drawPack(served(PACK_TEXT), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    await waitFor(() => expect(screen.getByRole('toolbar', { name: 'Editing' })).toBeTruthy())
    expect(screen.queryByText(SENTENCE)).toBeNull()
  })
})

describe('what this page will not say', () => {
  it('states no lock, conformance, health or pass-fail verdict anywhere', async () => {
    chassis({
      content: PACK_TEXT,
      sha256: PACK_DIGEST,
      files: listed(PACK_PATH, 'jpack.lock.json')
    })
    const { container } = drawPack(served(PACK_TEXT), { path: EDIT })
    await screen.findByRole('navigation', { name: 'Members' })
    await waitFor(() => expect(screen.getByText(SENTENCE)).toBeTruthy())
    const words = container.textContent ?? ''
    for (const forbidden of [
      'locked',
      'unlocked',
      'out of date',
      'conformant',
      'non-conformant',
      'conformance',
      'healthy',
      'passing',
      'failing',
      'pass/fail',
      'up to date',
      'in the reviewed set'
    ]) {
      expect(words.toLowerCase(), forbidden).not.toContain(forbidden)
    }
    // What it does say is one sentence about the project and one about whose
    // step updating it is.
    expect(words).toContain('Updating it is the project’s own step.')
  })
})
