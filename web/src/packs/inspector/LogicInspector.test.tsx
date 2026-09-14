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
  it('adds supporting details beside Logic and restores conditions for compact reading', () => {
    const props = { model: projectLogic(doc), at: '/rules/1', onSelect: () => {}, advanced: <p>Validation and provenance</p> }
    const { container, rerender } = render(<LogicInspector {...props} mainContent />)
    expect(container.querySelector('[data-condition-tree]')).toBeNull()
    expect(screen.queryByRole('heading', { name: 'Contributes outcome' })).toBeNull()
    expect(screen.getByText('Validation and provenance')).toBeTruthy()
    expect(screen.getByText('Technical details')).toBeTruthy()
    rerender(<LogicInspector {...props} mainContent conditionsVisible={false} />)
    expect(container.querySelector('[data-condition-tree]')).not.toBeNull()
    rerender(<LogicInspector {...props} />)
    expect(screen.getByRole('heading', { name: 'Contributes outcome' })).toBeTruthy()
  })
  it('shows pack metadata when no item is selected, without a duplicate outline', () => {
    render(<LogicInspector model={projectLogic(doc)} at={null} onSelect={() => {}} advanced={<p>Pack validation</p>} mainContent />)
    expect(screen.getByRole('heading', { name: 'Pack details' })).toBeTruthy()
    expect(screen.queryByText('Open Outline')).toBeNull()
    expect(screen.getByText('Pack validation')).toBeTruthy()
  })
  it.each(['rules', 'resolution', 'sources'])('opens only the %s group and selects real member pointers', id => {
    const model = projectLogic(doc)
    const group = model.groups.find(group => group.id === id)!
    const select = vi.fn()
    render(<LogicInspector model={model} at={null} groupId={id}
      onSelect={select} advanced={null} />)
    expect(screen.getByRole('heading', { name: `${group.label} · ${group.items.length}` })).toBeTruthy()
    expect(document.querySelectorAll('[data-outline-pointer]')).toHaveLength(group.items.length)
    const item = group.items[0]!
    fireEvent.click(document.querySelector(`[data-outline-pointer="${item.pointer}"]`)!)
    expect(select).toHaveBeenCalledExactlyOnceWith(item.pointer)
    expect(screen.queryByText('Exact definition JSON')).toBeNull()
  })
  it('makes an empty group inspectable without inventing a member', () => {
    render(<LogicInspector model={projectLogic({ ...doc, sources: [] })} at={null} groupId="sources"
      onSelect={() => {}} advanced={null} />)
    expect(screen.getByRole('heading', { name: 'Source references · 0' })).toBeTruthy()
    expect(screen.getByText('None declared.')).toBeTruthy()
    expect(document.querySelector('[data-pointer]')).toBeNull()
  })
  it('shows exact nested conditions separately from the author description and raw JSON', () => {
    render(<LogicInspector model={projectLogic(doc)} at="/rules/1"
      onSelect={() => {}} advanced={null} />)
    expect(document.querySelector('[data-pointer]')).toBeNull()
    expect(screen.getByText('is greater than')).toBeTruthy()
    const raw = screen.getByText('Exact condition JSON').closest('details')!
    expect(raw.open).toBe(false)
    expect(JSON.parse(raw.querySelector('pre')!.textContent!)).toEqual(doc.rules[1]!.when)
    expect(screen.getByText('Author description').closest('details')!.open).toBe(false)
  })
})
