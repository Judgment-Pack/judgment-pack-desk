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
}

/**
 * The chassis, as a fetch stub.
 *
 * `disk` is mutable so a case can move the file underneath an open editor,
 * which is the whole of the stale-write story. `staleOnce` refuses the next
 * write with a 409 carrying both digests, exactly as the chassis does.
 */
export function chassis(options: {
  content: string
  sha256: string
  files?: { path: string; bytes: number; sha256: string }[]
  /** Refuse the next write with a 409, and say what is on disk now. */
  staleWith?: { sha256: string; exists?: boolean }
}): ChassisLog {
  const log: ChassisLog = { writes: [], reads: 0 }
  const disk = { content: options.content, sha256: options.sha256 }
  const files = options.files ?? [
    { path: PACK_PATH, bytes: options.content.length, sha256: options.sha256 }
  ]
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const address = String(url)
    if (address.includes('/api/files?')) {
      return ok({ root: '/project', files })
    }
    if (address.includes('/api/file?') && init?.method === undefined) {
      log.reads += 1
      return ok({
        path: PACK_PATH,
        bytes: disk.content.length,
        sha256: disk.sha256,
        content: disk.content
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
      disk.content = body.content
      disk.sha256 = `${body.content.length}`.padEnd(64, 'f')
      return ok({
        path: body.path,
        bytes: disk.content.length,
        sha256: disk.sha256,
        content: disk.content
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
