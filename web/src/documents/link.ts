/**
 * A link the person gave, taken apart into what the gateway fetches and what
 * the chat keeps: the fragment-free address the `web` source is asked for,
 * the spelling the person wrote for display and navigation, and the anchor
 * set beside it with a reading of what kind of thing it is.
 *
 * **The fragment never reaches a server, and nothing here pretends it did.**
 * A fragment is the client's business (RFC 3986 §3.5): the gateway's
 * `attachment.WebURL` and this desk's `validWebURL` refuse one on purpose, and
 * `verifyDocument` binds a document's proof to the exact address that was
 * sent. So the fetch address is the link without its fragment, the proof
 * records that address, and the anchor travels beside the document rather
 * than inside the proof. The retained text is a static snapshot with no
 * element ids in it, so a section anchor is never *located*; what a reader can
 * offer is a text match for the anchor's words, said to be one.
 *
 * **A failure is named by the adapter's own word.** `adapter-web` writes one
 * word on stderr — `web-invalid-url`, `web-public-only`, … — and the gateway
 * carries it back in its refusal (`source failed: web-…`). The sentence for
 * each is written here once, so a chat tool and a pane say the same thing
 * about the same failure, and neither quotes anything from a page.
 */
import { GatewayError } from '../research/gatewayClient'
import { validWebURL } from './record'

export interface NormalizedLink {
  /** The address the gateway is asked for: https, no sign-in, no port, no fragment. */
  fetchUrl: string
  /** The link as the person wrote it, trimmed, for display and navigation. */
  displayUrl: string
  /** The fragment without its `#`, or empty. */
  anchor: string
  /** What the anchor looks like: a section id, a client-side route, or nothing. */
  anchorKind: 'section' | 'route' | 'none'
}

/** The bound the attachment record puts on a source URL, in bytes. */
export const MAX_LINK_BYTES = 4096

const encoder = new TextEncoder()

/**
 * The person's link, as the gateway can be asked for it, or why it cannot.
 *
 * The address is rebuilt from its parsed parts rather than trimmed of its
 * fragment by hand: the URL parser lowercases the host, drops a default port
 * and percent-encodes what needs it, which is the canonical spelling the
 * gateway's `WebURL` insists on (`u.String() == raw`), so a link the person
 * wrote with a capital letter in its host is one the gateway will take.
 */
export function normalizeLink(raw: unknown): NormalizedLink | { refused: string } {
  const given = typeof raw === 'string' ? raw.trim() : ''
  if (given === '') return { refused: 'a link is needed' }
  if (encoder.encode(given).length > MAX_LINK_BYTES) return { refused: `a link is at most ${MAX_LINK_BYTES} bytes` }
  if (/[\\\x00-\x1f\x7f]/.test(given)) return { refused: 'the link contains characters a link cannot' }
  let parsed: URL
  try {
    parsed = new URL(given)
  } catch {
    return { refused: 'the link is not a well-formed address' }
  }
  if (parsed.protocol !== 'https:') return { refused: 'only public https:// links can be read' }
  // The parser drops an empty `@`; the authority as written is what is judged,
  // as `validWebURL` judges it, so a spelling with a sign-in in it is refused whole.
  if (parsed.username !== '' || parsed.password !== '' || given.slice('https://'.length).split(/[/?#]/, 1)[0]!.includes('@')) {
    return { refused: 'a link with a sign-in in it cannot be read' }
  }
  const fetchUrl = `${parsed.origin}${parsed.pathname}${parsed.search}`
  if (!validWebURL(fetchUrl)) return { refused: 'the link is not a public HTTPS address the gateway accepts' }
  const anchor = parsed.hash.slice(1)
  return { fetchUrl, displayUrl: given, anchor, anchorKind: anchor === '' ? 'none' : /^[/!]/.test(anchor) ? 'route' : 'section' }
}

const LINK = /https:\/\/[^\s<>"'`]+/gi

/**
 * The https links written in a text, in order, each once, as written.
 *
 * Trailing punctuation belongs to the sentence, not the link, and a closing
 * bracket is kept only where the link opened one — so `(see https://a/b#c).`
 * yields `https://a/b#c` and a Wikipedia title keeps its parentheses.
 */
export function extractLinks(text: string): string[] {
  const found: string[] = []
  for (const match of text.matchAll(LINK)) {
    let link = match[0]
    for (;;) {
      const last = link.at(-1) ?? ''
      const opener = last === ')' ? '(' : last === ']' ? '[' : last === '}' ? '{' : ''
      if (/[.,;:!?*]/.test(last) || (opener !== '' && !link.includes(opener))) {
        link = link.slice(0, -1)
        continue
      }
      break
    }
    if (link.length > 'https://'.length && !found.includes(link)) found.push(link)
  }
  return found
}

/**
 * The words a section anchor is made of, for a text match:
 * `brief-human-review` → `brief human review`. Empty where the anchor has no
 * word of three letters in it, since a match for `s2` would be noise.
 */
export function anchorWords(anchor: string): string {
  let decoded = anchor
  try {
    decoded = decodeURIComponent(anchor)
  } catch {
    // Not percent-encoded as a whole; the spelling as given is searched.
  }
  const words = decoded.replace(/[-_+.:/]+/g, ' ').replace(/\s+/g, ' ').trim()
  return /\p{L}{3,}/u.test(words) ? words : ''
}

/** One sentence per word `adapter-web` can write; none quotes the page. */
const FAILURES: Readonly<Record<string, string>> = {
  'web-invalid-url': 'the link is not a public HTTPS address the gateway accepts',
  'web-public-only': 'the link resolves to a private or local address, which the gateway does not fetch',
  'web-unavailable': 'the page could not be fetched: DNS, TLS, a timeout, or a status other than 200 (a sign-in redirect answers this way)',
  'web-over-limit': 'the page is larger than the 4 MiB the gateway retains',
  'web-unsupported-type': 'the page is not HTML, plain text or PDF, or it is compressed or not UTF-8',
  'web-redirect-limit': 'the link redirected more than five times',
  'web-processing-failed': 'the page was fetched but its text could not be processed',
  'web-invalid-request': 'the gateway refused the request as malformed'
}

const MAX_FAILURE_TEXT = 300

function bounded(text: string): string {
  return text.length > MAX_FAILURE_TEXT ? `${text.slice(0, MAX_FAILURE_TEXT)}…` : text
}

/**
 * Why a link could not be read, in one sentence that names the class.
 *
 * The adapter's word wins where the message carries one; a chassis-authored
 * refusal (it has a code) is already a sentence and is kept; a gateway that
 * could not be reached, or that stopped the source at its timeout, is said as
 * such; anything else is the message as it came, bounded.
 */
export function describeLinkFailure(cause: unknown): string {
  if ((cause as { name?: unknown } | null)?.name === 'AbortError') return 'the read was stopped'
  const message = cause instanceof Error ? cause.message : String(cause)
  const word = /\bweb-[a-z-]+\b/.exec(message)?.[0]
  if (word !== undefined && Object.hasOwn(FAILURES, word)) return FAILURES[word]!
  if (cause instanceof GatewayError) {
    if (cause.code !== undefined) return bounded(message)
    if (/timeout/i.test(message)) return 'the gateway stopped the fetch at its timeout'
    if (cause.status === 502 || cause.status === 503 || cause.status === 504) return 'the gateway could not be reached'
    return `the gateway refused: ${bounded(message)}`
  }
  return bounded(message)
}
