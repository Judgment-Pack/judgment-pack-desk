import { act, cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { connected, renderConnected } from '../testing/harness'
import { AuthorView } from './AuthorView'

/**
 * The authoring shell, driven against a wire-shaped stub of the chassis file
 * API (issue #14, phase 1).
 *
 * The stub answers `fetch` the way the chassis answers it — a JSON body, a
 * status, and a 409 carrying both digests — because that is the shape the
 * client reads. A fixture shaped like the client's own types would test the
 * fixture.
 */

const LOADED = '{\n  "id": "vendor-onboarding"\n}'
const LOADED_SHA = 'a'.repeat(64)
const EDITED = '{\n  "id": "vendor-onboarding",\n  "version": "0.2.0"\n}'
const EDITED_SHA = 'b'.repeat(64)
/** What someone else wrote while this edit was open. */
const THEIRS_SHA = 'c'.repeat(64)

interface Call {
  url: string
  method: string
  body?: Record<string, unknown>
}

/** A chassis whose answers a test writes, recording what it was asked. */
interface Answer {
  status: number
  body: unknown
  /** Resolve to let a deliberately slow answer complete. */
  delay?: Promise<void>
}

function chassis(handlers: {
  files?: () => Answer
  file?: () => Answer
  write?: (body: Record<string, unknown>) => Answer
}) {
  const calls: Call[] = []
  const stub = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : undefined
    calls.push({ url, method, body })

    const answer: Answer =
      method === 'PUT'
        ? (handlers.write ?? ((): Answer => ({ status: 500, body: { error: 'no write stub' } })))(body!)
        : url.startsWith('/api/files')
          ? (handlers.files ?? ((): Answer => ({ status: 200, body: { root: '/project', files: [] } })))()
          : (handlers.file ?? ((): Answer => ({ status: 500, body: { error: 'no read stub' } })))()

    if (answer.delay) await answer.delay
    return new Response(JSON.stringify(answer.body), {
      status: answer.status,
      headers: { 'Content-Type': 'application/json' }
    })
  })
  vi.stubGlobal('fetch', stub)
  return calls
}

const LISTING = {
  root: '/project',
  files: [
    { path: 'jpack.json', bytes: 42, sha256: 'd'.repeat(64) },
    { path: 'packs/vendor-onboarding.pack.json', bytes: LOADED.length, sha256: LOADED_SHA }
  ]
}

const READ = {
  path: 'packs/vendor-onboarding.pack.json',
  bytes: LOADED.length,
  sha256: LOADED_SHA,
  content: LOADED
}

function view() {
  return (
    <Routes>
      <Route path="/author" element={<AuthorView />} />
    </Routes>
  )
}

function render() {
  return renderConnected(view(), connected(), { path: '/author' })
}

/** Open the pack file and wait for its bytes to be in the editor. */
async function openTheFile() {
  fireEvent.click(await screen.findByText('packs/vendor-onboarding.pack.json'))
  return (await screen.findByLabelText('File contents')) as HTMLTextAreaElement
}

beforeEach(() => {
  window.sessionStorage.setItem('jpack-desk-token', 'test-token')
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('the authoring shell', () => {
  it('lists the project files and carries the session token on every call', async () => {
    const calls = chassis({ files: () => ({ status: 200, body: LISTING }) })
    const { container } = render()
    await screen.findByText('jpack.json')
    expect(container.textContent).toContain('packs/vendor-onboarding.pack.json')
    // The chassis refuses an untokened request, so a client that forgot the
    // token would show an empty desk against a working project.
    expect(calls[0]!.url).toContain('token=test-token')
  })

  it('says nothing is modified until the buffer differs from what was loaded', async () => {
    chassis({ files: () => ({ status: 200, body: LISTING }), file: () => ({ status: 200, body: READ }) })
    const { container } = render()
    const box = await openTheFile()
    expect(box.value).toBe(LOADED)
    expect(container.textContent).toContain('saved')
    expect(container.textContent).not.toContain('unsaved changes')

    fireEvent.change(box, { target: { value: EDITED } })
    expect(container.textContent).toContain('unsaved changes')

    // Typed back to what it was: not modified. Dirty state compares to the
    // loaded bytes, not to whether anything was typed.
    fireEvent.change(box, { target: { value: LOADED } })
    expect(container.textContent).not.toContain('unsaved changes')
  })

  it('discards back to the loaded bytes without asking the chassis', async () => {
    const calls = chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 200, body: READ })
    })
    const { container } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })
    const before = calls.length

    fireEvent.click(screen.getByText('Discard changes'))
    expect(box.value).toBe(LOADED)
    expect(container.textContent).not.toContain('unsaved changes')
    expect(calls).toHaveLength(before)
  })

  it('saves the buffer with the digest it loaded, and verifies the read-back', async () => {
    const calls = chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 200, body: READ }),
      write: (body) => ({
        status: 200,
        // The chassis answers with what it read back off the disk.
        body: {
          path: body.path,
          bytes: String(body.content).length,
          sha256: EDITED_SHA,
          content: body.content
        }
      })
    })
    const { container } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })
    fireEvent.click(screen.getByText('Save'))

    await screen.findByText(/Saved, and verified/)
    const write = calls.find((call) => call.method === 'PUT')!
    expect(write.body).toEqual({
      path: 'packs/vendor-onboarding.pack.json',
      content: EDITED,
      // The digest of the bytes this edit started from — not of the buffer.
      baseSha256: LOADED_SHA,
      override: false
    })
    expect(container.textContent).toContain('read it back off the disk')
    expect(container.textContent).toContain(EDITED_SHA.slice(0, 12))
    // Saved: the buffer now matches the bytes on disk.
    expect(container.textContent).not.toContain('unsaved changes')
  })

  it('reports a read-back that is not what was sent, rather than calling it saved', async () => {
    // The write succeeded and the disk holds something else. Saying "saved"
    // here would be a claim about bytes nobody has: the read-back is the only
    // evidence, and it disagrees.
    chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 200, body: READ }),
      write: () => ({
        status: 200,
        body: {
          path: 'packs/vendor-onboarding.pack.json',
          bytes: 3,
          sha256: THEIRS_SHA,
          content: '{}\n'
        }
      })
    })
    const { container } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })
    fireEvent.click(screen.getByText('Save'))

    await screen.findByText(/read-back does not match/)
    expect(container.textContent).not.toContain('Saved, and verified')
  })

  it('shows a stale write with both digests, and writes nothing', async () => {
    const calls = chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 200, body: READ }),
      write: () => ({
        status: 409,
        body: {
          error: 'the file on disk is not the file this edit started from',
          path: 'packs/vendor-onboarding.pack.json',
          expectedSha256: LOADED_SHA,
          actualSha256: THEIRS_SHA,
          exists: true
        }
      })
    })
    const { container } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })
    fireEvent.click(screen.getByText('Save'))

    await screen.findByText(/the file changed since you opened it/)
    // Both digests, so the user can see what they had and what is there.
    expect(container.textContent).toContain(`this edit started from`)
    expect(container.textContent).toContain(LOADED_SHA.slice(0, 12))
    expect(container.textContent).toContain('on disk now')
    expect(container.textContent).toContain(THEIRS_SHA.slice(0, 12))
    // The edit is not lost, and nothing claims to have been saved.
    expect(box.value).toBe(EDITED)
    expect(container.textContent).not.toContain('Saved, and verified')
    expect(calls.filter((call) => call.method === 'PUT')).toHaveLength(1)
  })

  it('distinguishes a file that was deleted from one that was changed', async () => {
    chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 200, body: READ }),
      write: () => ({
        status: 409,
        body: {
          error: 'the file on disk is not the file this edit started from',
          path: 'packs/vendor-onboarding.pack.json',
          expectedSha256: LOADED_SHA,
          actualSha256: '',
          exists: false
        }
      })
    })
    const { container } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })
    fireEvent.click(screen.getByText('Save'))

    await screen.findByText(/no longer on disk/)
    expect(container.textContent).toContain('(no file)')
  })

  it('overwrites only when the user asks for it, and says so on the wire', async () => {
    let attempt = 0
    const calls = chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 200, body: READ }),
      write: (body) => {
        attempt += 1
        if (attempt === 1) {
          return {
            status: 409,
            body: {
              error: 'stale',
              path: 'packs/vendor-onboarding.pack.json',
              expectedSha256: LOADED_SHA,
              actualSha256: THEIRS_SHA,
              exists: true
            }
          }
        }
        return {
          status: 200,
          body: { path: body.path, bytes: 1, sha256: EDITED_SHA, content: body.content }
        }
      }
    })
    const { container } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })
    fireEvent.click(screen.getByText('Save'))
    await screen.findByText(/the file changed since you opened it/)

    // The first attempt did not carry override; overwriting is a second,
    // deliberate act.
    const writes = () => calls.filter((call) => call.method === 'PUT')
    expect(writes()[0]!.body!.override).toBe(false)

    fireEvent.click(screen.getByText('Overwrite anyway'))
    await screen.findByText(/Saved, and verified/)
    expect(writes()).toHaveLength(2)
    expect(writes()[1]!.body!.override).toBe(true)
    expect(container.textContent).not.toContain('the file changed since you opened it')
  })

  it('surfaces a containment refusal as the chassis states it', async () => {
    chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 403, body: { error: 'path is not inside the project' } })
    })
    render()
    fireEvent.click(await screen.findByText('packs/vendor-onboarding.pack.json'))
    await screen.findByText(/Could not read/)
    expect(await screen.findByText(/path is not inside the project/)).toBeTruthy()
  })

  it('reloads from disk and drops the buffer', async () => {
    let reads = 0
    chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => {
        reads += 1
        return {
          status: 200,
          body: reads === 1 ? READ : { ...READ, content: '{"id":"reloaded"}', sha256: THEIRS_SHA }
        }
      }
    })
    const { container } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })
    expect(container.textContent).toContain('unsaved changes')

    fireEvent.click(screen.getByText('Reload from disk'))
    await waitFor(() =>
      expect((screen.getByLabelText('File contents') as HTMLTextAreaElement).value).toBe(
        '{"id":"reloaded"}'
      )
    )
    expect(container.textContent).not.toContain('unsaved changes')
  })
})

describe('the authoring shell, against the desk invalidating its own queries', () => {
  /**
   * The watcher makes the desk invalidate every active query on any file
   * change. These drive that for real — `queryClient.invalidateQueries()` with
   * no key, exactly as `McpProvider` calls it — because the failure being
   * guarded against is precisely that a background refetch quietly moves the
   * bytes an open edit is measured against.
   */
  it('does not rebase an open edit when the file changes on disk', async () => {
    let reads = 0
    const calls = chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => {
        reads += 1
        // The second read is what the watcher-driven refetch gets back: the
        // file changed underneath the edit.
        return reads === 1
          ? { status: 200, body: READ }
          : { status: 200, body: { ...READ, content: '{"id":"theirs"}', sha256: THEIRS_SHA } }
      },
      write: (body) => ({
        status: 200,
        body: { path: body.path, bytes: 1, sha256: EDITED_SHA, content: body.content }
      })
    })
    const { container, queryClient } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })

    // The desk's own invalidation, as the file watcher triggers it.
    await act(async () => {
      void queryClient.invalidateQueries()
      await Promise.resolve()
    })
    await screen.findByText(/changed on disk since you opened it/)

    // The buffer is untouched and still dirty.
    expect((screen.getByLabelText('File contents') as HTMLTextAreaElement).value).toBe(EDITED)
    expect(container.textContent).toContain('unsaved changes')

    // And the save still carries the digest of the bytes the edit started from,
    // so the chassis can refuse it. A rebased base would have sent THEIRS_SHA
    // and overwritten a change the user never saw.
    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(calls.some((call) => call.method === 'PUT')).toBe(true))
    expect(calls.find((call) => call.method === 'PUT')!.body!.baseSha256).toBe(LOADED_SHA)
  })

  it('keeps a dirty buffer when the file is deleted underneath it', async () => {
    let listings = 0
    chassis({
      files: () => {
        listings += 1
        return listings === 1
          ? { status: 200, body: LISTING }
          : { status: 200, body: { ...LISTING, files: [LISTING.files[0]] } }
      },
      file: () => (listings <= 1 ? { status: 200, body: READ } : { status: 404, body: { error: 'no such file in the project' } })
    })
    const { container, queryClient } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })

    await act(async () => {
      void queryClient.invalidateQueries()
      await Promise.resolve()
    })
    await screen.findByText(/no longer in the project/)

    // The editor is still mounted and the edit survives — the whole point.
    expect((screen.getByLabelText('File contents') as HTMLTextAreaElement).value).toBe(EDITED)
    expect(container.textContent).toContain('unsaved changes')
  })
})

describe('the authoring shell, saving', () => {
  it('verifies against the bytes it sent, not the bytes in the box', async () => {
    // Typing after a successful save must not turn "verified" into a false
    // "does not match": the claim is about what was written, and what was
    // written is fixed at the moment of the request.
    chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 200, body: READ }),
      write: (body) => ({
        status: 200,
        body: { path: body.path, bytes: 1, sha256: EDITED_SHA, content: body.content }
      })
    })
    const { container } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })
    fireEvent.click(screen.getByText('Save'))
    await screen.findByText(/Saved, and verified/)

    fireEvent.change(box, { target: { value: EDITED + '\n// more' } })
    expect(container.textContent).toContain('Saved, and verified')
    expect(container.textContent).not.toContain('read-back does not match')
    // And the new typing is unsaved work again, measured against what landed.
    expect(container.textContent).toContain('unsaved changes')
  })

  it('holds its verdict while a slow write is in flight, and does not let a reload race it', async () => {
    let settle: (() => void) | undefined
    chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 200, body: READ }),
      write: (body) => ({
        status: 200,
        body: { path: body.path, bytes: 1, sha256: EDITED_SHA, content: body.content },
        delay: new Promise<void>((resolve) => {
          settle = resolve
        })
      })
    })
    const { container } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })
    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => expect(screen.getByText('Saving…')).toBeTruthy())
    // Reload cannot be taken while the PUT is unresolved: it is not cancellable,
    // and a reload would set a base the in-flight save is about to supersede.
    expect((screen.getByText('Reload from disk') as HTMLButtonElement).disabled).toBe(true)

    // Typing during the write does not change what the write will be judged on.
    fireEvent.change(box, { target: { value: EDITED + '\n// typed during the save' } })
    await act(async () => {
      settle!()
      await Promise.resolve()
    })
    await screen.findByText(/Saved, and verified/)
    expect(container.textContent).not.toContain('read-back does not match')
  })

  it('sends text that is not JSON without complaint, because the runtime is the judge', async () => {
    // Phase 1 edits bytes. A desk that refused to save invalid JSON would be
    // making a judgement the runtime is there to make — and would make a
    // half-finished document impossible to save at all.
    const calls = chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 200, body: READ }),
      write: (body) => ({
        status: 200,
        body: { path: body.path, bytes: 1, sha256: EDITED_SHA, content: body.content }
      })
    })
    const broken = '{ "id": "vendor-onboarding", '
    const { container } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: broken } })
    fireEvent.click(screen.getByText('Save'))

    await screen.findByText(/Saved, and verified/)
    expect(calls.find((call) => call.method === 'PUT')!.body!.content).toBe(broken)
    expect(container.textContent).not.toContain('not valid JSON')
  })
})

describe('the authoring shell, not losing an edit', () => {
  it('asks before opening another file over unsaved changes', async () => {
    chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 200, body: READ })
    })
    const confirm = vi.fn(() => false)
    vi.stubGlobal('confirm', confirm)

    const { container } = render()
    const box = await openTheFile()
    fireEvent.change(box, { target: { value: EDITED } })

    fireEvent.click(screen.getByText('jpack.json'))
    expect(confirm).toHaveBeenCalledOnce()
    // Refused, so the edit is still open and still here.
    expect((screen.getByLabelText('File contents') as HTMLTextAreaElement).value).toBe(EDITED)
    expect(container.textContent).toContain('unsaved changes')

    confirm.mockReturnValue(true)
    fireEvent.click(screen.getByText('jpack.json'))
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(2))
  })

  it('does not ask when nothing is unsaved', async () => {
    chassis({
      files: () => ({ status: 200, body: LISTING }),
      file: () => ({ status: 200, body: READ })
    })
    const confirm = vi.fn(() => true)
    vi.stubGlobal('confirm', confirm)

    render()
    await openTheFile()
    fireEvent.click(screen.getByText('jpack.json'))
    expect(confirm).not.toHaveBeenCalled()
  })
})
