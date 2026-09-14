import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { useState } from 'react'
import type { PackDocument, TraceEntry } from '../mcp/types'
import type { RelationshipMap } from '../components/RelationshipMap'
import { PackLogic } from './PackLogic'
import { projectLogic } from './logicModel'
import { initialLogicDisplay, initialLogicMode, rememberLogicDisplay, rememberLogicMode, type LogicMode } from './logicState'

// Browser verification exercises real node measurement, pan and keyboard input.
vi.mock('../components/RelationshipMap', () => ({
  RelationshipMap: ({ nodes, onSelect, focusRequest }: Parameters<typeof RelationshipMap>[0]) => <div data-focus-node={focusRequest?.id}>{nodes.map(node =>
    <div key={node.id} data-map-node={node.id} data-search-match={node.matched || undefined}>
      <button aria-label={node.id} aria-current={node.selected || undefined}
        onClick={() => onSelect(node.id)}>{node.title}</button>{node.content}{node.observation && <p>{node.observation}</p>}
    </div>)}</div>
}))

afterEach(() => { cleanup(); localStorage.clear() })
const base = JSON.parse(readFileSync(join(import.meta.dirname, '__fixtures__', 'minimal.pack.json'), 'utf8')) as PackDocument
const doc = { ...base, applicability: { op: 'fact', path: '/case/type', operator: 'in', value: ['a', 'b', 'c', 'd'] },
  rules: [true, false].map((value, i) => ({ id: `rule-${i}`, description: 'A declared rule', onUnknown: 'escalate', outcome: i ? 'hold' : 'proceed',
    when: { op: 'all', conditions: [
      { op: 'fact', path: '/case/correctionApplied', operator: 'equals', value },
      { op: 'fact', path: '/amount', operator: 'greater-than', value: '5000' },
      { op: 'evidence-present', evidenceRequirement: 'confirmation' }
    ] } })),
  evidenceRequirements: [{ id: 'confirmation', description: 'Written confirmation.', required: true, kind: 'attestation' }],
  escalation: { triggers: ['unknown', 'conflict'], target: { kind: 'human-role', name: 'Compliance' } }
} as PackDocument
const inspect = vi.fn(), select = vi.fn()
function Example({ document = doc, mode: initial = 'list', trace }: { document?: PackDocument; mode?: LogicMode; trace?: TraceEntry[] }) {
  const [at, setAt] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [mode, setMode] = useState(initial)
  const [display, setDisplay] = useState(initialLogicDisplay)
  // A stable model is owned by the route in production.
  const [model] = useState(() => projectLogic(document))
  return <PackLogic model={model} at={at} groupId={null}
    select={pointer => { select(pointer); setAt(pointer) }} mode={mode} onMode={setMode}
    query={query} onQuery={setQuery} inspect={inspect} display={display} onDisplay={setDisplay}
    viewport={{ x: 0, y: 24, zoom: 1 }} onViewport={() => {}} listScroll={{ current: 0 }} trace={trace} />
}

it('makes scope, evidence, conditions, outcomes and handoff readable without inspection', () => {
  render(<Example />)
  const rules = screen.getByRole('region', { name: 'Decision rules' })
  expect(within(rules).getAllByText('/case/correctionApplied')).toHaveLength(2)
  expect(within(rules).getByText('true')).toBeTruthy()
  expect(within(rules).getByText('false')).toBeTruthy()
  expect(within(rules).getAllByText('"5000"')).toHaveLength(2)
  expect(screen.getByText('/case/type')).toBeTruthy()
  expect(screen.getByText(/Required · attestation/)).toBeTruthy()
  expect(screen.getByText('No fallback outcome')).toBeTruthy()
  expect(screen.getByText('Compliance')).toBeTruthy()
  expect(screen.getByText('Conflicting outcomes')).toBeTruthy()
  expect(within(rules).getAllByText('Keep the result unresolved')).toHaveLength(2)
  expect(document.querySelector('[data-pointer]')).toBeNull()
})

it('selects each real map item directly without a group or Outline step', async () => {
  render(<Example mode="map" />)
  await screen.findByRole('button', { name: '/rules/0' })
  for (const id of ['/rules/0', '/rules/1', '/outcomes/0', '/outcomes/1']) {
    const button = screen.getByRole('button', { name: id })
    fireEvent.click(button)
    expect(button.getAttribute('aria-current')).toBe('true')
    expect(select).toHaveBeenLastCalledWith(id)
  }
  expect(inspect).not.toHaveBeenCalled()
  expect(screen.getByText('true')).toBeTruthy()
  expect(screen.getByText('false')).toBeTruthy()
})

it('remembers an optional compact display, while search exposes matching conditions', () => {
  rememberLogicDisplay({ conditions: false, grouped: false })
  render(<Example />)
  expect(screen.queryByText('/case/correctionApplied')).toBeNull()
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'correctionApplied' } })
  expect(screen.getAllByText('/case/correctionApplied')).toHaveLength(2)
  expect(screen.getByRole('status').textContent).toBe('2 matching items')
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'does-not-exist' } })
  expect(screen.getByRole('status').textContent).toContain('No items match')
  expect(screen.getByRole('button', { name: 'Next match' }).hasAttribute('disabled')).toBe(true)
})

it('highlights map matches immediately and jumps without opening the Inspector', async () => {
  render(<Example mode="map" />)
  await screen.findByRole('button', { name: '/rules/0' })
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'correctionApplied' } })
  expect(document.querySelectorAll('[data-map-node][data-search-match]')).toHaveLength(2)
  fireEvent.submit(screen.getByRole('searchbox').closest('form')!)
  expect(document.querySelector('[data-focus-node]')?.getAttribute('data-focus-node')).toBe('/rules/0')
  fireEvent.click(screen.getByRole('button', { name: 'Next match' }))
  expect(document.querySelector('[data-focus-node]')?.getAttribute('data-focus-node')).toBe('/rules/1')
  expect(inspect).not.toHaveBeenCalled()
})

it('expands a large outcome group on the main canvas', async () => {
  rememberLogicDisplay({ conditions: true, grouped: true })
  render(<Example mode="map" document={{ ...doc, rules: Array.from({ length: 80 }, (_, i) => ({ ...doc.rules[0]!, id: `rule-${i}` })) }} />)
  fireEvent.click(await screen.findByRole('button', { name: 'rules:"proceed"' }))
  expect(await screen.findByRole('button', { name: '/rules/79' })).toBeTruthy()
  expect(screen.getAllByText('/case/correctionApplied')).toHaveLength(80)
  expect(inspect).not.toHaveBeenCalled()
})

it('keeps invalid definitions readable in List when Map is unavailable', () => {
  render(<PackLogic model={projectLogic(doc)} at={null} groupId={null} select={select}
    mode="map" onMode={vi.fn()} query="" onQuery={vi.fn()} inspect={inspect} display={initialLogicDisplay()} onDisplay={vi.fn()}
    viewport={{ x: 0, y: 0, zoom: 1 }} onViewport={vi.fn()} listScroll={{ current: 0 }} mapUnavailable="Validation needed" />)
  expect(screen.getByText('Validation needed')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Read List' })).toBeTruthy()
})

it('keeps mixed recorded observations explicit in a grouped map', async () => {
  rememberLogicDisplay({ conditions: true, grouped: true })
  render(<Example mode="map" document={{ ...doc, rules: [0, 1, 2].map(i => ({ ...doc.rules[0]!, id: `rule-${i}` })) }}
    trace={[{ stage: 'rule', id: 'rule-0', condition: 'true' }, { stage: 'rule', id: 'rule-1', condition: 'unknown' }]} />)
  expect(await screen.findByText('Recorded conditions: 1 Met · 1 Cannot determine · 1 Unreported')).toBeTruthy()
})

it('defaults to detailed List and honors saved choices and unavailable storage', () => {
  expect(initialLogicMode()).toBe('list')
  expect(initialLogicDisplay()).toEqual({ conditions: true, grouped: false })
  rememberLogicMode('map')
  expect(initialLogicMode()).toBe('map')
  localStorage.setItem('jp-desk:pack-logic-display:v1', '{broken')
  expect(initialLogicDisplay()).toEqual({ conditions: true, grouped: false })
  const unavailable = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('Unavailable') })
  expect(initialLogicDisplay()).toEqual({ conditions: true, grouped: false })
  expect(initialLogicMode()).toBe('list')
  unavailable.mockRestore()
})
