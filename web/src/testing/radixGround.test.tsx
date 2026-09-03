/**
 * The dependency proof, committed rather than run once in a terminal.
 *
 * One case per Radix primitive the shell uses, each rendering it and asserting
 * the accessible output it produces under this repo's jsdom. It exists for two
 * reasons. A Radix bump that changes a primitive's rendered semantics fails
 * here, beside a name that says which primitive, rather than in whichever shell
 * test happened to depend on it. And the three findings below cost real time to
 * discover; written down as assertions they cost the next reader nothing.
 *
 * **Tabs do not switch on `fireEvent.click`.** Radix activates a tab on
 * `mousedown` (and on focus), and `click` alone leaves the panel `hidden`. A
 * console-channel test written the obvious way therefore asserts on a tab that
 * never switched.
 * **An inactive `Tabs.Content` keeps its element and loses its children.** The
 * panel stays in the DOM carrying `hidden`, and its content is unmounted — so
 * a channel's entries are absent until its tab is actually switched to, and
 * `queryByText` on them returns null rather than hidden text.
 * **`Avatar.Fallback` is absent on first paint** even with `delayMs={0}` and no
 * `Avatar.Image`: it appears a tick later. Every avatar assertion in this repo
 * therefore uses `findBy*`; a `getBy*` would pass or fail on how long an
 * earlier `await` happened to take.
 */
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { Avatar, Collapsible, Dialog, DropdownMenu, Separator, Tabs, Toggle, Tooltip, VisuallyHidden } from 'radix-ui'
import { afterEach, describe, expect, it } from 'vitest'

afterEach(cleanup)

describe('the Radix primitives this shell is built on', () => {
  it('opens a Dialog on a click and exposes its title', async () => {
    render(
      <Dialog.Root>
        <Dialog.Trigger>Open the dialog</Dialog.Trigger>
        <Dialog.Portal>
          <Dialog.Content>
            <Dialog.Title>Create a pack</Dialog.Title>
            <Dialog.Description>bytes come from the runtime</Dialog.Description>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    )
    expect(screen.queryByRole('dialog')).toBeNull()
    fireEvent.click(screen.getByText('Open the dialog'))
    const dialog = await screen.findByRole('dialog')
    expect(dialog.textContent).toContain('Create a pack')
  })

  it('opens a DropdownMenu on Enter and exposes menu items', async () => {
    render(
      <DropdownMenu.Root>
        <DropdownMenu.Trigger>Admin</DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content>
            <DropdownMenu.Item>Organization</DropdownMenu.Item>
            <DropdownMenu.Item>Panes</DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
    )
    fireEvent.keyDown(screen.getByText('Admin'), { key: 'Enter' })
    await screen.findByRole('menu')
    expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual([
      'Organization',
      'Panes'
    ])
  })

  it('renders a Collapsible that is open by default', () => {
    render(
      <Collapsible.Root defaultOpen>
        <Collapsible.Trigger>Packs</Collapsible.Trigger>
        <Collapsible.Content>
          <a href="/packs/intake-triage">intake-triage</a>
        </Collapsible.Content>
      </Collapsible.Root>
    )
    expect(screen.getByRole('link', { name: 'intake-triage' })).toBeTruthy()
  })

  it('switches Tabs on mousedown and not on click, and keeps the inactive panel mounted', () => {
    render(
      <Tabs.Root defaultValue="connection">
        <Tabs.List>
          <Tabs.Trigger value="connection">Connection</Tabs.Trigger>
          <Tabs.Trigger value="files">Files</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="connection">connection entries</Tabs.Content>
        <Tabs.Content value="files">file entries</Tabs.Content>
      </Tabs.Root>
    )
    const files = screen.getByRole('tab', { name: 'Files' })

    // A click alone does nothing: the Files panel stays hidden.
    fireEvent.click(files)
    expect(files.getAttribute('aria-selected')).toBe('false')

    fireEvent.mouseDown(files)
    expect(files.getAttribute('aria-selected')).toBe('true')

    // Files' entries only appear once its tab is the active one.
    expect(screen.getByText('file entries')).toBeTruthy()

    // And the trap: the now-inactive Connection panel keeps its element,
    // carrying `hidden`, but its children are unmounted. Both halves matter —
    // the element is there to be aria-labelled, the text is not there to be
    // asserted on — so a console test must switch the tab, not read through it.
    const panels = screen.getAllByRole('tabpanel', { hidden: true })
    const inactive = panels.find((panel) => panel.hasAttribute('hidden'))!
    expect(inactive.getAttribute('data-state')).toBe('inactive')
    expect(inactive.textContent).toBe('')
    expect(screen.queryByText('connection entries')).toBeNull()
  })

  it('paints an Avatar fallback asynchronously, not on first paint', async () => {
    render(
      <Avatar.Root>
        <Avatar.Fallback delayMs={0}>LU</Avatar.Fallback>
      </Avatar.Root>
    )
    expect(screen.queryByText('LU')).toBeNull()
    const fallback = await screen.findByText('LU')
    expect(fallback.tagName).toBe('SPAN')
  })

  it('gives a Toggle aria-pressed', () => {
    render(<Toggle.Root aria-label="Inspector">I</Toggle.Root>)
    const toggle = screen.getByRole('button', { name: 'Inspector' })
    expect(toggle.getAttribute('aria-pressed')).toBe('false')
    fireEvent.click(toggle)
    expect(toggle.getAttribute('aria-pressed')).toBe('true')
  })

  it('maps a decorative Separator to role="none"', () => {
    const { container } = render(<Separator.Root decorative orientation="vertical" />)
    const rule = container.firstElementChild!
    expect(rule.getAttribute('role')).toBe('none')
    expect(rule.getAttribute('data-orientation')).toBe('vertical')
  })

  it('leaves a Tooltip trigger its own accessible name', () => {
    render(
      <Tooltip.Provider>
        <Tooltip.Root>
          <Tooltip.Trigger aria-label="Graphs">G</Tooltip.Trigger>
          <Tooltip.Portal>
            <Tooltip.Content>Graphs</Tooltip.Content>
          </Tooltip.Portal>
        </Tooltip.Root>
      </Tooltip.Provider>
    )
    expect(screen.getByRole('button', { name: 'Graphs' })).toBeTruthy()
  })

  it('applies VisuallyHidden as inline styles — which is why the skip link is not one', () => {
    render(<VisuallyHidden.Root>Skip to main content</VisuallyHidden.Root>)
    const hidden = screen.getByText('Skip to main content')
    // Inline, so a `.desk-skip:focus` class rule could not beat it without
    // `!important` on every property. The skip link is a plain class instead.
    expect(hidden.style.position).toBe('absolute')
    expect(hidden.style.width).toBe('1px')
    expect(hidden.style.clip).toContain('rect')
  })
})
