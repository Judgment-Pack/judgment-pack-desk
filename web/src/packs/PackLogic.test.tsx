import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { useState } from 'react'
import type { PackDocument } from '../mcp/types'
import type { RelationshipMap } from '../components/RelationshipMap'
import { PackLogic } from './PackLogic'
import { projectLogic } from './logicModel'

// The browser regression exercises React Flow itself. Here, retain its public
// selection/click contract to test every document-group transition deterministically.
vi.mock('../components/RelationshipMap', () => ({
  RelationshipMap: ({ nodes, onInspect }: Parameters<typeof RelationshipMap>[0]) => <div>{nodes.map(node =>
    <button key={node.id} aria-label={node.id} aria-pressed={Boolean(node.selected)}
      onClick={() => onInspect(node.id)}>{node.title}</button>)}</div>
}))

afterEach(cleanup)
const doc = JSON.parse(readFileSync(join(import.meta.dirname, '__fixtures__', 'full.pack.json'), 'utf8')) as PackDocument

it('selects every map node, including multi-item and empty groups, without opening the general outline', async () => {
  const model = projectLogic({ ...doc, sources: [] })
  const outline = vi.fn()
  function Example() {
    const [at, setAt] = useState<string | null>('/applicability')
    const [groupId, setGroup] = useState<string | null>(null)
    return <PackLogic model={model} at={at} groupId={groupId}
      select={pointer => { setAt(pointer); setGroup(null) }}
      inspectGroup={id => { setGroup(id); setAt(null) }} mode="map" onMode={() => {}}
      query="" onQuery={() => {}} openOutline={outline}
      viewport={{ x: 0, y: 24, zoom: 1 }} onViewport={() => {}} listScroll={{ current: 0 }} />
  }
  render(<Example />)
  await screen.findByRole('button', { name: 'applicability' })
  for (const group of model.groups) {
    const button = screen.getByRole('button', { name: group.id })
    fireEvent.click(button)
    expect(button.getAttribute('aria-pressed'), group.id).toBe('true')
    expect(screen.getAllByRole('button', { pressed: true })).toHaveLength(1)
  }
  expect(outline).not.toHaveBeenCalled()
})
