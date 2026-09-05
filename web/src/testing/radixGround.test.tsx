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
 * **A `Select`'s options do not exist until its trigger is opened**, and the
 * trigger reports the current choice as its own text. So "what does this
 * select offer" cannot be asked of a closed one — which is why the Create-pack
 * dialog's template cases open it first, and why the eleven cases written
 * against a native `<select>` could not be adjusted to it.
 * **An arrow key moves a `Select`'s focus inside a `setTimeout`.** The handler
 * on the content computes the candidates and then defers `focusFirst`, so an
 * assertion on the next line reads the focus the key was pressed from. Every
 * keyboard case here waits for the move instead. Opening focuses the
 * *selected* item, not the first one, which is what makes the direction of an
 * arrow assertion meaningful.
 * **Inside a `<form>`, a `Select` reports back a value nobody chose.** Radix
 * mirrors the value into a hidden native `<select>` and dispatches `change` on
 * it whenever the value changes; that select's options are the items that have
 * registered, and items mount only while the list is open. So a controlled
 * value changed while closed is set against a select with no matching option,
 * lands as `""`, and arrives at `onValueChange` as `""`. It takes the option
 * set changing *with* the value to produce it — the two cases below are that
 * pair. `src/ui/Select.tsx` drops a value it never offered because of this,
 * and these cases are why.
 */
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Avatar, Collapsible, Dialog, DropdownMenu, Select, Separator, Tabs, Toggle, ToggleGroup, Toolbar, Tooltip, VisuallyHidden } from 'radix-ui'
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

  it('hides a Select’s options until it opens, and moves focus a tick after an arrow', async () => {
    render(
      <Select.Root defaultValue="empty">
        <Select.Trigger aria-label="Template">
          <Select.Value />
        </Select.Trigger>
        <Select.Portal>
          <Select.Content>
            <Select.Viewport>
              <Select.Item value="minimal">
                <Select.ItemText>minimal-expense-approval</Select.ItemText>
              </Select.Item>
              <Select.Item value="empty">
                <Select.ItemText>Empty pack</Select.ItemText>
              </Select.Item>
            </Select.Viewport>
          </Select.Content>
        </Select.Portal>
      </Select.Root>
    )
    const trigger = screen.getByRole('combobox', { name: 'Template' })
    // Closed: the trigger carries the choice and there are no options at all.
    expect(trigger.textContent).toContain('Empty pack')
    expect(screen.queryAllByRole('option')).toHaveLength(0)

    fireEvent.click(trigger)
    const options = await screen.findAllByRole('option')
    expect(options.map((option) => option.textContent)).toEqual([
      'minimal-expense-approval',
      'Empty pack'
    ])
    // Opening focuses the selected item, not the first one.
    expect(document.activeElement?.textContent).toBe('Empty pack')

    // And the arrow's focus move is deferred — asserted on the next line it
    // would still read "Empty pack", which is the trap this case records.
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' })
    await waitFor(() =>
      expect(document.activeElement?.textContent).toBe('minimal-expense-approval')
    )
  })

  it('reports "" from a Select whose value changed while closed, inside a form', async () => {
    const seen: string[] = []
    // The real shape: the options arrive with the new default, exactly as a
    // runtime's example listing does.
    function Bare({ value, names }: { value: string; names: string[] }) {
      return (
        <form>
          <Select.Root value={value} onValueChange={(next) => seen.push(next)}>
            <Select.Trigger aria-label="Template">
              <Select.Value />
            </Select.Trigger>
            <Select.Portal>
              <Select.Content>
                <Select.Viewport>
                  {names.map((name) => (
                    <Select.Item key={name} value={name}>
                      <Select.ItemText>{name}</Select.ItemText>
                    </Select.Item>
                  ))}
                </Select.Viewport>
              </Select.Content>
            </Select.Portal>
          </Select.Root>
        </form>
      )
    }
    const { rerender } = render(<Bare value="empty" names={['empty']} />)
    rerender(<Bare value="minimal" names={['minimal', 'empty']} />)
    // Not "minimal" — the empty string, reported back from a native select
    // whose options are one render behind the value being set on it.
    await waitFor(() => expect(seen).toContain(''))
    expect(seen).not.toContain('minimal')
    // A caller that stores what it is handed now holds "", and the next render
    // is the blank trigger the create dialog showed before `ui/Select.tsx`
    // started dropping values it never offered.
  })

  it('does not report "" when the value changes and the option set does not', async () => {
    // The other half, and the reason the case above builds the options up
    // rather than holding them still: it is the option set changing with the
    // value that puts the native select a render behind. A value moving
    // between options that were both already there is reported to nobody.
    const seen: string[] = []
    function Steady({ value }: { value: string }) {
      return (
        <form>
          <Select.Root value={value} onValueChange={(next) => seen.push(next)}>
            <Select.Trigger aria-label="Steady">
              <Select.Value />
            </Select.Trigger>
            <Select.Portal>
              <Select.Content>
                <Select.Viewport>
                  <Select.Item value="a">
                    <Select.ItemText>alpha</Select.ItemText>
                  </Select.Item>
                  <Select.Item value="b">
                    <Select.ItemText>beta</Select.ItemText>
                  </Select.Item>
                </Select.Viewport>
              </Select.Content>
            </Select.Portal>
          </Select.Root>
        </form>
      )
    }
    const { rerender } = render(<Steady value="a" />)
    rerender(<Steady value="b" />)
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(seen).toEqual([])
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

  it('reports a ToggleGroup deselect as the empty string, which is why the segmented control drops it', () => {
    // **The finding the segmented control is built around.** Pressing the item
    // that is already on is a deselect, and a single-value group reports the
    // empty string for it — a group with nothing chosen. That is a real state
    // for a toggle group and is not one Edit|Read has: "neither Edit nor Read"
    // is not a mode. `ui/SegmentedControl.tsx` refuses the empty answer, and
    // this case is why.
    const seen: string[] = []
    render(
      <ToggleGroup.Root type="single" value="edit" onValueChange={(next) => seen.push(next)}>
        <ToggleGroup.Item value="edit">Edit</ToggleGroup.Item>
        <ToggleGroup.Item value="read">Read</ToggleGroup.Item>
      </ToggleGroup.Root>
    )
    // Pressed and unpressed are reported per item, so the group's state is
    // readable without asking the caller what it stored.
    expect(screen.getByRole('radio', { name: 'Edit' }).getAttribute('data-state')).toBe('on')
    fireEvent.click(screen.getByRole('radio', { name: 'Read' }))
    expect(seen).toEqual(['read'])
    fireEvent.click(screen.getByRole('radio', { name: 'Edit' }))
    expect(seen).toEqual(['read', ''])
  })

  it('makes a Toolbar one tab stop with the arrow keys inside it', () => {
    render(
      <Toolbar.Root aria-label="Editing">
        <Toolbar.Button>Check</Toolbar.Button>
        <Toolbar.Separator />
        <Toolbar.Button>Save</Toolbar.Button>
      </Toolbar.Root>
    )
    const toolbar = screen.getByRole('toolbar', { name: 'Editing' })
    // **The stop is the toolbar, not its first button.** Before focus has
    // entered it, every item is `-1` and the container carries `0` — so a test
    // asserting the first button is the tab stop asserts a state this group
    // only reaches after it has been focused once.
    expect(toolbar.getAttribute('tabindex')).toBe('0')
    for (const button of toolbar.querySelectorAll('button')) {
      expect(button.getAttribute('tabindex')).toBe('-1')
    }
  })
})
