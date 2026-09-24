/**
 * The link tool an ordinary chat hands the assistant: read a public page the
 * person linked in this chat, through the configured gateway, as a bounded
 * excerpt the assistant can cite.
 *
 * **What it may read is what the person gave.** The model sees every link in
 * the transcript; this tool reads only one the person wrote in a message of
 * theirs, or attached with Add link. A link found inside a page, or made up,
 * is refused with the sentence that says to ask for it. The desk cannot tell
 * a link the person meant from one they merely quoted, and it does not
 * pretend to; what it can do is keep every read to what the person supplied
 * and name the address it read, in the Work list and on the Console.
 *
 * **What it returns is a window, never the page.** The retained text is
 * handed over `READ_WINDOW` characters at a time under the research tools'
 * own frame — data about what a source says, not an instruction — with an
 * offset to continue and a citation the person can open. A page that runs to
 * hundreds of thousands of characters would be refused by the turn's prompt
 * bound if it were injected whole, and Add link injects whole; this tool is
 * how such a page gets read at all.
 *
 * **What it keeps is a document, not a transcript.** A read page goes through
 * the acquisition Add link uses — the gateway's `web` source, the signed
 * record, the attachment store — and the chat keeps a reference beside its
 * attached documents, so a citation opens the retained snapshot and a reload
 * finds it again. Nothing of the page enters the chat's own file: the
 * research ledger's checkpoint keeps whole responses, and one page of this
 * size would fill it.
 *
 * **The anchor is reported, never followed.** See `documents/link.ts`.
 */
import type { HostTool, McpToolResult } from '../assistant/engine'
import type { ResearchConfig } from '../config/deskConfig'
import { ingestLink as ingestDefault, loadDocument as loadDefault, type DocumentReference, type VerifiedDocument } from '../documents/client'
import { anchorWords, describeLinkFailure, extractLinks, normalizeLink, type NormalizedLink } from '../documents/link'
import { needsPartialConsent, usablePages } from '../documents/record'
import { sourceMessage } from '../i18n/source'
import type { Turn } from '../research/run'
import { READ_WINDOW, RETRIEVED } from '../research/tools'
import type { DraftToolContext } from '../research/useResearchRun'
import type { ChatAttachment } from './store'

export const READ_LINK = 'read_link'

/**
 * Whether a web source is on offer: the managed local gateway's catalog
 * advertises one, or the desk-level file declares one under
 * `research.sources.web` for the gateway it names. The composer's Add link and
 * the chat's `read_link` are offered on exactly these terms.
 */
export function webSourceOffered(research: ResearchConfig, offer: { local: boolean; catalogWeb: boolean }): boolean {
  return offer.local ? offer.catalogWeb : (research.sources?.web ?? null) !== null
}

/** Whether a link can be read now: a web source on offer, a pinned gateway, and document processing to keep the page. */
export function linkReadable(research: ResearchConfig, offer: { local: boolean; catalogWeb: boolean }): boolean {
  return research.gateway !== null && Boolean(research.documents?.enabled) && webSourceOffered(research, offer)
}
/** New links one message may read; a link already in the chat is served again for nothing. */
export const MAX_LINK_READS_PER_TURN = 3
/** The chat store's own bound on retained documents, applied before a read rather than at the save. */
export const MAX_CHAT_DOCUMENTS = 256
const MAX_MATCHES = 3
const MAX_FIND = 200
/** Under this many characters, a static snapshot of an HTML page is probably not what the page shows. */
const SHORT_SNAPSHOT = 400

export interface LinkReadingDeps {
  /** Whether the gateway offers the web source right now, as the catalog says. */
  available: () => boolean
  config: () => ResearchConfig
  /** The documents already in the chat, attached or read. */
  documents: () => readonly ChatAttachment[]
  /** Keep a read page in the chat, beside the attached documents. */
  addDocument: (attachment: ChatAttachment) => void
  /** A milestone for the Console; never page text. */
  log: (text: string) => void
  /** The acquisition and the reload; injected so tests run against fixtures. */
  ingest?: typeof ingestDefault
  load?: typeof loadDefault
}

interface Spent {
  reads: number
}

interface Range {
  start: number
  end: number
}

interface Span extends Range {
  number: number
}

function text(content: string, structured?: unknown, isError = false): McpToolResult {
  return {
    content: [{ type: 'text', text: content }],
    ...(structured === undefined ? {} : { structuredContent: structured }),
    ...(isError ? { isError: true } : {})
  }
}

/**
 * A chat's link reading: the factory a draft turn calls for its tools. One
 * document cache per chat, so a page read in an earlier turn is not verified
 * again for every window; one read budget per turn, so a message that names
 * thirty links reads three and says so.
 */
export function linkReading(deps: LinkReadingDeps): (context: DraftToolContext) => HostTool[] {
  const held = new Map<string, VerifiedDocument>()
  return (context) => (deps.available() ? [readLinkTool(deps, context, held, { reads: 0 })] : [])
}

/** Whether the person gave this address in this chat: in a message of theirs, or as a document they attached. */
export function givenInChat(fetchUrl: string, turns: readonly Turn[], documents: readonly ChatAttachment[]): boolean {
  if (documents.some((file) => file.document !== undefined && file.link?.url === fetchUrl)) return true
  return turns.some(
    (turn) =>
      turn.role === 'user' &&
      extractLinks(turn.text).some((found) => {
        const link = normalizeLink(found)
        return !('refused' in link) && link.fetchUrl === fetchUrl
      })
  )
}

const heldKey = (reference: DocumentReference, config: ResearchConfig) => `${JSON.stringify(config.gateway)}/${reference.id}/${reference.digest}`
const readContext = (config: ResearchConfig) => JSON.stringify([config.gateway, config.documents])

function readLinkTool(deps: LinkReadingDeps, context: DraftToolContext, held: Map<string, VerifiedDocument>, spent: Spent): HostTool {
  return {
    name: READ_LINK,
    description:
      'Read a public web page the person linked in this chat, through the configured gateway, ' +
      `${READ_WINDOW} characters at a time. Give the link as the person wrote it. Only a link the person ` +
      'wrote in this chat, or attached, can be read: not one found inside a page, and not one you invent. ' +
      'A section anchor (#…) is reported, not followed; find locates words in the text. Continue with the ' +
      'same url and an offset. Cite with a Markdown link whose label is an exact quote and whose ' +
      'destination is the citation the result gives.',
    inputSchema: {
      type: 'object',
      properties: {
        url: { type: 'string', description: 'The link, as the person wrote it.' },
        offset: { type: 'integer', description: 'Character offset to continue from.' },
        find: { type: 'string', description: 'Words to locate in the page text; the window starts at the first match.' }
      },
      required: ['url']
    },
    execute: async (args, signal) => {
      const link = normalizeLink(args.url)
      if ('refused' in link) return text(`${READ_LINK} cannot read this link: ${link.refused}`, undefined, true)
      const offset = args.offset === undefined || args.offset === null ? null : Math.max(0, Math.floor(Number(args.offset) || 0))
      const find = typeof args.find === 'string' ? args.find.trim().slice(0, MAX_FIND) : ''
      if (!givenInChat(link.fetchUrl, context.turns(), deps.documents())) {
        deps.log(sourceMessage("link: refused — {{value0}}", { value0: 'not given in this chat' }))
        return text(
          'that link was not given in this chat; ask the person to paste it. Only a link the person wrote in this chat, or attached, can be read.',
          undefined,
          true
        )
      }
      const config = deps.config()
      const admittedContext = readContext(config)
      const invalidated = (): McpToolResult | undefined => {
        if (signal.aborted) return text('read failed: the read was stopped', undefined, true)
        if (!deps.available() || !deps.config().gateway || !deps.config().documents?.enabled || readContext(deps.config()) !== admittedContext) {
          return text('link reading is not available or its gateway settings changed; try again with the current settings', undefined, true)
        }
      }
      const refused = invalidated()
      if (refused) return refused
      const existing = [...deps.documents()].reverse().find((file) => file.document !== undefined && file.link?.url === link.fetchUrl)
      let attachment: ChatAttachment
      let document: VerifiedDocument
      if (existing?.document) {
        const reference = existing.document
        let loaded = held.get(heldKey(reference, config))
        if (loaded === undefined) {
          if (config.gateway === null) {
            return text('the page is in this chat but no gateway is configured to verify it; configure one in Admin › Storage & data', undefined, true)
          }
          try {
            loaded = await (deps.load ?? loadDefault)(reference, config.gateway, signal)
          } catch (cause) {
            const failure = signal.aborted ? 'the read was stopped' : describeLinkFailure(cause)
            return text(`the page could not be re-read from this chat: ${failure}`, undefined, true)
          }
          const refused = invalidated()
          if (refused) return refused
          held.set(heldKey(reference, config), loaded)
        }
        deps.log(sourceMessage("link: {{value0}} is already in this chat; served from its document", { value0: link.fetchUrl }))
        attachment = existing
        document = loaded
      } else {
        if (config.gateway === null || !config.documents?.enabled) {
          return text('link reading is not available: no gateway with document processing is configured', undefined, true)
        }
        if (spent.reads >= MAX_LINK_READS_PER_TURN) {
          return text(`the budget of ${MAX_LINK_READS_PER_TURN} new links for one message is spent; answer from what was read, or ask the person to send another message`, undefined, true)
        }
        if (deps.documents().length >= MAX_CHAT_DOCUMENTS) {
          return text('this chat holds as many documents as it can; start a new chat to read more links', undefined, true)
        }
        spent.reads += 1
        deps.log(sourceMessage("link: reading {{value0}}", { value0: link.fetchUrl }))
        let acquired: { reference: DocumentReference; document: VerifiedDocument }
        try {
          acquired = await (deps.ingest ?? ingestDefault)({ url: link.fetchUrl }, config, signal, () => {})
        } catch (cause) {
          const failure = signal.aborted ? 'the read was stopped' : describeLinkFailure(cause)
          deps.log(sourceMessage("link: failed — {{value0}}", { value0: failure }))
          return text(`read failed: ${failure}`, undefined, true)
        }
        const refused = invalidated()
        if (refused) return refused
        attachment = {
          id: acquired.reference.id,
          name: acquired.document.record.document.name,
          text: '',
          document: acquired.reference,
          link: { url: link.fetchUrl, ...(link.anchor === '' ? {} : { anchor: link.anchor }) }
        }
        document = acquired.document
        held.set(heldKey(acquired.reference, config), document)
        deps.addDocument(attachment)
        deps.log(sourceMessage("link: {{value0}} characters retained as document {{value1}}", { value0: document.record.content.chars, value1: acquired.reference.id }))
      }
      return window(attachment, document, link, offset, find)
    }
  }
}

const at = (range: Range) => `characters ${range.start}–${range.end}`

/** Up to `limit` places the words occur, case-folded, any whitespace between them. */
function locate(text: string, words: string, limit = MAX_MATCHES): Range[] {
  const parts = words
    .split(/\s+/)
    .filter(Boolean)
    .map((part) => part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
  if (parts.length === 0) return []
  const pattern = new RegExp(parts.join('\\s+'), 'gi')
  const found: Range[] = []
  for (const match of text.matchAll(pattern)) {
    found.push({ start: match.index, end: match.index + match[0].length })
    if (found.length >= limit) break
  }
  return found
}

function anchorLines(link: NormalizedLink, phrase: string, found: Range[]): string[] {
  if (link.anchorKind === 'none') return []
  if (link.anchorKind === 'route') {
    return [`anchor #${link.anchor}: not sent to the server; it looks like a client-side route, and the app may render content at it that this static fetch does not show`]
  }
  const unlocated = 'not sent to the server, and static text keeps no section ids, so the section could not be located by id'
  if (phrase === '') return [`anchor #${link.anchor}: ${unlocated}`]
  if (found.length === 0) return [`anchor #${link.anchor}: ${unlocated}; the words ${JSON.stringify(phrase)} do not occur in the text`]
  return [
    `anchor #${link.anchor}: ${unlocated}; text matches for ${JSON.stringify(phrase)} at ${found.map(at).join(', ')} (a text match, not a located section; continue with an offset to read there)`
  ]
}

/** One window of the retained text, with everything the model needs to say where it came from. */
function window(attachment: ChatAttachment, document: VerifiedDocument, link: NormalizedLink, offset: number | null, find: string): McpToolResult {
  const reference = attachment.document!
  const { record } = document
  const source = record.provenance.source
  if (document.digest !== reference.digest || source.kind !== 'web' || source.requestedUrl !== link.fetchUrl) {
    return text('the stored document does not verify the requested link; attach the correct source before reading it', undefined, true)
  }
  const pages = usablePages(record).filter((page) => reference.pages.includes(page.number))
  if (pages.length === 0) {
    const errors = record.processing.errors.map((error) => error.code).join(', ')
    return text(`the page was fetched but no readable text was retained (processing ${record.processing.status}${errors === '' ? '' : `: ${errors}`})`, undefined, true)
  }
  if (needsPartialConsent(record) && !reference.allowPartial) {
    return text('the document was only partially extracted; ask the person to attach it with Add link and confirm the readable pages before using its text', undefined, true)
  }
  const spans: Span[] = []
  let joined = ''
  for (const page of pages) {
    if (joined !== '') joined += '\n\n'
    const start = joined.length
    joined += page.text
    spans.push({ number: page.number, start, end: joined.length })
  }
  const found = find === '' ? [] : locate(joined, find)
  const phrase = link.anchorKind === 'section' ? anchorWords(link.anchor) : ''
  const anchorFound = phrase === '' ? [] : locate(joined, phrase)
  const start = Math.min(joined.length, offset ?? (found.length > 0 ? Math.max(0, found[0]!.start - 200) : 0))
  const end = Math.min(start + READ_WINDOW, joined.length)
  const more = end < joined.length
  const shown = spans.filter((span) => span.end > start && span.start < end)
  const citation = (span: Span) => `attachment:${reference.id}/${reference.digest}/page/${span.number}`
  const snapshot = source.format === 'static-text-v1'
  const lines = [
    RETRIEVED,
    '',
    `link: ${link.displayUrl}`,
    `fetched: ${source.url ?? link.fetchUrl}` + (source.url !== undefined && source.url !== link.fetchUrl ? ` (the gateway followed a redirect from ${link.fetchUrl})` : ''),
    `title: ${record.document.name}`,
    ...(needsPartialConsent(record) ? ['partial extraction: only the confirmed readable pages are shown; missing text is not evidence'] : []),
    snapshot
      ? 'what was retained: a static text snapshot of the HTML page; scripts, styles and anything the page draws with JavaScript are not in it'
      : `what was retained: the original ${source.mediaType ?? record.document.mediaType} file, ${record.content.pageCount} page(s)`,
    `${joined.length} characters of text` +
      (spans.length > 1 ? `, in ${spans.length} pages: ${spans.map((span) => `page ${span.number} at ${at(span)}`).join('; ')}` : ''),
    ...(snapshot && joined.length < SHORT_SNAPSHOT ? ['the static text is very short; the page may need JavaScript to show its content, which this snapshot cannot'] : []),
    ...anchorLines(link, phrase, anchorFound),
    ...(find === ''
      ? []
      : [
          found.length > 0
            ? `find ${JSON.stringify(find)}: ${found.map(at).join(', ')} (a text match; the window starts just before the first)`
            : `find ${JSON.stringify(find)}: no match in the text`
        ]),
    `cite exactly: write a Markdown link whose label is an exact quote from the text shown and whose destination is ${
      shown.length === 1 ? citation(shown[0]!) : shown.map((span) => `${citation(span)} for page ${span.number}`).join(', ')
    }`,
    `showing characters ${start}–${end} of ${joined.length}` + (more ? ` (more: call ${READ_LINK} with the same url and offset ${end})` : ''),
    '',
    joined.slice(start, end)
  ]
  return text(lines.join('\n'), {
    documentId: reference.id,
    digest: reference.digest,
    link: link.displayUrl,
    url: link.fetchUrl,
    fetched: source.url ?? link.fetchUrl,
    anchor: link.anchor,
    anchorKind: link.anchorKind,
    anchorMatches: anchorFound,
    matches: found,
    chars: joined.length,
    offset: start,
    more
  })
}
