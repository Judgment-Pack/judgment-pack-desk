/**
 * The one card every Admin section renders through.
 *
 * What is asserted here is the shape: the four slots in one order, one status
 * line from a closed set, and a Content disclosure that quotes the file rather
 * than re-serialising it.
 */
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { CardField, SourceCard, type SourceStatus } from './SourceCard'

afterEach(cleanup)

describe('the source card', () => {
  it('prints the title, the location and the status, in that order', () => {
    const { container } = render(
      <SourceCard
        id="a-section"
        title="A section"
        location={<code>/somewhere/desk.json</code>}
        status={{ state: 'read' }}
      />
    )
    expect(screen.getByRole('heading', { level: 2 }).textContent).toBe('A section')
    const keys = Array.from(container.querySelectorAll('dt')).map((each) => each.textContent)
    expect(keys).toEqual(['Location', 'Status'])
    expect(screen.getByText('/somewhere/desk.json')).toBeTruthy()
    expect(screen.getByText('read')).toBeTruthy()
  })

  it('says which of the file states it is in, and never rounds one to another', () => {
    const cases: [SourceStatus, string][] = [
      [{ state: 'read' }, 'read'],
      [{ state: 'absent' }, 'not present — defaults in use'],
      [{ state: 'pending' }, 'not read yet'],
      [{ state: 'said', says: 'connected' }, 'connected']
    ]
    for (const [status, says] of cases) {
      render(<SourceCard id="s" title="S" location="somewhere" status={status} />)
      expect(screen.getByText(says), says).toBeTruthy()
      cleanup()
    }
  })

  it('quotes a refusal in the decoder’s own words, key path and all', () => {
    render(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{
          state: 'refused',
          problems: [{ key: 'panes.left.width', reason: 'must be between 160 and 640' }]
        }}
      />
    )
    expect(screen.getByText('refused:', { exact: false })).toBeTruthy()
    expect(screen.getByText('panes.left.width: must be between 160 and 640')).toBeTruthy()
  })

  it('attributes an unread reason to whoever actually said it', () => {
    // Three provenances, three sentences, carried rather than inferred. A 200
    // whose body this desk cannot use is an answer, and reading provenance off
    // "is there a status?" had the page call it a request with no answer.
    render(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{
          state: 'unread',
          failure: { reason: 'too large', responseReceived: true, status: 413, source: 'chassis' }
        }}
      />
    )
    expect(screen.getByText(/the desk answered 413, and its own reason/)).toBeTruthy()
    expect(screen.getByText('too large')).toBeTruthy()
    cleanup()

    render(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{
          state: 'unread',
          failure: { reason: 'Failed to fetch', responseReceived: false, source: 'browser' }
        }}
      />
    )
    expect(screen.getByText(/the browser’s own reason/)).toBeTruthy()
    expect(screen.queryByText(/the desk answered/)).toBeNull()
    cleanup()

    render(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{
          state: 'unread',
          failure: { reason: 'not JSON', responseReceived: true, status: 200, source: 'desk' }
        }}
      />
    )
    expect(screen.getByText(/this page’s reason/)).toBeTruthy()
  })

  it('shows the member’s own bytes rather than a re-serialisation', () => {
    // `1e2` is not `100` and `9007199254740993` is not what a round trip
    // through a double answers. A disclosure that re-serialised would show the
    // reader a file that is not on disk.
    render(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{ state: 'read' }}
        content={{
          text: '{"panes": {"n": 1e2, "big": 9007199254740993}}',
          member: 'panes',
          value: { n: 100, big: 9007199254740992 }
        }}
      />
    )
    expect(screen.getByText('{"n": 1e2, "big": 9007199254740993}')).toBeTruthy()
    // And it does not claim to be showing a decode.
    expect(screen.queryByText('(decoded)')).toBeNull()
  })

  it('shows the decoded value, said to be decoded, where it has no bytes', () => {
    render(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{ state: 'absent' }}
        content={{ value: { theme: 'system' } }}
      />
    )
    expect(screen.getByText('(decoded)')).toBeTruthy()
    expect(screen.getByText(/"theme": "system"/)).toBeTruthy()
  })

  it('shows the whole file where the card names no member', () => {
    render(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{ state: 'read' }}
        content={{ text: '{"deskConfigVersion": 1}', value: {} }}
      />
    )
    expect(screen.getByText('{"deskConfigVersion": 1}')).toBeTruthy()
  })

  it('renders no bytes of a file the decoder refused', () => {
    // The refusal is about a member; rendering the file anyway puts that
    // member on the page that reported it. The Status line is what a reader
    // needs there, and it is already the refusal.
    render(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{
          state: 'refused',
          problems: [{ key: 'identity.apiKey', reason: 'a key is never stored in configuration' }]
        }}
        content={{
          text: '{"deskConfigVersion":1,"identity":{"apiKey":"sk-live-secret"}}',
          member: 'identity',
          value: { provider: null }
        }}
      />
    )
    expect(document.body.textContent).not.toContain('sk-live-secret')
    // Not the decoded fallback either: the whole disclosure is gone.
    expect(document.querySelector('details')).toBeNull()
    expect(screen.getByText(/identity.apiKey: a key is never stored/)).toBeTruthy()
  })

  it('renders no bytes of a file that could not be read', () => {
    render(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{
          state: 'unread',
          failure: { reason: 'too large', responseReceived: true, status: 413, source: 'chassis' }
        }}
        content={{ text: '{"a":1}', value: { a: 1 } }}
      />
    )
    expect(document.querySelector('details')).toBeNull()
  })

  it('renders no disclosure where there are neither bytes nor a value', () => {
    render(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{ state: 'read' }}
        content={{}}
      />
    )
    expect(document.querySelector('details')).toBeNull()
  })

  it('renders the fields and the save slot only where they are given', () => {
    const { container, rerender } = render(
      <SourceCard id="s" title="S" location="somewhere" status={{ state: 'read' }} />
    )
    expect(container.querySelectorAll('button')).toHaveLength(0)
    rerender(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{ state: 'read' }}
        fields={<CardField label="Theme">system</CardField>}
        save={<button type="button">Save</button>}
      />
    )
    expect(screen.getByText('Theme')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy()
  })

  it('carries a field’s rule under it, and nothing where there is none', () => {
    const { container } = render(
      <SourceCard
        id="s"
        title="S"
        location="somewhere"
        status={{ state: 'read' }}
        fields={
          <>
            <CardField label="Width" rule="160–640px">
              248
            </CardField>
            <CardField label="Mode">expanded</CardField>
          </>
        }
      />
    )
    expect(screen.getByText('160–640px')).toBeTruthy()
    expect(container.querySelectorAll('p')).toHaveLength(1)
  })
})
