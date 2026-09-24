import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ResearchConfig } from '../config/deskConfig'
import fixture from '../documents/__fixtures__/web-snapshot.json'
import type { DocumentReference, VerifiedDocument } from '../documents/client'
import type { DocumentRecord } from '../documents/record'
import { GatewayError } from '../research/gatewayClient'
import { Ledger } from '../research/ledger'
import { AuthoringRun, type Turn } from '../research/run'
import type { DraftToolContext } from '../research/useResearchRun'
import { READ_WINDOW, RETRIEVED } from '../research/tools'
import { MAX_LINK_READS_PER_TURN, READ_LINK, givenInChat, linkReading, type LinkReadingDeps } from './linkTools'
import type { ChatAttachment } from './store'

const INCIDENT = 'https://lab-notes-ai-git-builders-night-demo-treo-gaia.vercel.app/gaia/policy-evidence#brief-human-review'
const FETCHED = 'https://lab-notes-ai-git-builders-night-demo-treo-gaia.vercel.app/gaia/policy-evidence'
const PAGE = 'First fact & second. A brief human review follows the evidence.'

const CONFIG: ResearchConfig = {
  gateway: { url: 'http://127.0.0.1:8787', authority: 'gateway:local', signer: { algorithm: 'ed25519', public: 'ab'.repeat(32) } },
  sources: { search: null, read: null },
  limits: { searches: 8, reads: 12, bytes: 8_388_608, seconds: 600 },
  documents: { enabled: true, source: 'documents', maxFileBytes: 16_777_216, maxRequestBytes: 33_554_432, maxResponseBytes: 8_388_608 }
}

let minted = 0
/** A verified web document as `ingestLink` would hand one back, with the given text as its one page. */
function webDocument(text: string, url = FETCHED, pages: string[] = [text]): { reference: DocumentReference; document: VerifiedDocument } {
  const record = structuredClone(fixture) as unknown as DocumentRecord
  const first = record.content.pages[0]!
  record.content.pages = pages.map((body, index) => ({ ...first, number: index + 1, text: body, chars: [...body].length }))
  record.content.pageCount = pages.length
  record.content.chars = record.content.pages.reduce((sum, page) => sum + page.chars, 0)
  record.provenance.source.requestedUrl = url
  record.provenance.source.url = url
  minted += 1
  const digest = `sha256:${minted.toString(16).padStart(64, '0')}`
  const reference = { id: `12345678-1234-1234-1234-${minted.toString().padStart(12, '0')}`, digest, pages: pages.map((_, index) => index + 1), allowPartial: false }
  return { reference, document: { record, digest, object: { version: 1, original: { name: record.document.name, mediaType: 'text/plain', bytes: '', sha256: record.document.id } } } }
}
const user = (text: string): Turn => ({ role: 'user', kind: 'message', text, at: '2026-09-24T00:00:00Z' })
const assistant = (text: string): Turn => ({ role: 'assistant', kind: 'message', text, at: '2026-09-24T00:00:01Z' })
const signal = new AbortController().signal
const said = (result: { content?: { text?: string }[] }) => result.content?.[0]?.text ?? ''

function harness(options: {
  turns?: Turn[]; documents?: ChatAttachment[]; text?: string; available?: boolean; config?: ResearchConfig
  ingest?: LinkReadingDeps['ingest']; load?: LinkReadingDeps['load']
} = {}) {
  const documents: ChatAttachment[] = [...(options.documents ?? [])]
  const turns = options.turns ?? [user(`can you check the link he posted: ${INCIDENT}`)]
  const logged: string[] = []
  const ingested: string[] = []
  const deps: LinkReadingDeps = {
    available: () => options.available ?? true,
    config: () => options.config ?? CONFIG,
    documents: () => documents,
    addDocument: (attachment) => documents.push(attachment),
    log: (line) => logged.push(line),
    ingest: options.ingest ?? (async (selection) => { ingested.push(selection.url); return webDocument(options.text ?? PAGE, selection.url) }),
    ...(options.load ? { load: options.load } : {})
  }
  const factory = linkReading(deps)
  const tools = factory({ turns: () => turns })
  return { tool: tools.find((tool) => tool.name === READ_LINK)!, tools, factory, documents, turns, logged, ingested }
}

afterEach(() => vi.useRealTimers())

describe('read_link', () => {
  it('reads the link the person pasted, without its fragment, and says what became of the anchor', async () => {
    const { tool, ingested, documents, logged } = harness()
    const result = await tool.execute({ url: INCIDENT }, signal)
    expect(result.isError).toBeUndefined()
    expect(ingested).toEqual([FETCHED])
    const answer = said(result)
    expect(answer.startsWith(RETRIEVED)).toBe(true)
    expect(answer).toContain(`link: ${INCIDENT}`)
    expect(answer).toContain(`fetched: ${FETCHED}`)
    expect(answer).toContain('anchor #brief-human-review: not sent to the server, and static text keeps no section ids, so the section could not be located by id')
    expect(answer).toContain('text matches for "brief human review" at characters 23–41 (a text match, not a located section')
    expect(answer).toContain('a static text snapshot of the HTML page')
    expect(answer).toContain(`destination is attachment:${documents[0]!.id}/${documents[0]!.document!.digest}/page/1`)
    expect(answer.endsWith(PAGE)).toBe(true)
    expect(result.structuredContent).toMatchObject({ url: FETCHED, link: INCIDENT, anchor: 'brief-human-review', anchorKind: 'section', chars: PAGE.length, offset: 0, more: false })
    // The chat keeps a reference and the link, never the text.
    expect(documents).toHaveLength(1)
    expect(documents[0]).toMatchObject({ text: '', link: { url: FETCHED, anchor: 'brief-human-review' } })
    expect(documents[0]!.document).toBeDefined()
    expect(logged.some((line) => line.includes('retained as document'))).toBe(true)
  })

  it('refuses a link the person never gave, and asks the gateway for nothing', async () => {
    const { tool, ingested, documents } = harness({ turns: [user('what does the policy say?'), assistant('See https://invented.example/policy for details.')] })
    const result = await tool.execute({ url: 'https://invented.example/policy' }, signal)
    expect(result.isError).toBe(true)
    expect(said(result)).toContain('that link was not given in this chat; ask the person to paste it')
    expect(ingested).toEqual([])
    expect(documents).toHaveLength(0)
  })

  it('refuses a link found only inside a page that was read', async () => {
    const { tool, ingested } = harness({ text: 'Read more at https://other.example/next for the rest.' })
    await tool.execute({ url: INCIDENT }, signal)
    const result = await tool.execute({ url: 'https://other.example/next' }, signal)
    expect(result.isError).toBe(true)
    expect(said(result)).toContain('not given in this chat')
    expect(ingested).toEqual([FETCHED])
  })

  it('hands over a window, never the page, and pages by offset', async () => {
    const text = 'word '.repeat(151_800).trimEnd()
    expect(text.length).toBe(759_000 - 1)
    const { tool, ingested } = harness({ text })
    const first = await tool.execute({ url: INCIDENT }, signal)
    const answer = said(first)
    const shown = answer.slice(answer.lastIndexOf('\n\n') + 2)
    expect(shown.length).toBe(READ_WINDOW)
    expect(answer.length).toBeLessThan(READ_WINDOW + 2_000)
    expect(answer).toContain(`showing characters 0–${READ_WINDOW} of ${text.length} (more: call ${READ_LINK} with the same url and offset ${READ_WINDOW})`)
    expect(first.structuredContent).toMatchObject({ offset: 0, more: true, chars: text.length })
    const next = await tool.execute({ url: INCIDENT, offset: READ_WINDOW }, signal)
    expect(said(next)).toContain(`showing characters ${READ_WINDOW}–${2 * READ_WINDOW} of ${text.length}`)
    const last = await tool.execute({ url: INCIDENT, offset: text.length - 10 }, signal)
    expect(said(last)).toContain(`showing characters ${text.length - 10}–${text.length} of ${text.length}`)
    expect(last.structuredContent).toMatchObject({ more: false })
    expect(ingested).toEqual([FETCHED])
  })

  it('serves a link already in the chat from its document, whatever its fragment', async () => {
    const { tool, ingested, logged, documents } = harness()
    await tool.execute({ url: INCIDENT }, signal)
    const again = await tool.execute({ url: `${FETCHED}#other-section` }, signal)
    expect(ingested).toEqual([FETCHED])
    expect(documents).toHaveLength(1)
    expect(logged.some((line) => line.includes('already in this chat'))).toBe(true)
    expect(said(again)).toContain('anchor #other-section:')
    expect(said(again)).not.toContain('#brief-human-review')
  })

  it('finds a page kept by an earlier session through its reference, verifying it once', async () => {
    const kept = webDocument(PAGE)
    const load = vi.fn(async () => kept.document)
    const attachment: ChatAttachment = { id: kept.reference.id, name: 'policy-evidence.txt', text: '', document: kept.reference, link: { url: FETCHED } }
    const { tool, ingested } = harness({ documents: [attachment], load })
    const first = await tool.execute({ url: INCIDENT }, signal)
    const second = await tool.execute({ url: INCIDENT, offset: 5 }, signal)
    expect(first.isError).toBeUndefined()
    expect(said(second)).toContain('showing characters 5–')
    expect(load).toHaveBeenCalledTimes(1)
    expect(load).toHaveBeenCalledWith(kept.reference, CONFIG.gateway, signal)
    expect(ingested).toEqual([])
  })

  it('reads a link the person attached with Add link without fetching it again', async () => {
    const kept = webDocument(PAGE, 'https://example.com/attached')
    const load = vi.fn(async () => kept.document)
    const attachment: ChatAttachment = { id: kept.reference.id, name: 'attached.txt', text: '', document: kept.reference, link: { url: 'https://example.com/attached' } }
    const { tool, ingested } = harness({ turns: [user('summarise the attached page')], documents: [attachment], load })
    const result = await tool.execute({ url: 'https://example.com/attached' }, signal)
    expect(result.isError).toBeUndefined()
    expect(ingested).toEqual([])
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('spends at most three new links per message, and nothing of the clock', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-24T09:00:00Z'))
    const links = [1, 2, 3, 4].map((n) => `https://example.com/page-${n}`)
    const { tool, factory, ingested, turns } = harness({ turns: [user(`compare ${links.join(' , ')}`)] })
    vi.setSystemTime(new Date('2026-09-24T09:20:00Z'))
    for (const link of links.slice(0, MAX_LINK_READS_PER_TURN)) expect((await tool.execute({ url: link }, signal)).isError).toBeUndefined()
    const fourth = await tool.execute({ url: links[3]! }, signal)
    expect(fourth.isError).toBe(true)
    expect(said(fourth)).toContain(`the budget of ${MAX_LINK_READS_PER_TURN} new links for one message is spent`)
    // A link already read is not a new link.
    expect((await tool.execute({ url: links[0]! }, signal)).isError).toBeUndefined()
    // The next message's tool starts afresh.
    const next = factory({ turns: () => turns }).find((entry) => entry.name === READ_LINK)!
    expect((await next.execute({ url: links[3]! }, signal)).isError).toBeUndefined()
    expect(ingested).toEqual(links)
  })

  it('names a failure by the adapter’s word, and a stopped read as stopped', async () => {
    const refused = harness({ ingest: async () => { throw new GatewayError(400, 'source failed: web-public-only') } })
    const result = await refused.tool.execute({ url: INCIDENT }, signal)
    expect(result.isError).toBe(true)
    expect(said(result)).toBe('read failed: the link resolves to a private or local address, which the gateway does not fetch')
    expect(refused.logged.some((line) => line.includes('failed'))).toBe(true)
    expect(refused.documents).toHaveLength(0)
    const stopped = new AbortController()
    const aborted = harness({ ingest: async () => { stopped.abort(); throw Object.assign(new Error('aborted'), { name: 'AbortError' }) } })
    expect(said(await aborted.tool.execute({ url: INCIDENT }, stopped.signal))).toBe('read failed: the read was stopped')
    const down = harness({ ingest: async () => { throw new GatewayError(502, 'Bad Gateway') } })
    expect(said(await down.tool.execute({ url: INCIDENT }, signal))).toBe('read failed: the gateway could not be reached')
  })

  it('is offered only where the gateway offers the web source, and reads only with document processing', async () => {
    expect(harness({ available: false }).tools).toEqual([])
    const disabled = harness({ config: { ...CONFIG, documents: { ...CONFIG.documents!, enabled: false } } })
    expect(said(await disabled.tool.execute({ url: INCIDENT }, signal))).toContain('link reading is not available')
    const unpinned = harness({ config: { ...CONFIG, gateway: null } })
    expect(said(await unpinned.tool.execute({ url: INCIDENT }, signal))).toContain('link reading is not available')
    expect(disabled.ingested).toEqual([])
  })

  it('refuses a spelling that is not a public https link before looking anything up', async () => {
    const { tool, ingested } = harness({ turns: [user('see http://example.com/x and https://user:pw@example.com/y')] })
    expect(said(await tool.execute({ url: 'http://example.com/x' }, signal))).toContain(`${READ_LINK} cannot read this link`)
    expect(said(await tool.execute({ url: 'https://user:pw@example.com/y' }, signal))).toContain('sign-in')
    expect(said(await tool.execute({}, signal))).toContain('a link is needed')
    expect(ingested).toEqual([])
  })

  it('starts the window just before a find, and reports a route-style anchor as one', async () => {
    const text = `${'filler '.repeat(4_000)}Eligibility rules: an applicant must be resident.${' more'.repeat(3_000)}`
    const { tool } = harness({ turns: [user('read https://app.example/policy#/evidence/brief')], text })
    const result = await tool.execute({ url: 'https://app.example/policy#/evidence/brief', find: 'eligibility RULES' }, signal)
    const answer = said(result)
    const match = text.indexOf('Eligibility rules')
    expect(answer).toContain(`find "eligibility RULES": characters ${match}–${match + 'Eligibility rules'.length} (a text match; the window starts just before the first)`)
    expect(answer).toContain(`showing characters ${match - 200}–`)
    expect(answer).toContain('anchor #/evidence/brief: not sent to the server; it looks like a client-side route')
    expect(result.structuredContent).toMatchObject({ anchorKind: 'route', offset: match - 200 })
    const missing = await tool.execute({ url: 'https://app.example/policy', find: 'nowhere at all' }, signal)
    expect(said(missing)).toContain('find "nowhere at all": no match in the text')
  })

  it('says when the snapshot is too short to be the page', async () => {
    const { tool } = harness({ text: 'Loading…' })
    expect(said(await tool.execute({ url: INCIDENT }, signal))).toContain('the static text is very short; the page may need JavaScript')
  })

  it('cites each page a window spans when the link was a PDF', async () => {
    const pages = ['Page one text about residence.', 'Page two text about income.']
    const { tool, documents } = harness({ ingest: async (selection) => webDocument(pages[0]!, selection.url, pages) })
    const answer = said(await tool.execute({ url: INCIDENT }, signal))
    const reference = documents[0]!.document!
    expect(answer).toContain(`in 2 pages: page 1 at characters 0–${pages[0]!.length}; page 2 at characters ${pages[0]!.length + 2}–`)
    expect(answer).toContain(`attachment:${reference.id}/${reference.digest}/page/1 for page 1, attachment:${reference.id}/${reference.digest}/page/2 for page 2`)
  })

  it('reports a page whose text was not retained instead of an empty window', async () => {
    const { tool } = harness({ ingest: async (selection) => {
      const made = webDocument('', selection.url)
      made.document.record.content.pages[0]!.status = 'no-text'
      made.document.record.processing.status = 'partial'
      made.document.record.processing.errors = [{ code: 'text-over-bound', message: 'past the bound', page: 1 }]
      return made
    } })
    const result = await tool.execute({ url: INCIDENT }, signal)
    expect(result.isError).toBe(true)
    expect(said(result)).toContain('no readable text was retained (processing partial: text-over-bound)')
  })
})

describe('givenInChat', () => {
  it('reads the person’s turns and the chat’s documents, never the assistant’s words', () => {
    const turns = [user(`(see ${INCIDENT}).`), assistant('Also https://assistant.example/made-up')]
    expect(givenInChat(FETCHED, turns, [])).toBe(true)
    expect(givenInChat('https://assistant.example/made-up', turns, [])).toBe(false)
    const kept = webDocument(PAGE, 'https://example.com/attached')
    expect(givenInChat('https://example.com/attached', [], [{ id: kept.reference.id, name: 'a', text: '', document: kept.reference, link: { url: 'https://example.com/attached' } }])).toBe(true)
    expect(givenInChat('https://example.com/attached', [], [{ id: 'x', name: 'a', text: '', link: { url: 'https://example.com/attached' } }])).toBe(false)
  })
})

describe('in a draft run', () => {
  it('is offered to the turn by the controller, with the message just sent among the turns it may read from', async () => {
    const { factory, ingested } = harness({ turns: [] })
    let answered = ''
    let offered: string[] = []
    let built: AuthoringRun | null = null
    // The hook's own wiring: the context reads the run that is about to exist.
    const context: DraftToolContext = { turns: () => built?.getSnapshot().turns ?? [] }
    built = new AuthoringRun({
      mode: 'draft',
      turn: async (request, turnSignal, onEvent) => {
        offered = request.hostTools.map((tool) => tool.name)
        const read = request.hostTools.find((tool) => tool.name === READ_LINK)!
        answered = said(await read.execute({ url: INCIDENT }, turnSignal))
        onEvent({ type: 'message', text: 'The page describes a brief human review.' })
      },
      callTool: async () => { throw new Error('the runtime is not asked in this test') },
      ledger: new Ledger('unset'),
      get researchTools() { return factory(context) },
      seal: async () => {},
      registry: async () => '',
      gateway: null,
      newSession: () => 's1',
      authorPrompt: '',
      maxRevisions: 2,
      seconds: 600,
      log: () => {}
    })
    built.start(`can you check the link he posted: ${INCIDENT}`, [])
    const started = Date.now()
    while (built.getSnapshot().status === 'running' || built.getSnapshot().status === 'idle') {
      if (Date.now() - started > 5000) throw new Error(`still ${built.getSnapshot().status}`)
      await new Promise((resolve) => setTimeout(resolve, 5))
    }
    expect(built.getSnapshot().status).toBe('complete')
    expect(offered).toEqual(['get_authoring_instructions', READ_LINK])
    expect(ingested).toEqual([FETCHED])
    expect(answered).toContain(`link: ${INCIDENT}`)
    expect(built.getSnapshot().turns.map((turn) => turn.role)).toEqual(['user', 'assistant'])
  })
})
