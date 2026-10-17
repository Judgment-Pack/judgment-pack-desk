import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { Select } from './Select'

afterEach(cleanup)

const OPTIONS = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'system', label: 'System', disabled: true }
]

function mount(open = true) {
  const changed = vi.fn()
  const committed = vi.fn()
  const view = render(<Select id="theme" aria-label="Theme" value="dark"
    options={OPTIONS} onValueChange={changed} onValueCommit={committed} />)
  if (open) fireEvent.keyDown(screen.getByRole('combobox', { name: 'Theme' }), { key: 'Enter' })
  return { ...view, changed, committed }
}

it.each(['pointer', 'Enter', ' '] as const)('reports an explicit same-value %j activation once without reporting a value change', activation => {
  const { changed, committed } = mount()
  const option = screen.getByRole('option', { name: 'Dark' })
  if (activation === 'pointer') {
    fireEvent.pointerDown(option, { pointerType: 'mouse' })
    fireEvent.pointerUp(option, { pointerType: 'mouse' })
    fireEvent.click(option)
  } else {
    fireEvent.keyDown(option, { key: activation })
  }
  expect(screen.queryByRole('listbox')).toBeNull()
  expect(committed).toHaveBeenCalledExactlyOnceWith('dark')
  expect(changed).not.toHaveBeenCalled()
})

it.each(['pointer', 'Enter', ' '] as const)('reports a changed %j option through both callbacks once', activation => {
  const { changed, committed } = mount()
  const option = screen.getByRole('option', { name: 'Light' })
  if (activation === 'pointer') {
    fireEvent.pointerDown(option, { pointerType: 'mouse' })
    fireEvent.pointerUp(option, { pointerType: 'mouse' })
    fireEvent.click(option)
  } else {
    fireEvent.keyDown(option, { key: activation })
  }
  expect(committed).toHaveBeenCalledExactlyOnceWith('light')
  expect(changed).toHaveBeenCalledExactlyOnceWith('light')
})

it('reports a closed-trigger typeahead choice through both callbacks once', () => {
  const { changed, committed } = mount(false)
  fireEvent.keyDown(screen.getByRole('combobox', { name: 'Theme' }), { key: 'l' })
  expect(screen.queryByRole('listbox')).toBeNull()
  expect(committed).toHaveBeenCalledExactlyOnceWith('light')
  expect(changed).toHaveBeenCalledExactlyOnceWith('light')
})

it('does not commit a value supplied by a prop change', () => {
  const changed = vi.fn()
  const committed = vi.fn()
  const element = (value: string) => <form><Select id="theme" aria-label="Theme" value={value}
    options={OPTIONS} onValueChange={changed} onValueCommit={committed} /></form>
  const view = render(element('dark'))
  view.rerender(element('light'))
  expect(screen.getByRole('combobox', { name: 'Theme' }).textContent).toContain('Light')
  expect(committed).not.toHaveBeenCalled()
  expect(changed).not.toHaveBeenCalled()
})

it('does not commit when opening, navigating, typing, or dismissing the menu', () => {
  const { changed, committed } = mount()
  const option = screen.getByRole('option', { name: 'Dark' })
  fireEvent.keyDown(option, { key: 'ArrowDown' })
  fireEvent.keyDown(option, { key: 'd' })
  fireEvent.keyDown(option, { key: ' ' })
  expect(screen.getByRole('listbox')).toBeTruthy()
  fireEvent.keyDown(option, { key: 'Escape' })
  expect(screen.queryByRole('listbox')).toBeNull()
  expect(committed).not.toHaveBeenCalled()
  expect(changed).not.toHaveBeenCalled()
})

it('does not commit a disabled option', () => {
  const { changed, committed } = mount()
  const option = screen.getByRole('option', { name: 'System' })
  fireEvent.click(option)
  fireEvent.keyDown(option, { key: 'Enter' })
  expect(screen.getByRole('listbox')).toBeTruthy()
  fireEvent.keyDown(option, { key: 'Escape' })
  expect(committed).not.toHaveBeenCalled()
  expect(changed).not.toHaveBeenCalled()
})
