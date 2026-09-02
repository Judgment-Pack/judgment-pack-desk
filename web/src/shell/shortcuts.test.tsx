/**
 * The three chords, and the rule that they stop at the edge of a text field.
 *
 * The suppression is the case that matters. `AuthorView`'s editor is a
 * `<textarea>` holding an unsaved buffer, and a Mod+B that collapsed the rail
 * mid-word would be the shell reaching into a document the user is writing.
 */
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useEffect, useState } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import { SHORTCUTS, installShortcuts, isTypingTarget, shortcutFor } from './shortcuts'

afterEach(cleanup)

/** A page with the listener installed and one counter per action. */
function Harness() {
  const [fired, setFired] = useState<string[]>([])
  useEffect(
    () =>
      installShortcuts({
        toggleRail: () => setFired((was) => [...was, 'rail']),
        toggleInspector: () => setFired((was) => [...was, 'inspector']),
        toggleConsole: () => setFired((was) => [...was, 'console'])
      }),
    []
  )
  return (
    <div>
      <p data-testid="fired">{fired.join(',')}</p>
      <input aria-label="a text field" />
      <textarea aria-label="the editor" />
      <div aria-label="a rich field" contentEditable suppressContentEditableWarning />
      {/* Every shortcut has a visible partner, so a browser that claims a
          chord costs a click rather than a feature. */}
      <button type="button">Collapse navigation</button>
      <button type="button">Inspector</button>
      <button type="button">Console</button>
    </div>
  )
}

function fired(): string[] {
  const text = screen.getByTestId('fired').textContent ?? ''
  return text === '' ? [] : text.split(',')
}

describe('the desk shortcuts', () => {
  it('publishes the list once, for Help & About and the README to quote', () => {
    expect(SHORTCUTS.map((shortcut) => shortcut.keys)).toEqual([
      'Mod+B',
      'Mod+Alt+I',
      'Mod+Alt+J'
    ])
  })

  it('toggles each pane on its own chord, on Ctrl and on Meta alike', () => {
    render(<Harness />)
    fireEvent.keyDown(document, { key: 'b', ctrlKey: true })
    fireEvent.keyDown(document, { key: 'i', metaKey: true, altKey: true })
    fireEvent.keyDown(document, { key: 'j', ctrlKey: true, altKey: true })
    expect(fired()).toEqual(['rail', 'inspector', 'console'])
  })

  it('is ignored while focus is in an input, a textarea, or a contenteditable', () => {
    render(<Harness />)
    for (const label of ['a text field', 'the editor', 'a rich field']) {
      const field = screen.getByLabelText(label)
      field.focus()
      fireEvent.keyDown(field, { key: 'b', ctrlKey: true })
      fireEvent.keyDown(field, { key: 'i', ctrlKey: true, altKey: true })
      fireEvent.keyDown(field, { key: 'j', ctrlKey: true, altKey: true })
    }
    expect(fired()).toEqual([])
  })

  it('renders a visible button for every shortcut it binds', () => {
    render(<Harness />)
    expect(screen.getByRole('button', { name: 'Collapse navigation' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Inspector' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Console' })).toBeTruthy()
  })

  it('binds neither Mod+J nor Alt+digit, which browsers have already claimed', () => {
    render(<Harness />)
    fireEvent.keyDown(document, { key: 'j', ctrlKey: true })
    fireEvent.keyDown(document, { key: '1', altKey: true })
    fireEvent.keyDown(document, { key: 'F6' })
    expect(fired()).toEqual([])
  })

  it('ignores an auto-repeat and an event something else already handled', () => {
    const repeat = new KeyboardEvent('keydown', { key: 'b', ctrlKey: true, repeat: true })
    expect(shortcutFor(repeat)).toBeUndefined()
    const handled = new KeyboardEvent('keydown', { key: 'b', ctrlKey: true, cancelable: true })
    handled.preventDefault()
    expect(shortcutFor(handled)).toBeUndefined()
  })

  it('names the typing rule directly, so it is one rule and not three renderings', () => {
    const input = document.createElement('input')
    const textarea = document.createElement('textarea')
    const plain = document.createElement('div')
    expect(isTypingTarget(input)).toBe(true)
    expect(isTypingTarget(textarea)).toBe(true)
    expect(isTypingTarget(plain)).toBe(false)
    expect(isTypingTarget(null)).toBe(false)
  })
})
