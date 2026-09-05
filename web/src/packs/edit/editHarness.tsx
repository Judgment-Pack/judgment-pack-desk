/**
 * The pack route, standing up, with the chassis and the runtime both stubbed.
 *
 * The route reads five things — `get_pack`, `list_packs`, `validate`, the file
 * API and the Inspector slot — and every case about the editor needs all five,
 * so they are wired once here rather than four times across four suites. It is
 * `components/extractionFixtures.ts`'s idiom: test support that is not itself a
 * test, kept beside the thing it supports.
 *
 * The route is mounted at `/packs/:packId` on a **data** router, because
 * `useDirtyGuard` calls `useBlocker` and that hook exists only there — a
 * harness on the older router would let the guard pass here and throw in the
 * page.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { Link, RouterProvider, createMemoryRouter } from 'react-router-dom'
import { vi } from 'vitest'
import { McpContext, type McpConnection } from '../../mcp/McpProvider'
import { InspectorSlotContext, type InspectorSlot } from '../../shell/InspectorSlot'
import { connected, stubClient, testQueryClient, type ToolHandler } from '../../testing/harness'
import { PackView } from '../../routes/PackView'

export const PACK_PATH = 'packs/vendor-onboarding.pack.json'
export const PACK_DIGEST = 'a1b2c3'.padEnd(64, '0')

export const CLEAN_REPORT = JSON.stringify({
  outputVersion: '2',
  status: 'valid',
  specVersion: '0.2.0-draft',
  layers: [
    { name: 'carrier', status: 'passed' },
    { name: 'structural', status: 'passed' },
    { name: 'semantic', status: 'passed' }
  ],
  diagnostics: [],
  diagnosticsTruncated: false
})

/** What the chassis answered, and what it was asked. */
export interface ChassisLog {
  writes: {
    path: string
    content: string
    baseSha256: string
    override: boolean
    createParents: boolean
  }[]
  reads: number
  /**
   * Answer a read this chassis was told to hold.
   *
   * A file read that answers immediately cannot express the gap the route
   * lives in: `get_pack(B)` returns and the page knows B's path while the
   * buffer is still A's, which is where a save used to send A's bytes to B.
   */
  release: (path: string) => void
  /**
   * Hold this path's reads from here on, and hand back the release.
   *
   * Each call replaces the gate, so two reads can be held separately and
   * answered **out of order** — which is the only way to ask what a page does
   * when an earlier read lands after a later one.
   */
  hold: (path: string) => () => void
  /** Answer the write this chassis was told to hold. */
  releaseWrite: () => void
  /** Refuse every read from here on, which is what a reload has to survive. */
  breakReads: () => void
  /** Move the file on disk, as something outside this page would. */
  write: (path: string, content: string) => void
}

/**
 * The chassis, as a fetch stub.
 *
 * `disk` is mutable so a case can move the file underneath an open editor,
 * which is the whole of the stale-write story. `staleWith` refuses the next
 * write with a 409 carrying both digests, exactly as the chassis does.
 *
 * **A read is answered by path.** It used to answer every read with the one
 * file whatever was asked for, which is a chassis that cannot tell two files
 * apart — so a case navigating between packs was measuring the stub rather
 * than the page. `also` carries the other files a case needs.
 */
export function chassis(options: {
  content: string
  sha256: string
  /** Other files on this disk, by path. */
  also?: Record<string, { content: string; sha256: string }>
  files?: { path: string; bytes: number; sha256: string }[]
  /** Paths whose reads wait until the case releases them. */
  hold?: string[]
  /** Hold the write until the case releases it, so a case can type during a PUT. */
  holdWrite?: boolean
  /** Refuse the write with something that is not a conflict. */
  failWrite?: { status: number; error: string }
  /** What the disk holds afterwards, where that is not what was sent. */
  landsAs?: (content: string) => string
  /** Refuse the next write with a 409, and say what is on disk now. */
  staleWith?: { sha256: string; exists?: boolean }
}): ChassisLog {
  const gates = new Map<string, { wait: Promise<void>; open: () => void }>()
  for (const path of options.hold ?? []) {
    let open = () => {}
    const wait = new Promise<void>((resolve) => {
      open = resolve
    })
    gates.set(path, { wait, open })
  }
  let openWrite = () => {}
  const writeGate = new Promise<void>((resolve) => {
    openWrite = resolve
  })
  let readsBroken = false
  const gateFor = (path: string) => {
    let open = () => {}
    const wait = new Promise<void>((resolve) => {
      open = resolve
    })
    gates.set(path, { wait, open })
    return open
  }
  const log: ChassisLog = {
    writes: [],
    reads: 0,
    hold: (path) => gateFor(path),
    release: (path) => gates.get(path)?.open(),
    releaseWrite: () => openWrite(),
    breakReads: () => {
      readsBroken = true
    },
    write: (path, content) => {
      disk.set(path, { content, sha256: `${content.length}`.padEnd(64, 'e') })
    }
  }
  const disk = new Map<string, { content: string; sha256: string }>([
    [PACK_PATH, { content: options.content, sha256: options.sha256 }],
    ...Object.entries(options.also ?? {})
  ])
  const files =
    options.files ??
    [...disk].map(([path, held]) => ({
      path,
      bytes: held.content.length,
      sha256: held.sha256
    }))
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const address = String(url)
    if (address.includes('/api/files?')) {
      return ok({ root: '/project', files })
    }
    if (address.includes('/api/file?') && init?.method === undefined) {
      log.reads += 1
      const wanted = decodeURIComponent(/[?&]path=([^&]*)/.exec(address)?.[1] ?? '')
      const gate = gates.get(wanted)
      // **The answer is the file as it was when the read was made.** Held until
      // the case says so, but not *read* then: a request that is answered late
      // is answering about the bytes it saw, which is the whole of what makes a
      // late answer wrong.
      const snapshot = disk.get(wanted)
      if (gate !== undefined) {
        await gate.wait
        if (snapshot !== undefined && !readsBroken) {
          return ok({
            path: wanted,
            bytes: snapshot.content.length,
            sha256: snapshot.sha256,
            content: snapshot.content
          })
        }
      }
      if (readsBroken) {
        return {
          ok: false,
          status: 500,
          statusText: 'Internal Server Error',
          text: async () => JSON.stringify({ error: 'the chassis could not read the file' })
        }
      }
      const held = disk.get(wanted)
      if (held === undefined) {
        return {
          ok: false,
          status: 404,
          statusText: 'Not Found',
          text: async () => JSON.stringify({ error: 'no such file' })
        }
      }
      return ok({
        path: wanted,
        bytes: held.content.length,
        sha256: held.sha256,
        content: held.content
      })
    }
    if (address.includes('/api/file') && init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as {
        path: string
        content: string
        baseSha256: string
        override: boolean
        createParents: boolean
      }
      log.writes.push(body)
      if (options.holdWrite === true) await writeGate
      if (options.failWrite !== undefined) {
        return {
          ok: false,
          status: options.failWrite.status,
          statusText: 'Error',
          text: async () => JSON.stringify({ error: options.failWrite!.error })
        }
      }
      if (options.staleWith !== undefined && !body.override) {
        return {
          ok: false,
          status: 409,
          statusText: 'Conflict',
          text: async () =>
            JSON.stringify({
              error: 'the file on disk is not the file this edit started from',
              code: options.staleWith!.exists === false ? 'exists' : 'stale',
              path: body.path,
              expectedSha256: body.baseSha256,
              actualSha256: options.staleWith!.sha256,
              exists: options.staleWith!.exists ?? true
            })
        }
      }
      const written = options.landsAs === undefined ? body.content : options.landsAs(body.content)
      const landed = {
        content: written,
        sha256: `${written.length}`.padEnd(64, 'f')
      }
      disk.set(body.path, landed)
      return ok({
        path: body.path,
        bytes: landed.content.length,
        sha256: landed.sha256,
        content: landed.content
      })
    }
    return {
      ok: false,
      status: 404,
      statusText: 'Not Found',
      text: async () => JSON.stringify({ error: 'no such file' })
    }
  })
  return log
}

function ok(body: unknown) {
  return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify(body) }
}

let slotTarget: HTMLElement | null = null

/** Tear down the portal target a case with an Inspector created. */
export function forgetSlot(): void {
  slotTarget?.remove()
  slotTarget = null
}

function slotFor(open: boolean, tab: string | null, revealed: string[]): InspectorSlot {
  if (open && slotTarget === null) {
    slotTarget = document.createElement('div')
    document.body.appendChild(slotTarget)
  }
  return {
    open,
    size: 0,
    tab,
    setTab: () => {},
    target: open ? slotTarget : null,
    claim: () => () => {},
    reveal: () => revealed.push('reveal')
  }
}

/** Draw the pack route, at one address, with one set of tool answers. */
export function drawPack(
  handlers: Record<string, ToolHandler>,
  options: {
    path?: string
    connection?: Partial<McpConnection>
    inspector?: boolean
    tab?: string | null
    /** An in-app link, so a case can drive same-document navigation. */
    nav?: boolean
  } = {}
) {
  const stub = stubClient(handlers)
  const revealed: string[] = []
  const Mounted = ({ children }: { children?: ReactNode }) => {
    const [open] = useState(options.inspector === true)
    return (
      <McpContext.Provider
        value={connected({ client: stub.client, validateSupported: true, ...options.connection })}
      >
        <InspectorSlotContext.Provider value={slotFor(open, options.tab ?? null, revealed)}>
          {children}
          <PackView />
        </InspectorSlotContext.Provider>
      </McpContext.Provider>
    )
  }
  const router = createMemoryRouter(
    [
      {
        path: '/packs/:packId',
        element: <Mounted>{options.nav && <Link to="/elsewhere">go elsewhere</Link>}</Mounted>
      },
      { path: '/elsewhere', element: <p>elsewhere</p> }
    ],
    { initialEntries: [options.path ?? '/packs/vendor-onboarding'] }
  )
  const queryClient = testQueryClient()
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return { ...view, router, queryClient, calls: stub.calls, revealed }
}

/** The tool answers a pack route needs, for one document. */
export function served(text: string, report = CLEAN_REPORT): Record<string, ToolHandler> {
  return {
    get_pack: () => ({
      text,
      structured: { path: PACK_PATH, bytes: text.length, sha256: PACK_DIGEST }
    }),
    // `list_packs` is read from the **text** block, like every other inventory.
    list_packs: () => ({
      text: JSON.stringify({
        packs: [
          {
            id: 'vendor-onboarding',
            path: PACK_PATH,
            consultedFactPaths: ['/request/amount', '/request/type'],
            evidenceRequirements: ['screening-report', 'insurance-cert']
          }
        ]
      })
    }),
    validate: () => ({ text: report })
  }
}

/** One pack the runtime serves, for a case that needs more than one. */
export interface ServedPack {
  id: string
  path: string
  text: string
  sha256: string
}

/**
 * Two or more packs, answered by id.
 *
 * `/packs/:packId` is one element inside a layout route, so moving between
 * packs changes the parameter without remounting anything — which is exactly
 * the case a single-pack stub cannot express.
 */
export function servedPacks(
  packs: readonly ServedPack[],
  report = CLEAN_REPORT
): Record<string, ToolHandler> {
  const find = (args: Record<string, unknown>) =>
    packs.find((entry) => entry.id === args.pack_id) ?? packs[0]!
  return {
    get_pack: (args) => {
      const wanted = find(args)
      return {
        text: wanted.text,
        structured: { path: wanted.path, bytes: wanted.text.length, sha256: wanted.sha256 }
      }
    },
    list_packs: () => ({
      text: JSON.stringify({
        packs: packs.map((entry) => ({ id: entry.id, path: entry.path }))
      })
    }),
    validate: () => ({ text: report })
  }
}
