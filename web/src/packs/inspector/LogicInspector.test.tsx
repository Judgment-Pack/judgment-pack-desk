import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { projectLogic } from '../logicModel'
import { LogicInspector } from './LogicInspector'
import type { PackDocument } from '../../mcp/types'

const doc: PackDocument = JSON.parse(readFileSync(join(import.meta.dirname, '..', '__fixtures__', 'full.pack.json'), 'utf8'))
afterEach(cleanup)
describe('contextual pack inspection', () => {
  it.each(['rules', 'resolution', 'sources'])('opens only the %s group and selects real member pointers', id => {
    const model = projectLogic(doc)
    const group = model.groups.find(group => group.id === id)!
    const select = vi.fn()
    render(<LogicInspector model={model} at={null} groupId={id} pane="detail" query="unrelated search"
      onSelect={select} onOutline={() => {}} outlineScroll={{ current: 0 }} advanced={null} />)
    expect(screen.getByRole('heading', { name: `${group.label} · ${group.items.length}` })).toBeTruthy()
    expect(document.querySelectorAll('[data-outline-pointer]')).toHaveLength(group.items.length)
    const item = group.items[0]!
    fireEvent.click(document.querySelector(`[data-outline-pointer="${item.pointer}"]`)!)
    expect(select).toHaveBeenCalledExactlyOnceWith(item.pointer)
    expect(screen.queryByText('Exact definition JSON')).toBeNull()
  })
  it('makes an empty group inspectable without inventing a member', () => {
    render(<LogicInspector model={projectLogic({ ...doc, sources: [] })} at={null} groupId="sources" pane="detail" query=""
      onSelect={() => {}} onOutline={() => {}} outlineScroll={{ current: 0 }} advanced={null} />)
    expect(screen.getByRole('heading', { name: 'Sources · 0' })).toBeTruthy()
    expect(screen.getByText('None declared.')).toBeTruthy()
    expect(document.querySelector('[data-pointer]')).toBeNull()
  })
  it('finds an exception in the same outline as rules and preserves its pointer', () => {
    const select = vi.fn()
    render(<LogicInspector model={projectLogic(doc)} at={null} pane="outline" query={doc.exceptions![0]!.id}
      onSelect={select} onOutline={() => {}} outlineScroll={{ current: 0 }} advanced={null} />)
    const button = document.querySelector<HTMLButtonElement>('[data-outline-pointer="/exceptions/0"]')!
    expect(button).not.toBeNull(); fireEvent.click(button)
    expect(select).toHaveBeenCalledWith('/exceptions/0')
    expect(document.querySelector('[data-outline-pointer="/rules/0"]')).toBeNull()
  })
  it('shows exact nested conditions separately from the author description and raw JSON', () => {
    render(<LogicInspector model={projectLogic(doc)} at="/rules/1" pane="detail" query=""
      onSelect={() => {}} onOutline={() => {}} outlineScroll={{ current: 0 }} advanced={null} />)
    expect(document.querySelector('[data-pointer]')).toBeNull()
    expect(screen.getByText('greater-than')).toBeTruthy()
    const raw = screen.getByText('Exact condition JSON').closest('details')!
    expect(raw.open).toBe(false)
    expect(JSON.parse(raw.querySelector('pre')!.textContent!)).toEqual(doc.rules[1]!.when)
    expect(screen.getByText('Author description').closest('details')!.open).toBe(false)
  })
})
