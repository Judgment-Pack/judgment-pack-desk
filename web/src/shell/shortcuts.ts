/**
 * The desk's three keyboard shortcuts, and the one rule that matters more than
 * the three of them.
 *
 * **Every shortcut is suppressed while focus is in a text field.** That is
 * exactly AuthorView's `<textarea className="code-editor">`, where a Mod+B
 * that collapsed the rail mid-sentence would be a shell reaching into an
 * editor. The suppression is checked first, before anything is read about
 * which chord this is.
 *
 * What is deliberately *not* bound: `Mod+J` (Chrome and Firefox Downloads),
 * `Alt+<digit>` (Firefox tab switching), and `F6` (the browser's own pane
 * cycle, which is a better answer than anything this could install).
 *
 * On macOS, Cmd+Alt+I and Cmd+Alt+J are DevTools chords the browser claims
 * before the page ever sees them, and Cmd+B is Firefox's bookmarks sidebar.
 * `mod` accepts Ctrl *or* Meta, so the Ctrl spelling works everywhere
 * including macOS — and every shortcut has a visible button, so a chord the
 * browser eats costs a click rather than a feature.
 */

export interface Shortcut {
  keys: string
  label: string
}

/**
 * The list, typed and exported once: Help & About renders it and the README
 * quotes it, so there is one place a chord is written down.
 */
export const SHORTCUTS: readonly Shortcut[] = [
  { keys: 'Mod+B', label: 'Collapse or expand the navigation rail' },
  { keys: 'Mod+Alt+I', label: 'Open or close the Inspector' },
  { keys: 'Mod+Alt+J', label: 'Open or close the Console' }
]

export interface ShortcutActions {
  toggleRail: () => void
  toggleInspector: () => void
  toggleConsole: () => void
}

/**
 * True where a key event belongs to whatever the user is typing into.
 *
 * Editability is asked two ways because the two answer different questions and
 * neither covers the other. `isContentEditable` is the browser's *computed*
 * answer and is the one that catches an element nested inside an editable
 * region — but jsdom does not implement it, so a test could not tell a working
 * suppression from a broken one on that alone. The nearest declaring ancestor
 * is the *declared* answer, and it holds in both.
 *
 * Exported so the test names the rule rather than a rendering of it.
 */
export function isTypingTarget(target: EventTarget | null): boolean {
  const element = target as HTMLElement | null
  if (!element || typeof element.tagName !== 'string') return false
  if (element.tagName === 'INPUT' || element.tagName === 'TEXTAREA') return true
  if (element.isContentEditable === true) return true
  return Boolean(element.closest?.('[contenteditable]:not([contenteditable="false"])'))
}

/**
 * Which action a keydown asks for, or undefined.
 *
 * **Every modifier a chord does not declare is rejected**, not ignored. Read
 * loosely, `Mod+B` also claimed Ctrl+Shift+B — a chord this desk never
 * declared, which the browser and the operating system are entitled to own —
 * and worse, claiming it called `preventDefault` on it, so the shifted spelling
 * of a shortcut fired the action and took the key away from whatever else
 * wanted it. Ctrl and Meta together are rejected on the same ground: `mod` is
 * "Ctrl **or** Cmd", and Ctrl+Cmd+B is a third chord, not either of them.
 */
export function shortcutFor(event: KeyboardEvent): keyof ShortcutActions | undefined {
  if (event.repeat || event.defaultPrevented) return undefined
  if (isTypingTarget(event.target)) return undefined
  const mod = event.ctrlKey !== event.metaKey
  if (!mod) return undefined
  if (event.shiftKey) return undefined
  const key = event.key.toLowerCase()
  if (!event.altKey && key === 'b') return 'toggleRail'
  if (event.altKey && key === 'i') return 'toggleInspector'
  if (event.altKey && key === 'j') return 'toggleConsole'
  return undefined
}

/**
 * Install the listener. Returns the remover, so the caller's effect cleanup is
 * the whole lifecycle.
 */
export function installShortcuts(actions: ShortcutActions): () => void {
  const onKeyDown = (event: KeyboardEvent) => {
    const action = shortcutFor(event)
    if (!action) return
    // Prevented only after it fires, so a chord this does not handle is left
    // entirely to the browser.
    actions[action]()
    event.preventDefault()
  }
  document.addEventListener('keydown', onKeyDown)
  return () => document.removeEventListener('keydown', onKeyDown)
}
