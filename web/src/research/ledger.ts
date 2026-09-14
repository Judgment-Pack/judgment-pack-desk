/**
 * The source ledger of one authoring run: every search and every read the
 * assistant made, each with the receipt the gateway minted for it, the
 * result beside it, what the page read out of that result, the excerpts the
 * assistant cited from it, and the state its receipt verification is in.
 *
 * **Four things are kept apart on purpose.** A search hit's snippet is the
 * provider's index entry, not the page; a read is the reader service's
 * rendering, not the page's bytes; a provider-reported time is not a
 * publication date, and a page's own declared dates are recorded under their
 * own names with nothing filled in for a date nobody stated; and the receipt's
 * `observedAt` is when the gateway's adapter read the answer — the retrieval
 * time — which is a fact about this run, not about the page.
 *
 * The ledger is an external store: React reads it through `useSyncExternalStore`,
 * the tools and the controller write it, and a snapshot is a new array so a
 * subscriber re-renders on a change and only then.
 */
import { memberOf, stringMember, type JsonNode } from './verify/canon'
import type { VerificationState } from './verify/session'
import type { ReadDocument, SearchHit } from './providers'

export interface AcquisitionSummary {
  session: string
  callIndex: number
  signature: string
  resultDigest: string
  /** The adapter's stamp of when the answer had been read: the retrieval time. */
  observedAt: string
  endpoint: string | null
  snapshot: string | null
  peerIdentity: string | null
  adapter: { name: string; version: string; digest: string } | null
}

export interface Excerpt {
  /** `<sourceId>#e<n>`. */
  id: string
  sourceId: string
  /** Character offsets into the source's text, `[start, end)`. */
  start: number
  end: number
  text: string
}

export interface SourceRecord {
  /** `src-<n>`, in the order the run asked. */
  id: string
  kind: 'search' | 'page'
  /**
   * The gateway session this record was opened under, as the desk chose it
   * -- never read off a receipt, which is the thing under verification.
   */
  session: string
  requestedAt: string
  request: { source: string; dialect: string; query?: string; url?: string }
  /** The acquire response, as received, or null where the call failed. */
  response: { text: string; result: JsonNode; receipt: JsonNode; salts: Record<string, string> } | null
  acquisition: AcquisitionSummary | null
  failure: string | null
  hits?: SearchHit[]
  document?: ReadDocument
  verification: VerificationState
  excerpts: Excerpt[]
}

/** The receipt's own members, read for the record. */
export function summarize(receipt: JsonNode): AcquisitionSummary | null {
  const callIndex = memberOf(receipt, 'callIndex')
  const acquisition = memberOf(receipt, 'acquisition')
  const adapter = acquisition === undefined ? undefined : memberOf(acquisition, 'adapter')
  const nullable = (name: string): string | null =>
    acquisition === undefined ? null : (stringMember(acquisition, name) ?? null)
  const session = stringMember(receipt, 'sessionId')
  const signature = stringMember(receipt, 'signature')
  const resultDigest = stringMember(receipt, 'resultDigest')
  if (session === undefined || signature === undefined || resultDigest === undefined || callIndex?.kind !== 'number') {
    return null
  }
  return {
    session,
    callIndex: Number(callIndex.literal),
    signature,
    resultDigest,
    observedAt: acquisition === undefined ? '' : (stringMember(acquisition, 'observedAt') ?? ''),
    endpoint: nullable('endpoint'),
    snapshot: nullable('snapshot'),
    peerIdentity: nullable('peerIdentity'),
    adapter:
      adapter === undefined
        ? null
        : {
            name: stringMember(adapter, 'name') ?? '',
            version: stringMember(adapter, 'version') ?? '',
            digest: stringMember(adapter, 'digest') ?? ''
          }
  }
}

export class Ledger {
  private records: SourceRecord[] = []
  private listeners = new Set<() => void>()
  private snapshot: readonly SourceRecord[] = []

  constructor(private currentSession: string) {}

  /** The gateway session records opened from now on are acquired under. */
  get session(): string {
    return this.currentSession
  }

  openSession(id: string): void {
    this.currentSession = id
  }

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  getSnapshot = (): readonly SourceRecord[] => this.snapshot

  private changed(): void {
    this.snapshot = [...this.records]
    for (const listener of this.listeners) listener()
  }

  get sources(): readonly SourceRecord[] {
    return this.snapshot
  }

  byId(id: string): SourceRecord | undefined {
    return this.records.find((record) => record.id === id)
  }

  /** The page a URL was read into, where one was, so a URL is read once. */
  byUrl(url: string): SourceRecord | undefined {
    return this.records.find((record) => record.kind === 'page' && record.request.url === url && record.failure === null)
  }

  excerpt(id: string): Excerpt | undefined {
    for (const record of this.records) {
      const found = record.excerpts.find((excerpt) => excerpt.id === id)
      if (found) return found
    }
    return undefined
  }

  /** An excerpt whose source's receipt has verified, and no other. */
  verifiedExcerpt(id: string): Excerpt | undefined {
    const excerpt = this.excerpt(id)
    if (!excerpt) return undefined
    const record = this.byId(excerpt.sourceId)
    return record?.verification.state === 'verified' ? excerpt : undefined
  }

  /** The records opened under one session, in the order they were opened. */
  under(session: string): SourceRecord[] {
    return this.records.filter((record) => record.session === session)
  }

  /** Open a record for a call about to be made; filled in by `settle`. */
  open(kind: SourceRecord['kind'], request: SourceRecord['request'], now = new Date()): SourceRecord {
    const record: SourceRecord = {
      id: `src-${this.records.length + 1}`,
      kind,
      session: this.currentSession,
      requestedAt: now.toISOString(),
      request,
      response: null,
      acquisition: null,
      failure: null,
      verification: { state: 'unchecked' },
      excerpts: []
    }
    this.records.push(record)
    this.changed()
    return record
  }

  settle(
    id: string,
    outcome:
      | { response: NonNullable<SourceRecord['response']>; hits?: SearchHit[]; document?: ReadDocument }
      | { failure: string }
  ): SourceRecord {
    const record = this.byId(id)
    if (!record) throw new Error(`no source ${id}`)
    if ('failure' in outcome) {
      record.failure = outcome.failure
    } else {
      record.response = outcome.response
      record.acquisition = summarize(outcome.response.receipt)
      if (outcome.hits) record.hits = outcome.hits
      if (outcome.document) record.document = outcome.document
    }
    this.changed()
    return record
  }

  verified(id: string, verification: VerificationState): void {
    const record = this.byId(id)
    if (!record) return
    record.verification = verification
    this.changed()
  }

  /**
   * Cite a quote from a read source: the quote is located in the source's
   * text exactly as written, or, failing that, with runs of whitespace folded
   * on both sides, and the excerpt records where it was found. A quote that is
   * not in the text is refused, since an excerpt is what makes a citation
   * traceable and a paraphrase would be the assistant's, not the source's.
   */
  cite(sourceId: string, quote: string): Excerpt | { refused: string } {
    const record = this.byId(sourceId)
    if (!record) return { refused: `no source ${sourceId} in this run` }
    if (record.kind !== 'page' || !record.document) {
      return { refused: `${sourceId} is a search, not a page that was read; read the page and cite from its text` }
    }
    const text = record.document.text
    const wanted = quote.trim()
    if (wanted.length < 8) return { refused: 'a quote is at least eight characters' }
    let start = text.indexOf(wanted)
    let end = start + wanted.length
    if (start < 0) {
      // Folded whitespace on both sides, mapped back to the source's offsets.
      const map: number[] = []
      let folded = ''
      let lastSpace = true
      for (let i = 0; i < text.length; i += 1) {
        const ch = text[i]!
        if (/\s/.test(ch)) {
          if (!lastSpace) {
            folded += ' '
            map.push(i)
          }
          lastSpace = true
        } else {
          folded += ch
          map.push(i)
          lastSpace = false
        }
      }
      const foldedQuote = wanted.replace(/\s+/g, ' ')
      const at = folded.indexOf(foldedQuote)
      if (at < 0) return { refused: 'the quote is not in the source text as written; quote it exactly' }
      start = map[at]!
      end = map[at + foldedQuote.length - 1]! + 1
    }
    const existing = record.excerpts.find((excerpt) => excerpt.start === start && excerpt.end === end)
    if (existing) return existing
    const excerpt: Excerpt = {
      id: `${sourceId}#e${record.excerpts.length + 1}`,
      sourceId,
      start,
      end,
      text: text.slice(start, end)
    }
    record.excerpts.push(excerpt)
    this.changed()
    return excerpt
  }
}
