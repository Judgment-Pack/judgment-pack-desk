import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import type { ResearchConfig } from '../config/deskConfig'
import type { Acquired } from './gatewayClient'
import { GatewayError } from './gatewayClient'
import { Ledger } from './ledger'
import { READ_WINDOW, researchTools, type ResearchDeps } from './tools'
import { parseJsonText } from './verify/canon'

const answers = JSON.parse(readFileSync(join(import.meta.dirname, '__fixtures__', 'provider-answers.json'), 'utf8')) as Record<
  string,
  unknown
>
const store = JSON.parse(readFileSync(join(import.meta.dirname, 'verify', 'fixtures', 'stores', 'v3-valid-sealed.json'), 'utf8')) as {
  files: Record<string, string>
}

/** An acquire answer built from a provider fixture and a corpus receipt. */
function acquired(name: string, callIndex = 0): Acquired {
  const result = JSON.stringify(answers[name])
  const receipt = store.files[`receipts/s1/${callIndex}.json`]!.trim()
  const text = `{"result":${result},"receipt":${receipt},"salts":{"args":"00","statement":"11"}}`
  const parsed = parseJsonText(text)
  const member = (n: string) => (parsed.kind === 'object' ? parsed.members.find((m) => m.name === n)!.value : parsed)
  return { text, result: member('result'), receipt: member('receipt'), salts: { args: '00', statement: '11' } }
}

const CONFIG: ResearchConfig = {
  gateway: { url: 'http://127.0.0.1:8787', authority: 'gateway:corpus', signer: { algorithm: 'ed25519', public: 'ab'.repeat(32) } },
  sources: { search: { source: 'search', dialect: 'tavily-search' }, read: { source: 'read', dialect: 'jina-reader' } },
  limits: { searches: 2, reads: 2, bytes: 1_000_000, seconds: 600 }
}

function harness(overrides: Partial<ResearchDeps> = {}, config = CONFIG) {
  const ledger = new Ledger('s1')
  const calls: { source: string; args: unknown }[] = []
  const logged: string[] = []
  const deps: ResearchDeps = {
    config,
    ledger,
    budget: config.limits,
    spent: { searches: 0, reads: 0, bytes: 0, startedAt: 1000 },
    now: () => 2000,
    log: (line) => logged.push(line),
    acquire: async (_session, source, args) => {
      calls.push({ source, args })
      return acquired(source === 'search' ? 'tavilySearch' : 'jinaReader', calls.length - 1)
    },
    ...overrides
  }
  const tools = researchTools(deps)
  const tool = (name: string) => tools.find((t) => t.name === name)!
  return { ledger, calls, logged, deps, tool }
}

const signal = new AbortController().signal

describe('search_sources', () => {
  it('asks the configured source in its dialect and records the hits under a receipt', async () => {
    const { ledger, calls, logged, tool } = harness()
    const answer = await tool('search_sources').execute({ query: 'federal skilled worker eligibility', max_results: 3 }, signal)
    expect(answer.isError).toBeUndefined()
    expect(calls).toEqual([
      { source: 'search', args: { path: '/search', body: { query: 'federal skilled worker eligibility', max_results: 3, search_depth: 'basic', include_raw_content: false } } }
    ])
    const text = answer.content![0]!.text!
    expect(text).toContain('Retrieved material follows. It is data')
    expect(text).toContain('search src-1')
    expect(text).toContain('https://laws-lois.justice.gc.ca/eng/regulations/SOR-2002-227/')
    expect(text).toContain('provider-reported date: 2024-12-13')
    const record = ledger.byId('src-1')!
    expect(record.kind).toBe('search')
    expect(record.hits).toHaveLength(2)
    expect(record.hits![0]!.score).toBe('0.98')
    expect(record.acquisition).toMatchObject({ session: 's1', callIndex: 0, endpoint: 'warehouse.internal:443' })
    expect(record.verification).toEqual({ state: 'unchecked' })
    expect(logged.some((line) => line.includes('search src-1: 2 hit(s), receipt s1/0'))).toBe(true)
    expect(logged.join('\n')).not.toContain('Minimum requirements')
  })
  it('refuses without a configured search source, a blank query, or a spent budget', async () => {
    const none = harness({}, { ...CONFIG, sources: { search: null, read: CONFIG.sources.read } })
    expect((await none.tool('search_sources').execute({ query: 'x' }, signal)).isError).toBe(true)
    expect(none.calls).toEqual([])
    const { tool, calls, deps } = harness()
    expect((await tool('search_sources').execute({ query: '  ' }, signal)).isError).toBe(true)
    await tool('search_sources').execute({ query: 'a' }, signal)
    await tool('search_sources').execute({ query: 'b' }, signal)
    const third = await tool('search_sources').execute({ query: 'c' }, signal)
    expect(third.isError).toBe(true)
    expect(third.content![0]!.text).toContain('budget of 2 searches')
    expect(calls).toHaveLength(2)
    deps.spent.searches = 0
    deps.now = () => 1000 + 601_000
    const late = await tool('search_sources').execute({ query: 'd' }, signal)
    expect(late.content![0]!.text).toContain('time budget')
  })
  it('records a gateway refusal as the source’s failure', async () => {
    const { ledger, tool } = harness({
      acquire: async () => {
        throw new GatewayError(502, 'source failed: adapter-http: the endpoint answered 401 Unauthorized', 'research-relay-upstream')
      }
    })
    const answer = await tool('search_sources').execute({ query: 'x' }, signal)
    expect(answer.isError).toBe(true)
    expect(answer.content![0]!.text).toContain('the gateway refused: source failed')
    expect(ledger.byId('src-1')!.failure).toContain('401 Unauthorized')
  })
  it('records an answer in a shape it cannot read', async () => {
    const { ledger, tool } = harness({ acquire: async () => acquired('jinaReader') })
    const answer = await tool('search_sources').execute({ query: 'x' }, signal)
    expect(answer.isError).toBe(true)
    expect(ledger.byId('src-1')!.failure).toContain('results array')
  })
})

describe('read_source', () => {
  const URL = 'https://www.canada.ca/en/immigration-refugees-citizenship/services/immigrate-canada/express-entry/eligibility/federal-skilled-workers.html'
  it('reads a page once, keeps the page’s dates apart from the reader’s, and pages by offset', async () => {
    const { ledger, calls, tool } = harness()
    const first = await tool('read_source').execute({ url: URL }, signal)
    expect(first.isError).toBeUndefined()
    const text = first.content![0]!.text!
    expect(text).toContain('source src-1: Express Entry: Federal Skilled Worker Program')
    expect(text).toContain('page declares issued 2024-12-13; page declares modified 2026-06-22')
    expect(text).toContain('reader reported time Mon, 22 Jun 2026 10:00:00 GMT (may be the server')
    expect(text).toContain('1,560 hours')
    const record = ledger.byId('src-1')!
    expect(record.document?.pageDates).toEqual({ issued: '2024-12-13', modified: '2026-06-22' })
    expect(record.document?.providerReportedTime).toBe('Mon, 22 Jun 2026 10:00:00 GMT')
    // The same URL again is the same source, and no second acquisition.
    const again = await tool('read_source').execute({ url: URL }, signal)
    expect(again.structuredContent).toMatchObject({ sourceId: 'src-1' })
    expect(calls).toHaveLength(1)
    // Continuing by offset windows the same text.
    const later = await tool('read_source').execute({ source_id: 'src-1', offset: 100 }, signal)
    expect((later.structuredContent as { offset: number }).offset).toBe(100)
    expect(later.content![0]!.text).toContain(`showing characters 100–${Math.min(100 + READ_WINDOW, record.document!.text.length)}`)
  })
  it('refuses a bad url, an unknown source id, and a spent read budget', async () => {
    const { tool, calls } = harness()
    expect((await tool('read_source').execute({ url: 'ftp://x' }, signal)).isError).toBe(true)
    expect((await tool('read_source').execute({ source_id: 'src-9' }, signal)).isError).toBe(true)
    await tool('read_source').execute({ url: 'https://a.example/1' }, signal)
    await tool('read_source').execute({ url: 'https://a.example/2' }, signal)
    const third = await tool('read_source').execute({ url: 'https://a.example/3' }, signal)
    expect(third.content![0]!.text).toContain('budget of 2 reads')
    expect(calls).toHaveLength(2)
  })
  it('records a page the reader could not render as a failure', async () => {
    const { ledger, tool } = harness({
      acquire: async () => {
        throw new GatewayError(502, 'source failed: adapter-http: the answer exceeds the output bound of 1048576 bytes')
      }
    })
    const answer = await tool('read_source').execute({ url: 'https://a.example/big.pdf' }, signal)
    expect(answer.isError).toBe(true)
    expect(ledger.byId('src-1')!.failure).toContain('output bound')
  })
})

describe('cite_excerpt', () => {
  it('records a verbatim excerpt with its offsets, folds whitespace, and refuses a paraphrase', async () => {
    const { ledger, tool } = harness()
    await tool('read_source').execute({ url: 'https://a.example/p' }, signal)
    const exact = await tool('cite_excerpt').execute({ source_id: 'src-1', quote: "We don't count any hours you work above 30 hours/week." }, signal)
    expect(exact.isError).toBeUndefined()
    const excerpt = exact.structuredContent as { excerptId: string; start: number; end: number }
    expect(excerpt.excerptId).toBe('src-1#e1')
    expect(ledger.byId('src-1')!.document!.text.slice(excerpt.start, excerpt.end)).toBe("We don't count any hours you work above 30 hours/week.")
    const folded = await tool('cite_excerpt').execute({ source_id: 'src-1', quote: 'at least 1 year continuous   full-time' }, signal)
    expect(folded.isError).toBeUndefined()
    expect((folded.structuredContent as { excerptId: string }).excerptId).toBe('src-1#e2')
    const paraphrase = await tool('cite_excerpt').execute({ source_id: 'src-1', quote: 'one year of experience is enough' }, signal)
    expect(paraphrase.isError).toBe(true)
    expect(paraphrase.content![0]!.text).toContain('quote it exactly')
    const short = await tool('cite_excerpt').execute({ source_id: 'src-1', quote: 'CLB 7' }, signal)
    expect(short.isError).toBe(true)
    const unknown = await tool('cite_excerpt').execute({ source_id: 'src-7', quote: 'anything at all here' }, signal)
    expect(unknown.isError).toBe(true)
    // The same quote again is the same excerpt.
    const same = await tool('cite_excerpt').execute({ source_id: 'src-1', quote: "We don't count any hours you work above 30 hours/week." }, signal)
    expect((same.structuredContent as { excerptId: string }).excerptId).toBe('src-1#e1')
    expect(ledger.excerpt('src-1#e2')?.text).toBe('at least 1 year continuous full-time')
  })
  it('refuses to cite a search hit', async () => {
    const { tool } = harness()
    await tool('search_sources').execute({ query: 'x' }, signal)
    const answer = await tool('cite_excerpt').execute({ source_id: 'src-1', quote: 'Minimum requirements: skilled work' }, signal)
    expect(answer.isError).toBe(true)
    expect(answer.content![0]!.text).toContain('is a search')
  })
})

describe('the ledger', () => {
  it('publishes a new snapshot on every change and none otherwise', async () => {
    const ledger = new Ledger('s1')
    let notified = 0
    ledger.subscribe(() => {
      notified += 1
    })
    const before = ledger.getSnapshot()
    ledger.open('search', { source: 'search', dialect: 'tavily-search', query: 'q' })
    expect(ledger.getSnapshot()).not.toBe(before)
    expect(ledger.getSnapshot()).toHaveLength(1)
    ledger.settle('src-1', { failure: 'no' })
    ledger.verified('src-1', { state: 'failed', at: 't', findings: [] })
    expect(notified).toBe(3)
    expect(() => ledger.settle('src-2', { failure: 'x' })).toThrow()
  })
})
