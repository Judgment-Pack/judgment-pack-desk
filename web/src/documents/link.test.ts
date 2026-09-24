import { describe, expect, it } from 'vitest'
import { GatewayError } from '../research/gatewayClient'
import { anchorWords, describeLinkFailure, extractLinks, normalizeLink } from './link'
import { validWebURL } from './record'

const INCIDENT = 'https://lab-notes-ai-git-builders-night-demo-treo-gaia.vercel.app/gaia/policy-evidence#brief-human-review'
const FETCHED = 'https://lab-notes-ai-git-builders-night-demo-treo-gaia.vercel.app/gaia/policy-evidence'

describe('normalizeLink', () => {
  it('fetches the link without its fragment and keeps the anchor beside it', () => {
    const link = normalizeLink(INCIDENT)
    expect(link).toEqual({ fetchUrl: FETCHED, displayUrl: INCIDENT, anchor: 'brief-human-review', anchorKind: 'section' })
    // What is fetched is what the record's own rule admits; the link as written is not.
    expect(validWebURL(FETCHED)).toBe(true)
    expect(validWebURL(INCIDENT)).toBe(false)
  })

  it.each([
    ['https://a.example/app#/policy/evidence', 'route'],
    ['https://a.example/app#!evidence', 'route'],
    ['https://a.example/policy', 'none'],
    ['https://a.example/policy#', 'none']
  ])('reads the anchor of %s as %s', (given, kind) => {
    expect(normalizeLink(given)).toMatchObject({ anchorKind: kind })
  })

  it('keeps the query, drops a default port, lowercases the host, trims and encodes', () => {
    expect(normalizeLink('  https://Example.COM:443/Policy?view=1&x=y#top ')).toEqual({
      fetchUrl: 'https://example.com/Policy?view=1&x=y', displayUrl: 'https://Example.COM:443/Policy?view=1&x=y#top', anchor: 'top', anchorKind: 'section'
    })
    expect(normalizeLink('https://example.com/ünïcode?q=ä')).toMatchObject({ fetchUrl: 'https://example.com/%C3%BCn%C3%AFcode?q=%C3%A4' })
    expect(normalizeLink('https://example.com')).toMatchObject({ fetchUrl: 'https://example.com/' })
  })

  it.each([
    'http://example.com/x',
    'https://user:pw@example.com/x',
    'https://@example.com/x',
    'https://example.com:8443/x',
    'ftp://example.com/x',
    'https://',
    'not a link',
    'https://example.com/a\\b',
    'https://example.com/a\nb',
    '',
    `https://example.com/${'a'.repeat(4096)}`
  ])('refuses %s', (given) => {
    expect(normalizeLink(given)).toHaveProperty('refused')
  })

  it('refuses what is not a string', () => {
    expect(normalizeLink(undefined)).toHaveProperty('refused')
    expect(normalizeLink(42)).toHaveProperty('refused')
  })
})

describe('extractLinks', () => {
  it('finds links in prose, once each, without the sentence’s punctuation', () => {
    const text = `He posted (see ${INCIDENT}). Also <https://a.example/b>, "https://c.example/d?q=1", and again ${INCIDENT}!`
    expect(extractLinks(text)).toEqual([INCIDENT, 'https://a.example/b', 'https://c.example/d?q=1'])
  })

  it('reads a Markdown link and keeps balanced parentheses', () => {
    expect(extractLinks('[the page](https://a.example/p) and https://en.wikipedia.org/wiki/Foo_(bar).'))
      .toEqual(['https://a.example/p', 'https://en.wikipedia.org/wiki/Foo_(bar)'])
  })

  it('ignores http:// links and bare scheme', () => {
    expect(extractLinks('http://plain.example/x and https:// and nothing')).toEqual([])
  })
})

describe('anchorWords', () => {
  it('turns a slug into words and decodes percent-encoding', () => {
    expect(anchorWords('brief-human-review')).toBe('brief human review')
    expect(anchorWords('Section_2.1%20Eligibility')).toBe('Section 2 1 Eligibility')
  })

  it('offers nothing for an anchor with no word in it', () => {
    expect(anchorWords('s2')).toBe('')
    expect(anchorWords('1-2')).toBe('')
    expect(anchorWords('%E0%A4%A')).toBe('')
  })
})

describe('describeLinkFailure', () => {
  it.each([
    ['web-invalid-url', 'not a public HTTPS address'],
    ['web-public-only', 'private or local address'],
    ['web-unavailable', 'could not be fetched'],
    ['web-over-limit', '4 MiB'],
    ['web-unsupported-type', 'not HTML, plain text or PDF'],
    ['web-redirect-limit', 'redirected more than five times'],
    ['web-processing-failed', 'could not be processed'],
    ['web-invalid-request', 'malformed']
  ])('names the class for %s', (word, expected) => {
    expect(describeLinkFailure(new GatewayError(400, `source failed: ${word}`))).toContain(expected)
  })

  it('says a gateway that could not be reached, a timeout, and a stop as such', () => {
    expect(describeLinkFailure(new GatewayError(502, 'Bad Gateway'))).toBe('the gateway could not be reached')
    expect(describeLinkFailure(new GatewayError(400, 'source did not finish within its 60-second timeout'))).toContain('timeout')
    expect(describeLinkFailure(Object.assign(new Error('aborted'), { name: 'AbortError' }))).toBe('the read was stopped')
  })

  it('keeps a chassis refusal’s own sentence, bounds a long one, and passes a desk error through', () => {
    expect(describeLinkFailure(new GatewayError(503, 'this desk is already carrying 2 requests to the gateway; nothing was sent', 'research-relay-busy'))).toContain('already carrying')
    expect(describeLinkFailure(new GatewayError(400, 'x'.repeat(400))).length).toBeLessThan(340)
    expect(describeLinkFailure(new Error('Enable document processing first'))).toBe('Enable document processing first')
  })
})
