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
import { act, cleanup, fireEvent, screen, waitFor, within as inside } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  chassis,
  drawPack,
  forgetSlot,
  servedPacks,
  CLEAN_REPORT,
  PACK_DIGEST,
  PACK_PATH
} from './editHarness'
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
/** Alpha again, with a `fact` operand — the one control that holds a draft. */
const ALPHA_FACT = `${JSON.stringify(
  {
    ...(JSON.parse(ALPHA) as Record<string, unknown>),
    rules: [
      {
        id: 'alpha-rule',
        description: 'Alpha pack rule',
        when: { op: 'fact', path: '/request/amount', operator: 'equals', value: '5000' },
        outcome: 'proceed',
        onUnknown: 'escalate'
      }
    ]
  },
  null,
  2
)}\n`
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

describe('the gap between the two answers', () => {
  it('draws no pack and saves nothing while the buffer is another file’s', async () => {
    // **`get_pack` and the file read do not land together.** `path` moves the
    // moment the runtime answers about Bravo, and the buffer is still Alpha
    // until Bravo's bytes arrive — a window in which the page drew Alpha's
    // document under Bravo's address and Save combined Bravo's path with
    // Alpha's bytes and Alpha's digest. A 409 catches that only where the
    // digests differ, and the 409 itself offers *Overwrite anyway*.
    const log = chassis({
      content: ALPHA,
      sha256: PACK_DIGEST,
      also: { [BRAVO_PATH]: { content: BRAVO, sha256: BRAVO_DIGEST } },
      hold: [BRAVO_PATH]
    })
    const { router } = drawPack(servedPacks(PACKS), { path: '/packs/alpha?edit=1' })
    const title = await screen.findByDisplayValue('Alpha pack')
    fireEvent.change(title, { target: { value: 'Alpha pack, revised' } })

    vi.stubGlobal('confirm', () => true)
    await act(async () => {
      await router.navigate('/packs/bravo?edit=1')
    })

    // Alpha is gone from the page the moment the address is not Alpha's…
    await waitFor(() => expect(screen.queryByDisplayValue('Alpha pack, revised')).toBeNull())
    // …and a save in the gap writes nothing at all, by either route.
    fireEvent.keyDown(document, { key: 's', ctrlKey: true })
    const save = screen.queryByRole('button', { name: 'Save' })
    if (save !== null) fireEvent.click(save)
    await act(async () => {})
    expect(log.writes).toHaveLength(0)

    // And when Bravo's bytes do arrive, this is Bravo's editor.
    log.release(BRAVO_PATH)
    const bravo = await screen.findByDisplayValue('Bravo pack')
    fireEvent.change(bravo, { target: { value: 'Bravo pack, revised' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))
    expect(log.writes[0]!.path).toBe(BRAVO_PATH)
    expect(log.writes[0]!.baseSha256).toBe(BRAVO_DIGEST)
    expect(log.writes[0]!.content).not.toContain('Alpha')
  })

  it('comes back to the first pack as the first pack, not as the second', async () => {
    // A→B→A, which is the ordinary shape of following a link and pressing Back.
    const log = chassis({
      content: ALPHA,
      sha256: PACK_DIGEST,
      also: { [BRAVO_PATH]: { content: BRAVO, sha256: BRAVO_DIGEST } }
    })
    const { router } = drawPack(servedPacks(PACKS), { path: '/packs/alpha?edit=1' })
    await screen.findByDisplayValue('Alpha pack')
    await act(async () => {
      await router.navigate('/packs/bravo?edit=1')
    })
    await screen.findByDisplayValue('Bravo pack')
    await act(async () => {
      await router.navigate('/packs/alpha?edit=1')
    })
    const back = await screen.findByDisplayValue('Alpha pack')
    fireEvent.change(back, { target: { value: 'Alpha pack, again' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))
    expect(log.writes[0]!.path).toBe(PACK_PATH)
    expect(log.writes[0]!.baseSha256).toBe(PACK_DIGEST)
    expect(log.writes[0]!.content).toContain('Alpha pack, again')
  })
})

describe('a path that moves under one address', () => {
  it('holds the new file rather than replacing unsaved work, and sends nothing to it', async () => {
    // Nobody navigated: the listing re-answers, or `get_pack` names a different
    // file, and `path` moves under an address that has not changed. Seeding on
    // that replaced an author's edits with another document's bytes — and until
    // it did, Save was sending *these* bytes to *that* path.
    let servedPath = PACK_PATH
    const log = chassis({
      content: ALPHA,
      sha256: PACK_DIGEST,
      also: { [BRAVO_PATH]: { content: BRAVO, sha256: BRAVO_DIGEST } }
    })
    const handlers = {
      get_pack: () => ({
        text: ALPHA,
        structured: { path: servedPath, bytes: ALPHA.length, sha256: PACK_DIGEST }
      }),
      list_packs: () => ({
        text: JSON.stringify({ packs: [{ id: 'alpha', path: servedPath }] })
      }),
      validate: () => ({ text: CLEAN_REPORT })
    }
    const { queryClient } = drawPack(handlers, { path: '/packs/alpha?edit=1' })
    const title = await screen.findByDisplayValue('Alpha pack')
    fireEvent.change(title, { target: { value: 'Alpha pack, revised' } })

    servedPath = BRAVO_PATH
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['get_pack'] })
    })

    // The other file is an offer, and the edit is neither drawn nor taken: the
    // page will not show one file's bytes under another file's address, and it
    // will not throw them away to show the new one.
    await waitFor(() =>
      expect(screen.getByText(/This page is now about a different file/)).toBeTruthy()
    )
    expect(screen.queryByDisplayValue('Alpha pack, revised')).toBeNull()

    // A save writes nothing: these bytes belong to a path this page is no
    // longer about.
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await act(async () => {})
    expect(log.writes).toHaveLength(0)

    // And the work is still there when the address is about that file again.
    servedPath = PACK_PATH
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['get_pack'] })
    })
    await waitFor(() => expect(screen.getByDisplayValue('Alpha pack, revised')).toBeTruthy())
  })

  it('holds a file that would replace unwritten text, and keeps the text', async () => {
    // **Unwritten operand text is work the buffer cannot see.** With only a
    // draft and no byte change the buffer adopted the other document, and the
    // route had already thrown the draft away the moment the path moved — so
    // the work went with nothing having asked about it.
    let servedPath = PACK_PATH
    chassis({
      content: ALPHA_FACT,
      sha256: PACK_DIGEST,
      also: { [BRAVO_PATH]: { content: BRAVO, sha256: BRAVO_DIGEST } }
    })
    const handlers = {
      get_pack: () => ({
        text: servedPath === PACK_PATH ? ALPHA_FACT : BRAVO,
        structured: {
          path: servedPath,
          bytes: ALPHA_FACT.length,
          sha256: servedPath === PACK_PATH ? PACK_DIGEST : BRAVO_DIGEST
        }
      }),
      list_packs: () => ({
        text: JSON.stringify({ packs: [{ id: 'alpha', path: servedPath }] })
      }),
      validate: () => ({ text: CLEAN_REPORT })
    }
    const { queryClient } = drawPack(handlers, { path: '/packs/alpha?edit=1' })
    await screen.findByDisplayValue('Alpha pack')

    const operand = inside(document.getElementById('/rules/0/when/value')!).getByDisplayValue(
      '"5000"'
    )
    fireEvent.change(operand, { target: { value: '{"shade"' } })
    await waitFor(() => expect(screen.getByText('1 field is not written yet')).toBeTruthy())
    // The bytes have not moved: this is work and it is not dirtiness.
    expect(screen.queryByLabelText('unsaved changes')).toBeNull()

    servedPath = BRAVO_PATH
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['get_pack'] })
    })

    await waitFor(() =>
      expect(screen.getByText(/This page is now about a different file/)).toBeTruthy()
    )
    // Held, and the text is still there to come back to.
    expect(screen.getByText('1 field is not written yet')).toBeTruthy()

    // Taking the offer is what spends it.
    fireEvent.click(screen.getByRole('button', { name: 'Open it and lose these changes' }))
    await waitFor(() => expect(screen.getByDisplayValue('Bravo pack')).toBeTruthy())
    expect(screen.queryByText('1 field is not written yet')).toBeNull()
  })

  it('forgets an offer the address has come back from', async () => {
    // A→B→A left B waiting behind the page, and its offer — "open it and lose
    // these changes" — was still on screen: pressing it discarded a dirty A for
    // a document the address is not about.
    let servedPath = PACK_PATH
    chassis({
      content: ALPHA,
      sha256: PACK_DIGEST,
      also: { [BRAVO_PATH]: { content: BRAVO, sha256: BRAVO_DIGEST } }
    })
    const handlers = {
      get_pack: () => ({
        text: servedPath === PACK_PATH ? ALPHA : BRAVO,
        structured: {
          path: servedPath,
          bytes: ALPHA.length,
          sha256: servedPath === PACK_PATH ? PACK_DIGEST : BRAVO_DIGEST
        }
      }),
      list_packs: () => ({
        text: JSON.stringify({ packs: [{ id: 'alpha', path: servedPath }] })
      }),
      validate: () => ({ text: CLEAN_REPORT })
    }
    const { queryClient } = drawPack(handlers, { path: '/packs/alpha?edit=1' })
    const title = await screen.findByDisplayValue('Alpha pack')
    fireEvent.change(title, { target: { value: 'Alpha pack, revised' } })

    servedPath = BRAVO_PATH
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['get_pack'] })
    })
    await screen.findByText(/This page is now about a different file/)

    servedPath = PACK_PATH
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['get_pack'] })
    })
    await waitFor(() => expect(screen.getByDisplayValue('Alpha pack, revised')).toBeTruthy())
    // The offer is gone with the address that produced it.
    expect(screen.queryByText(/This page is now about a different file/)).toBeNull()
    expect(
      screen.queryByRole('button', { name: 'Open it and lose these changes' })
    ).toBeNull()
  })

  it('takes the other file only when the offer is accepted', async () => {
    let servedPath = PACK_PATH
    chassis({
      content: ALPHA,
      sha256: PACK_DIGEST,
      also: { [BRAVO_PATH]: { content: BRAVO, sha256: BRAVO_DIGEST } }
    })
    const handlers = {
      get_pack: () => ({
        text: servedPath === PACK_PATH ? ALPHA : BRAVO,
        structured: {
          path: servedPath,
          bytes: ALPHA.length,
          sha256: servedPath === PACK_PATH ? PACK_DIGEST : BRAVO_DIGEST
        }
      }),
      list_packs: () => ({
        text: JSON.stringify({ packs: [{ id: 'alpha', path: servedPath }] })
      }),
      validate: () => ({ text: CLEAN_REPORT })
    }
    const { queryClient } = drawPack(handlers, { path: '/packs/alpha?edit=1' })
    const title = await screen.findByDisplayValue('Alpha pack')
    fireEvent.change(title, { target: { value: 'Alpha pack, revised' } })

    servedPath = BRAVO_PATH
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: ['get_pack'] })
    })
    const take = await screen.findByRole('button', { name: 'Open it and lose these changes' })
    fireEvent.click(take)
    await waitFor(() => expect(screen.getByDisplayValue('Bravo pack')).toBeTruthy())
    expect(screen.queryByText(/This page is now about a different file/)).toBeNull()
  })
})

describe('a read that lands after the page has moved on', () => {
  it('does not put pack A into pack B, and names A when it fails', async () => {
    // **A reload is a read that takes as long as it takes.** Ask for one on A,
    // navigate to B, edit B — and when A's read lands, the buffer took it: B's
    // dirty bytes became A's clean ones, with no undo and nothing having asked.
    const log = chassis({
      content: ALPHA,
      sha256: PACK_DIGEST,
      also: { [BRAVO_PATH]: { content: BRAVO, sha256: BRAVO_DIGEST } },
      staleWith: { sha256: 'c0c0c0'.padEnd(64, '0') }
    })
    vi.stubGlobal('confirm', () => true)
    const { router } = drawPack(servedPacks(PACKS), { path: '/packs/alpha?edit=1' })

    // A conflict on A, which is what puts Reload on screen.
    const alpha = await screen.findByDisplayValue('Alpha pack')
    fireEvent.change(alpha, { target: { value: 'Alpha pack, revised' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    const alert = await screen.findByRole('alert')

    // The read for it is held, and the page leaves while it is in the air.
    log.hold(PACK_PATH)
    fireEvent.click(inside(alert).getByRole('button', { name: 'Reload' }))
    await act(async () => {
      await router.navigate('/packs/bravo?edit=1')
    })
    const bravo = await screen.findByDisplayValue('Bravo pack')
    fireEvent.change(bravo, { target: { value: 'Bravo pack, revised' } })

    log.release(PACK_PATH)
    await act(async () => {})

    // B is still B, still dirty, and Alpha is nowhere on this page.
    expect(screen.getByDisplayValue('Bravo pack, revised')).toBeTruthy()
    expect(screen.queryByDisplayValue('Alpha pack')).toBeNull()
    expect(screen.getByLabelText('unsaved changes')).toBeTruthy()
  })

  it('names the file a failed reload was for, not the one on screen', async () => {
    const log = chassis({
      content: ALPHA,
      sha256: PACK_DIGEST,
      also: { [BRAVO_PATH]: { content: BRAVO, sha256: BRAVO_DIGEST } },
      staleWith: { sha256: 'c0c0c0'.padEnd(64, '0') }
    })
    drawPack(servedPacks(PACKS), { path: '/packs/alpha?edit=1' })
    const alpha = await screen.findByDisplayValue('Alpha pack')
    fireEvent.change(alpha, { target: { value: 'Alpha pack, revised' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    const alert = await screen.findByRole('alert')
    log.breakReads()
    fireEvent.click(inside(alert).getByRole('button', { name: 'Reload' }))
    await waitFor(() => expect(screen.getByText(`Could not reload ${PACK_PATH}`)).toBeTruthy())
  })

  it('keeps saving after a write is left in flight by a navigation', async () => {
    // `write.reset()` on a path change detaches the mutation's observer, so a
    // per-mutation `onSettled` never arrived: the single-flight latch was held
    // for ever and every later Save returned silently — on every pack.
    const log = chassis({
      content: ALPHA,
      sha256: PACK_DIGEST,
      also: { [BRAVO_PATH]: { content: BRAVO, sha256: BRAVO_DIGEST } },
      holdWrite: true
    })
    vi.stubGlobal('confirm', () => true)
    const { router } = drawPack(servedPacks(PACKS), { path: '/packs/alpha?edit=1' })
    const alpha = await screen.findByDisplayValue('Alpha pack')
    fireEvent.change(alpha, { target: { value: 'Alpha pack, revised' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))

    await act(async () => {
      await router.navigate('/packs/bravo?edit=1')
    })
    await screen.findByDisplayValue('Bravo pack')
    await act(async () => {
      await router.navigate('/packs/alpha?edit=1')
    })
    log.releaseWrite()
    await act(async () => {})

    const back = await screen.findByDisplayValue('Alpha pack')
    fireEvent.change(back, { target: { value: 'Alpha pack, again' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    await waitFor(() => expect(log.writes).toHaveLength(2))
    expect(log.writes[1]!.content).toContain('Alpha pack, again')
  })
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
