/**
 * The wire dialects a research source speaks: what the page writes on an
 * `/acquire` for a search or a read, and how it reads the answer the
 * `adapter-http` envelope carries back — `{status, headers, bodyEncoding,
 * body}` with the provider's JSON as `body`.
 *
 * **What a dialect reads is the provider's testimony.** A search hit's title,
 * URL and snippet are what the search provider said about a page it indexed;
 * a read's text and title are what the reader service rendered when it
 * fetched the URL. Neither is the page's own bytes, and the record says so:
 * a provider-reported time is kept apart from a page's own declared dates,
 * and a date nobody stated stays unknown.
 */
import type { ResearchDialect } from '../config/deskConfig'
import { toPlain, type JsonNode } from './verify/canon'

export interface SearchHit {
  rank: number
  title: string
  url: string
  /** The provider's snippet: a search result, never the page. */
  snippet: string
  /** A date the provider reported for the hit, as it reported it, or undefined. */
  providerDate?: string
  score?: string
}

export interface ReadDocument {
  url: string
  title: string
  /** The rendered text, as the reader service returned it. */
  text: string
  /** The HTTP status the reader saw at the page's own URL, where it said. */
  httpStatus?: number
  /**
   * A time the reader reported for the page. Jina labels this `publishedTime`
   * and fills it from `Last-Modified` where the page declares nothing, so it
   * is kept under this name and never promoted to a publication date.
   */
  providerReportedTime?: string
  /** The page's own declared dates, from its metadata, where it declared any. */
  pageDates: { issued?: string; modified?: string }
  language?: string
  pages?: number
}

/** The provider's body out of the adapter's result envelope, as plain data. */
export function providerBody(result: JsonNode): { status: number; body: unknown } {
  const plain = toPlain(result) as { status?: unknown; bodyEncoding?: unknown; body?: unknown }
  if (typeof plain.status !== 'number' || plain.bodyEncoding !== 'json') {
    throw new Error('the source answered with something other than a JSON body')
  }
  return { status: plain.status, body: plain.body }
}

function str(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined
}

export interface SearchDialect {
  request(query: string, options: { maxResults: number; domains?: string[] }): unknown
  hits(result: JsonNode): SearchHit[]
}

export interface ReadDialect {
  request(url: string): unknown
  document(result: JsonNode): ReadDocument
}

const tavilySearch: SearchDialect = {
  request: (query, { maxResults, domains }) => ({
    path: '/search',
    body: {
      query,
      max_results: maxResults,
      search_depth: 'basic',
      include_raw_content: false,
      ...(domains && domains.length > 0 ? { include_domains: domains } : {})
    }
  }),
  hits: (result) => {
    const { body } = providerBody(result)
    const results = (body as { results?: unknown })?.results
    if (!Array.isArray(results)) throw new Error('the search answered without a results array')
    return results.map((entry, index) => {
      const hit = entry as Record<string, unknown>
      return {
        rank: index + 1,
        title: str(hit.title) ?? '',
        url: str(hit.url) ?? '',
        snippet: str(hit.content) ?? '',
        ...(str(hit.published_date) !== undefined ? { providerDate: str(hit.published_date) } : {}),
        ...(hit.score !== undefined ? { score: String(hit.score) } : {})
      }
    })
  }
}

const jinaReader: ReadDialect = {
  request: (url) => ({ path: '/', body: { url } }),
  document: (result) => {
    const { body } = providerBody(result)
    const data = (body as { data?: unknown })?.data as Record<string, unknown> | undefined
    if (data === undefined || typeof data !== 'object') throw new Error('the reader answered without a data member')
    const metadata = (data.metadata ?? {}) as Record<string, unknown>
    const issued = str(metadata['dcterms.issued']) ?? str(metadata['article:published_time'])
    const modified = str(metadata['dcterms.modified']) ?? str(metadata['article:modified_time'])
    return {
      url: str(data.url) ?? '',
      title: str(data.title) ?? '',
      text: str(data.content) ?? '',
      ...(typeof data.httpStatus === 'number' ? { httpStatus: data.httpStatus } : {}),
      ...(str(data.publishedTime) !== undefined ? { providerReportedTime: str(data.publishedTime) } : {}),
      pageDates: { ...(issued ? { issued } : {}), ...(modified ? { modified } : {}) },
      ...(str(metadata.lang) !== undefined ? { language: str(metadata.lang) } : {}),
      ...(typeof data.numPages === 'number' ? { pages: data.numPages } : {})
    }
  }
}

export const SEARCH_DIALECTS: Partial<Record<ResearchDialect, SearchDialect>> = {
  'tavily-search': tavilySearch
}

export const READ_DIALECTS: Partial<Record<ResearchDialect, ReadDialect>> = {
  'jina-reader': jinaReader
}
