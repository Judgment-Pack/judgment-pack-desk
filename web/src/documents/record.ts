import { sourceMessage } from '../i18n/source'

export interface DocumentPage {
  number: number; status: 'ok' | 'no-text' | 'needs-ocr' | 'failed'
  extraction: 'text-layer' | 'ocr' | 'verbatim' | 'none'; text: string; chars: number; unmapped: number
}
export interface DocumentRecord {
  attachmentVersion: '1'
  document: { id: string; name: string; mediaType: string; detectedMediaType: string | null; size: number; version: string | null; encryption: { handler: string | null; revision: number | null; opened: boolean } | null }
  original: { retention: 'caller'|'inline'; encoding: 'base64'|null; bytes: string|null }
  content: { kind: 'text'; extraction: DocumentPage['extraction'] | 'mixed'; pageCount: number; truncated: boolean; chars: number; pages: DocumentPage[] }
  processing: { status: 'complete' | 'partial' | 'failed'; errors: { code: string; message: string; page: number | null }[]; bounds: Record<string, number>; durationMs: number }
  provenance: { adapter: { name: string; version: string; digest: string }; source: { kind: 'inline'|'google-drive'; fileId?: string; version?: string; mediaType?: string }; observedAt: string; processor: string | null; ocr: { program: string; digest: string; pages: number[] } | null }
}
const ERROR_CODES = new Set(['media-type-unsupported','media-type-mismatch','document-over-bound','document-empty','pdf-malformed','pdf-encrypted','pdf-unsupported','pdf-page-failed','pdf-pages-over-bound','text-over-bound','stream-over-bound','timeout','ocr-not-run','ocr-failed','ocr-incomplete','ocr-timeout'])
const PAGE_ERRORS = new Set(['pdf-unsupported','pdf-page-failed','stream-over-bound','text-over-bound'])
const enc = new TextEncoder()
const digest = (v: unknown): v is string => typeof v === 'string' && /^sha256:[a-f0-9]{64}$/.test(v)
const int = (v: unknown, min = 0): v is number => typeof v === 'number' && Number.isSafeInteger(v) && v >= min
const str = (v: unknown): v is string => typeof v === 'string'
const nullableString = (v: unknown) => v === null || str(v)
const object = (v: unknown): Record<string, unknown> => {
  if (!v || typeof v !== 'object' || Array.isArray(v)) invalid()
  return v as Record<string, unknown>
}
function invalid(): never { throw new Error(sourceMessage('The document response is unsupported or inconsistent. No extracted text was sent.')) }
function require(condition: unknown): asserts condition { if (!condition) invalid() }

/** A consumer check, deliberately tolerant of additional members at every level.
 * Known values, required members and the relationships that govern page use are
 * checked. Stored text is never re-normalized (normalization is not idempotent). */
export function readDocumentRecord(value: unknown): DocumentRecord {
  const r = object(value), d = object(r.document), o = object(r.original), c = object(r.content), p = object(r.processing), v = object(r.provenance)
  require(r.attachmentVersion === '1' && digest(d.id) && str(d.name) && enc.encode(d.name).length >= 1 && enc.encode(d.name).length <= 255 && !/[\x00-\x1f\x7f]/.test(d.name))
  require(str(d.mediaType) && /^[a-z0-9][a-z0-9!#$&^_.+-]{0,126}\/[a-z0-9][a-z0-9!#$&^_.+-]{0,126}$/.test(d.mediaType))
  require([null, 'application/pdf', 'text/plain'].includes(d.detectedMediaType as string | null) && int(d.size) && nullableString(d.version))
  if (d.encryption !== null) { const e = object(d.encryption); require(nullableString(e.handler) && (e.revision === null || int(e.revision)) && typeof e.opened === 'boolean') }
  const sourceKind = object(v.source).kind
  require(sourceKind === 'google-drive' ? o.retention === 'inline' && o.encoding === 'base64' && str(o.bytes) : o.retention === 'caller' && o.encoding === null && o.bytes === null)
  require(c.kind === 'text' && ['text-layer','ocr','mixed','verbatim','none'].includes(c.extraction as string) && int(c.pageCount) && typeof c.truncated === 'boolean' && int(c.chars) && Array.isArray(c.pages))
  require(['complete','partial','failed'].includes(p.status as string) && Array.isArray(p.errors) && int(p.durationMs))
  const bounds = object(p.bounds)
  for (const key of ['maxBytes','maxPages','maxTextBytes','maxInflateBytes','maxOcrOutputBytes','timeoutMs']) require(int(bounds[key], 1))
  require(c.pages.length <= 500 && c.pages.length <= c.pageCount && c.pageCount <= (bounds.maxPages as number) && (c.pages.length === c.pageCount || c.truncated))
  let chars = 0, bytes = 0
  const methods = new Set<string>()
  for (const [index, item] of c.pages.entries()) {
    const page = object(item)
    require(page.number === index + 1 && ['ok','no-text','needs-ocr','failed'].includes(page.status as string) && ['text-layer','ocr','verbatim','none'].includes(page.extraction as string))
    require(str(page.text) && int(page.chars) && int(page.unmapped) && page.chars === [...page.text].length)
    require(!/[\x00-\x08\x0b-\x1f\x7f]/.test(page.text) && (page.text === '' || (!/^[ \t]*\n/.test(page.text) && !/\n[ \t]*$/.test(page.text) && !/^[ \t]+$/.test(page.text))))
    require(page.status === 'ok' ? page.text !== '' && page.extraction !== 'none' : page.text === '')
    require(page.status === 'no-text' ? page.extraction !== 'none' : !['needs-ocr','failed'].includes(page.status as string) || page.extraction === 'none')
    require(page.extraction === 'text-layer' || page.unmapped === 0)
    require(page.unmapped <= [...page.text].filter(c => c === '\ufffd').length)
    chars += page.chars; bytes += enc.encode(page.text).length
    if (page.extraction !== 'none') methods.add(page.extraction as string)
  }
  require(c.chars === chars && bytes <= (bounds.maxTextBytes as number))
  const method = methods.size === 0 ? 'none' : methods.size === 1 ? [...methods][0] : methods.size === 2 && methods.has('text-layer') && methods.has('ocr') ? 'mixed' : undefined
  require(c.extraction === method)
  for (const item of p.errors) {
    const e = object(item)
    require(str(e.code) && ERROR_CODES.has(e.code) && str(e.message) && enc.encode(e.message).length <= 512 && (e.page === null || int(e.page, 1) && e.page <= c.pageCount))
  }
  for (const item of p.errors) {
    const e = object(item)
    require(PAGE_ERRORS.has(e.code as string) ? e.page !== null : e.page === null)
  }
  for (const item of c.pages) {
    const page = object(item)
    if (page.status === 'failed') require(p.errors.filter(e => object(e).page === page.number && ['pdf-page-failed','pdf-unsupported','stream-over-bound'].includes(object(e).code as string)).length === 1)
  }
  const status = c.pages.length === 0 ? 'failed' : p.errors.length === 0 && !c.truncated ? 'complete' : 'partial'
  require(p.status === status && (p.status !== 'failed' || p.errors.length > 0))
  // A known incomplete page cannot masquerade as complete via an empty errors list.
  if (c.pages.some(item => ['failed','needs-ocr'].includes(object(item).status as string))) require(p.status !== 'complete')
  const adapter = object(v.adapter), source = object(v.source)
  require(str(adapter.name) && adapter.name.length > 0 && str(adapter.version) && digest(adapter.digest) && ['inline', 'google-drive'].includes(source.kind as string))
  if (source.kind === 'google-drive') require(str(source.fileId) && /^[A-Za-z0-9_-]{1,200}$/.test(source.fileId) && str(source.version) && source.version === d.version && str(source.mediaType) && o.retention === 'inline')
  require(str(v.observedAt) && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(v.observedAt) && Number.isFinite(Date.parse(v.observedAt)))
  require(v.processor === null || ['adapter-document/pdf/1','adapter-document/text/1'].includes(v.processor as string))
  if (v.processor === null) require(c.pages.length === 0 && d.detectedMediaType === null)
  if (v.processor === 'adapter-document/pdf/1') require(d.mediaType === 'application/pdf' && d.detectedMediaType === 'application/pdf')
  if (v.processor === 'adapter-document/text/1') require(d.mediaType !== 'application/pdf' && d.detectedMediaType === 'text/plain' && (c.pages.length === 0 || c.extraction === 'verbatim'))
  if (d.encryption !== null && !object(d.encryption).opened) require(c.pages.length === 0)
  if (v.ocr !== null) {
    const ocr = object(v.ocr)
    require(str(ocr.program) && ocr.program.length > 0 && digest(ocr.digest) && Array.isArray(ocr.pages))
    const numbers = ocr.pages as number[], count = c.pageCount as number
    require(c.pages.filter(p => object(p).extraction === 'ocr').map(p => object(p).number).join(',') === numbers.join(','))
    require(numbers.every((n, i) => int(n, 1) && n <= count && (i === 0 || n > numbers[i - 1]!)))
  }
  if (v.ocr === null) require(!methods.has('ocr'))
  return value as DocumentRecord
}
/** Conservatively withhold ANY page with unmapped glyphs, not just a guessed ratio. */
export function usablePages(record: DocumentRecord): DocumentPage[] {
  return record.processing.status === 'failed' ? [] : record.content.pages.filter(p => p.status === 'ok' && p.unmapped === 0)
}
export function needsPartialConsent(record: DocumentRecord): boolean {
  return record.processing.status !== 'complete' || record.content.pages.some(p => p.unmapped > 0)
}
