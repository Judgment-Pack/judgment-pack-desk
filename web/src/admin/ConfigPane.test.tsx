/**
 * The file in the right pane, and the two rules that came with it.
 *
 * These cases were `SourceCard.test.tsx`'s until the bytes left the main
 * column. They are here rather than there because they are about the component
 * that renders the bytes, and the one that no longer does should not be the
 * one asserting what may be shown.
 *
 * Two of them are safety rules and not tidiness: **a refused file's bytes are
 * never rendered**, because the refusal is about a member and rendering the
 * file anyway puts that member into the DOM of the surface reporting it; and
 * **what is shown is the file's own bytes**, because `1e2` is not `100` and a
 * pane that stringified the decode would show a reader a file that is not on
 * disk beside a Location row saying where that file is.
 */
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { ConfigPane, NO_BYTES_SAYS, digestSays } from './ConfigPane'

afterEach(cleanup)

describe('the configuration pane', () => {
  it('names the file and the member, and states the location it was given', () => {
    render(
      <ConfigPane
        title="jpack-desk.json › storage"
        location={<code>/a/project/jpack-desk.json</code>}
        status={{ state: 'read' }}
        digest={'a'.repeat(64)}
        text='{"deskConfigVersion": 1, "storage": {"packs": {"dir": "packs"}}}'
        member="storage"
      />
    )
    expect(screen.getByRole('heading', { level: 2 }).textContent).toBe(
      'jpack-desk.json › storage'
    )
    expect(screen.getByText('/a/project/jpack-desk.json')).toBeTruthy()
    expect(screen.getByText('read')).toBeTruthy()
    expect(screen.getByText('{"packs": {"dir": "packs"}}')).toBeTruthy()
  })

  it('shows the member’s own bytes rather than a re-serialisation', () => {
    // `1e2` is not `100` and `9007199254740993` is not what a round trip
    // through a double answers, so a pane that re-serialised would show the
    // reader a file that is not on disk.
    render(
      <ConfigPane
        title="jpack-desk.json › panes"
        location="somewhere"
        status={{ state: 'read' }}
        text='{"panes": {"n": 1e2, "big": 9007199254740993}}'
        member="panes"
      />
    )
    expect(screen.getByText('{"n": 1e2, "big": 9007199254740993}')).toBeTruthy()
    expect(document.body.textContent).not.toContain('9007199254740992')
  })

  it('shows the whole file where it names no member', () => {
    render(
      <ConfigPane
        title="jpack-desk.json"
        location="somewhere"
        status={{ state: 'read' }}
        text='{"deskConfigVersion": 1}'
      />
    )
    expect(screen.getByText('{"deskConfigVersion": 1}')).toBeTruthy()
  })

  it('renders no bytes of a file the decoder refused, and says the refusal', () => {
    render(
      <ConfigPane
        title="desk.json › identity"
        location="somewhere"
        status={{
          state: 'refused',
          problems: [{ key: 'identity.apiKey', reason: 'a key is never stored in configuration' }]
        }}
        text='{"deskConfigVersion":1,"identity":{"apiKey":"sk-live-secret"}}'
        member="identity"
      />
    )
    expect(document.body.textContent).not.toContain('sk-live-secret')
    expect(document.querySelector('pre')).toBeNull()
    // And the refusal itself is said, in the decoder's own words.
    expect(screen.getByText(/identity.apiKey: a key is never stored/)).toBeTruthy()
    // Not the "could not be established" line either: that is about a member
    // this page may quote and could not find, which is a different fact.
    expect(screen.queryByText(NO_BYTES_SAYS)).toBeNull()
  })

  it('renders no bytes of a file that could not be read at all', () => {
    render(
      <ConfigPane
        title="jpack-desk.json › storage"
        location="somewhere"
        status={{
          state: 'unread',
          failure: { reason: 'too large', responseReceived: true, status: 413, source: 'chassis' }
        }}
        text='{"storage":{"packs":{"dir":"packs"}}}'
        member="storage"
      />
    )
    expect(document.querySelector('pre')).toBeNull()
    expect(screen.getByText(/the desk answered 413, and its own reason/)).toBeTruthy()
  })

  it('says the bytes could not be established, rather than offering a decode', () => {
    // `memberBytes` answers nothing for four different states — not one JSON
    // object, malformed, the member absent, the member written twice — and the
    // page does the same thing in all four, because it can honestly say only
    // that it has no bytes to quote.
    render(
      <ConfigPane
        title="jpack-desk.json › storage"
        location="somewhere"
        status={{ state: 'read' }}
        text='{"deskConfigVersion": 1}'
        member="storage"
      />
    )
    expect(screen.getByText(NO_BYTES_SAYS)).toBeTruthy()
    expect(document.querySelector('pre')).toBeNull()
  })

  it('states the digest as a prefix, and states none where there is none', () => {
    // The digest is what a write says it replaces; a reader needs enough of it
    // to tell one revision from another on sight, and no more.
    const { rerender } = render(
      <ConfigPane
        title="jpack-desk.json"
        location="somewhere"
        status={{ state: 'read' }}
        digest="0123456789abcdef0123"
        text="{}"
      />
    )
    expect(screen.getByText(digestSays('0123456789abcdef0123'))).toBeTruthy()
    expect(screen.getByText('sha256 0123456789ab…')).toBeTruthy()
    // The empty string is the chassis saying "there is no file" — a digest a
    // write may state and not one a reader is shown. The Status row says which.
    rerender(
      <ConfigPane
        title="jpack-desk.json"
        location="somewhere"
        status={{ state: 'absent' }}
        digest=""
      />
    )
    expect(screen.queryByText(/^sha256 /)).toBeNull()
    expect(screen.getByText('not present — defaults in use')).toBeTruthy()
  })
})
