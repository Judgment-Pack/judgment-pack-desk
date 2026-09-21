# Public link attachment review

Candidate stacked on the capability catalog. The bundle pins gateway
`741620fa11cda18e935bc3df851433aac0d2649b` (gateway PR #148).

## Observed behavior

An isolated built Desk and managed gateway fetched `https://example.com` through
`adapter-web`, sealed the session, verified the receipt/current pin in the browser,
and attached `Example Domain.txt`. The composer retained its unsent text and
regained focus; the chat store still had zero saved chats. The source preview
showed the original URL, static snapshot label, and Download snapshot action.
No AI provider was called. No personal accounts or project content were used.

The same Add link controller retained its URL across desktop/drawer changes.
All 12 locales were opened at 390 × 720; no document-level horizontal overflow or
browser exceptions occurred. See [results.json](results.json),
[desktop](desktop.png), [Japanese drawer](narrow-ja.png), and
[verified snapshot](snapshot.png).

## Automated checks

- New frontend tests cover explicit attachment without chat creation/send,
  draft preservation, URL validation, failure/retry state, cancellation/unmount/
  capability loss, drawer changes, selected-URL proof and snapshot substitutions,
  incompatible source variants, and actual gateway-produced records.
- Gateway tests cover public/private DNS and redirects, checked numeric dialing,
  TLS host/certificate failures, bounds, charset/compression, HTML omission and
  paragraph handling, plain originals, text/scanned PDFs, and cancellation.
- Desk Go suite, TypeScript/build, locale coverage, bundle guards, existing
  mutation-needle checks, and gateway core/adapter suites were run. The full
  frontend suite initially found only the explicit icon inventory; that list was
  updated and the relevant tests passed. CI reruns the whole suite at the PR head.
- Gateway frozen conformance: 30 canonicalization and 41 store vectors, zero
  disagreements. Receipt format and signing engine are unchanged.

## Limits and remaining work

Public HTTPS only, up to 4 MiB; no sign-in, JavaScript rendering, or web search.
HTML retains static text rather than raw HTML. The adapter reports the raw-response
digest, but reproduction of conversion from that digest alone is not possible.
PDF extraction uses local processing without OCR; partial pages require the usual
review. Gateway may finish after client cancellation, but the result is discarded.

This is a separately reviewable rollout slice. Automatic search and the remaining
provider integrations remain in the connection backlog. Earlier review branches
and the live Desk installation were not changed. Gateway's material-decision
review requirement still applies before merging.
