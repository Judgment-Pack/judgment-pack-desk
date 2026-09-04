/**
 * Moving from one pack to another, on the route that does not remount.
 *
 * `/packs/:packId` is a single element inside the packs layout, so a different
 * pack is a different **parameter** and nothing unmounts — which is deliberate,
 * because it is what lets `?edit` toggle without losing the buffer, the scroll
 * or the selection. The cost is that everything the page holds about a file has
 * to follow the address itself, and the buffer is the one that matters: a
 * buffer seeded once drew pack A under pack B's URL, and its Save sent A's
 * bytes, A's digest and B's path.
 */
import { act, cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { chassis, drawPack, forgetSlot, servedPacks, PACK_DIGEST, PACK_PATH } from './editHarness'
import { forgetAuthorBridge } from '../../shell/authorBridge'

const BRAVO_PATH = 'packs/bravo.pack.json'
const BRAVO_DIGEST = 'b0b0b0'.padEnd(64, '0')

const pack = (id: string, title: string) =>
  `${JSON.stringify(
    {
      specVersion: '0.2.0-draft',
      id: `https://example.invalid/judgment-packs/${id}`,
      version: '1.0.0',
      title,
      decision: { intent: `${title} intent`, question: `${title}?` },
      outcomes: [{ id: 'proceed', label: 'Proceed' }],
      rules: [
        {
          id: `${id}-rule`,
          description: `${title} rule`,
          when: { op: 'literal', value: true },
          outcome: 'proceed',
          onUnknown: 'escalate'
        }
      ]
    },
    null,
    2
  )}\n`

const ALPHA = pack('alpha', 'Alpha pack')
const BRAVO = pack('bravo', 'Bravo pack')

const PACKS = [
  { id: 'alpha', path: PACK_PATH, text: ALPHA, sha256: PACK_DIGEST },
  { id: 'bravo', path: BRAVO_PATH, text: BRAVO, sha256: BRAVO_DIGEST }
]

function bothOnDisk(staleWith?: { sha256: string }) {
  return chassis({
    content: ALPHA,
    sha256: PACK_DIGEST,
    also: { [BRAVO_PATH]: { content: BRAVO, sha256: BRAVO_DIGEST } },
    staleWith
  })
}

afterEach(() => {
  cleanup()
  forgetSlot()
  forgetAuthorBridge()
  vi.unstubAllGlobals()
})

describe('the buffer follows the address', () => {
  it('draws the pack the URL names after moving to another one', async () => {
    bothOnDisk()
    const { router } = drawPack(servedPacks(PACKS), { path: '/packs/alpha?edit=1' })
    await screen.findByDisplayValue('Alpha pack')
    await act(async () => {
      await router.navigate('/packs/bravo?edit=1')
    })
    await waitFor(() => expect(screen.getByDisplayValue('Bravo pack')).toBeTruthy())
    // Not "also Bravo": the first pack's members are gone, rather than a page
    // that says one thing in the strip and shows another in the form.
    expect(screen.queryByDisplayValue('Alpha pack')).toBeNull()
    expect(screen.queryByDisplayValue('Alpha pack rule')).toBeNull()
  })

  it('sends the second pack’s bytes, digest and path when it is saved', async () => {
    const log = bothOnDisk()
    const { router } = drawPack(servedPacks(PACKS), { path: '/packs/alpha?edit=1' })
    await screen.findByDisplayValue('Alpha pack')
    await act(async () => {
      await router.navigate('/packs/bravo?edit=1')
    })
    const title = await screen.findByDisplayValue('Bravo pack')
    fireEvent.change(title, { target: { value: 'Bravo pack, revised' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))
    const written = log.writes[0]!
    // All three, because getting one right and another wrong is the failure:
    // Bravo's path with Alpha's digest is a 409 that blames a file nobody
    // touched, and *Overwrite anyway* then replaces Bravo with Alpha.
    expect(written.path).toBe(BRAVO_PATH)
    expect(written.baseSha256).toBe(BRAVO_DIGEST)
    expect(written.content).toContain('"title": "Bravo pack, revised"')
    expect(written.content).not.toContain('Alpha')
  })

  it('leaves the last write’s refusal on the pack it was about', async () => {
    // A conflict is about one file. Left standing over the next pack it names
    // a file nobody is looking at — and its *Overwrite anyway* would send
    // these bytes to that path.
    bothOnDisk({ sha256: 'c0c0c0'.padEnd(64, '0') })
    vi.stubGlobal('confirm', () => true)
    const { router } = drawPack(servedPacks(PACKS), { path: '/packs/alpha?edit=1' })
    const title = await screen.findByDisplayValue('Alpha pack')
    fireEvent.change(title, { target: { value: 'Alpha pack, edited' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('This file changed since you opened it')
    await act(async () => {
      await router.navigate('/packs/bravo?edit=1')
    })
    await waitFor(() => expect(screen.getByDisplayValue('Bravo pack')).toBeTruthy())
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('leaves nothing of a discarded edit on the pack that follows it', async () => {
    // The blocker asks, and a viewer who says yes is promised the edits are
    // left behind. They were still on screen afterwards, under the new
    // address, with the unsaved dot still up.
    bothOnDisk()
    vi.stubGlobal('confirm', () => true)
    const { router } = drawPack(servedPacks(PACKS), { path: '/packs/alpha?edit=1' })
    const title = await screen.findByDisplayValue('Alpha pack')
    fireEvent.change(title, { target: { value: 'Alpha pack, edited' } })
    await waitFor(() => expect(screen.getByLabelText('unsaved changes')).toBeTruthy())
    await act(async () => {
      await router.navigate('/packs/bravo?edit=1')
    })
    await waitFor(() => expect(screen.getByDisplayValue('Bravo pack')).toBeTruthy())
    expect(screen.queryByDisplayValue('Alpha pack, edited')).toBeNull()
    expect(screen.queryByLabelText('unsaved changes')).toBeNull()
  })
})
