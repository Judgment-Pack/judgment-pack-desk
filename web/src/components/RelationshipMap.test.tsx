import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ReactFlowProps } from '@xyflow/react'
import { RelationshipMap } from './RelationshipMap'

const captured = vi.hoisted(() => ({ props: null as ReactFlowProps | null }))
vi.mock('@xyflow/react', async original => ({
  applyNodeChanges: (await original<typeof import('@xyflow/react')>()).applyNodeChanges,
  ReactFlow: (props: ReactFlowProps) => { captured.props = props; return <div /> },
  ViewportPortal: () => null,
  Handle: () => null, Position: { Left: 'left', Right: 'right' }, MarkerType: { ArrowClosed: 'arrowclosed' }
}))
afterEach(cleanup)
it('retains measured dimensions on controlled pan updates and accepts later text/size changes', () => {
  const draw = (x: number, title = 'Rule') => <RelationshipMap nodes={[
    { id: 'one', title, column: 0, content: <p>{title}</p>, action: 'Inspect' },
    { id: 'two', title: 'Next', column: 0, content: null, action: 'Inspect' }
  ]} edges={[]} unit={16} viewport={{ x, y: 24, zoom: 1 }} onSelect={() => {}} onInspect={() => {}} onViewportChange={() => {}} />
  const { rerender } = render(draw(0))
  act(() => captured.props!.onNodesChange!([{ id: 'one', type: 'dimensions', dimensions: { width: 256, height: 120 } }]))
  rerender(draw(80))
  expect(captured.props!.nodes![0]!.measured).toEqual({ width: 256, height: 120 })
  expect(captured.props!.nodes![1]!.position.y).toBe(152)
  rerender(draw(80, 'Long translated condition'))
  act(() => captured.props!.onNodesChange!([{ id: 'one', type: 'dimensions', dimensions: { width: 256, height: 220 } }]))
  expect(captured.props!.nodes![0]!.measured?.height).toBe(220)
  expect(captured.props!.nodes![1]!.position.y).toBe(252)
  expect(captured.props!.viewport).toEqual({ x: 80, y: 24, zoom: 1 })
})

it('auto arranges measured nodes after dragging without clearing selection', () => {
  const viewport = { x: 50, y: 20, zoom: .8 }
  const view = (selected = false) => <RelationshipMap nodes={[{ id: 'one', title: 'Rule', column: 0, content: null, action: 'Inspect', selected }]} edges={[]} unit={16} viewport={viewport} onSelect={() => {}} onInspect={() => {}} onViewportChange={() => {}} />
  const { rerender } = render(view())
  act(() => captured.props!.onInit!({ fitView: vi.fn() } as never))
  act(() => captured.props!.onNodesChange!([{ id: 'one', type: 'position', position: { x: 250, y: 320 }, dragging: true }]))
  act(() => captured.props!.onNodesChange!([{ id: 'one', type: 'dimensions', dimensions: { width: 256, height: 200 } }]))
  rerender(view(true))
  expect(captured.props!.nodes![0]!.position).toEqual({ x: 250, y: 320 })
  expect(captured.props!.nodes![0]!.measured).toEqual({ width: 256, height: 200 })
  fireEvent.click(screen.getByRole('button', { name: 'Auto arrange' }))
  expect(captured.props!.nodes![0]!.position).toEqual({ x: 16, y: 0 })
  expect(captured.props!.viewport).toEqual(viewport)
  expect(captured.props!.nodes![0]!.selected).toBe(true)
})

it('emphasizes complete paths, retains exclusion dashes, and fits only highlighted nodes',()=>{
 const clear=vi.fn(), fitView=vi.fn()
 const nodes=['exception','rule','outcome','other'].map((id,column)=>({id,title:id,column,content:null,action:'Inspect',selected:id==='outcome'}))
 render(<RelationshipMap nodes={nodes} edges={[{id:'exclude',source:'exception',target:'rule',semantic:'excludes'},{id:'contribute',source:'rule',target:'outcome',semantic:'contributes'}]} unit={16} viewport={{x:0,y:24,zoom:1}} onSelect={()=>{}} onInspect={()=>{}} onViewportChange={()=>{}} onClearSelection={clear}/>)
 expect(captured.props!.edges!.map(edge=>edge.style?.strokeWidth)).toEqual([2.5,2.5])
 expect(captured.props!.edges![0]!.style?.strokeDasharray).toBe('5 4')
 expect(captured.props!.nodes!.find(node=>node.id==='other')!.data.muted).toBe(true)
 expect(captured.props!.nodes!.find(node=>node.id==='exception')!.data.related).toBe(true)
 act(()=>captured.props!.onInit!({fitView} as never))
 fireEvent.click(screen.getByRole('button',{name:'Fit highlighted path'}))
 expect(fitView).toHaveBeenCalledWith(expect.objectContaining({nodes:[{id:'exception'},{id:'rule'},{id:'outcome'}]}))
 fireEvent.keyDown(screen.getByLabelText('Pack relationship map'),{key:'Escape'})
 expect(clear).toHaveBeenCalledOnce()
 act(()=>captured.props!.onPaneClick!({} as never))
 expect(clear).toHaveBeenCalledTimes(2)
})

it('includes an authored meaning in the accessible outcome name',()=>{
 render(<RelationshipMap nodes={[{id:'outcome',title:'Refer to owner',column:0,content:null,action:'View details',kind:'outcome',kindLabel:'Outcome',meaning:'review',meaningLabel:'Review'}]} edges={[]} unit={16} viewport={{x:0,y:24,zoom:1}} onSelect={()=>{}} onInspect={()=>{}} onViewportChange={()=>{}}/>)
 expect(captured.props!.nodes![0]!.ariaLabel).toBe('Outcome · Review: Refer to owner. View details')
 expect(screen.getByRole('button',{name:'Auto arrange'})).toBeTruthy()
})

it('keeps drag frames local and retains unaffected nodes, edge definitions and callbacks', () => {
  const publish = vi.fn(), viewport = { x: 0, y: 24, zoom: 1 }
  const nodes = [
    { id: 'a', title: 'A', column: 0, content: null, action: 'Inspect' },
    { id: 'b', title: 'B', column: 1, content: null, action: 'Inspect' }
  ]
  const edges = [{ id: 'edge', source: 'a', target: 'b' }]
  const props = { nodes, edges, unit: 16, viewport, onSelect: vi.fn(), onInspect: vi.fn(), onViewportChange: vi.fn(), onNodePositionsChange: publish }
  const { rerender } = render(<RelationshipMap {...props} nodePositions={{}} />)
  const before = captured.props!, untouched = before.nodes![1]!, data = before.nodes![0]!.data
  for (let i = 1; i <= 60; i++) act(() => captured.props!.onNodesChange!([{ id: 'a', type: 'position', position: { x: i, y: i * 2 }, dragging: true }]))
  expect(publish).not.toHaveBeenCalled()
  expect(captured.props!.nodes![0]!.position).toEqual({ x: 60, y: 120 })
  expect(captured.props!.nodes![0]!.data).toBe(data)
  expect(captured.props!.nodes![1]).toBe(untouched)
  expect(captured.props!.edges).toBe(before.edges)
  expect(captured.props!.onNodesChange).toBe(before.onNodesChange)
  expect(captured.props!.onNodeClick).toBe(before.onNodeClick)
  act(() => captured.props!.onNodesChange!([{ id: 'a', type: 'position', position: { x: 60, y: 120 }, dragging: false }]))
  expect(publish).toHaveBeenCalledExactlyOnceWith({ a: { x: 60, y: 120 } })
  rerender(<RelationshipMap {...props} nodePositions={publish.mock.calls[0]![0]} />)
  expect(captured.props!.nodes![0]!.position).toEqual({ x: 60, y: 120 })
  expect(captured.props!.nodes![1]).toBe(untouched)
  rerender(<RelationshipMap {...props} nodePositions={{}} />)
  expect(captured.props!.nodes![0]!.position).toEqual({ x: 16, y: 0 })
})

it('keeps viewport movement local until its gesture ends and honors external view changes', () => {
  const publish = vi.fn(), nodes = [{ id: 'one', title: 'One', column: 0, content: null, action: 'Inspect' }]
  const props = { nodes, edges: [], unit: 16, onSelect: vi.fn(), onInspect: vi.fn(), onViewportChange: publish }
  const { rerender } = render(<RelationshipMap {...props} viewport={{ x: 0, y: 24, zoom: 1 }} />)
  const next = { x: 50, y: 70, zoom: .75 }, before = captured.props!.nodes
  act(() => captured.props!.onViewportChange!(next))
  expect(publish).not.toHaveBeenCalled()
  expect(captured.props!.viewport).toEqual(next)
  expect(captured.props!.nodes).toBe(before)
  act(() => captured.props!.onMoveEnd!(null, next))
  expect(publish).toHaveBeenCalledExactlyOnceWith(next)
  rerender(<RelationshipMap {...props} viewport={{ x: 0, y: 24, zoom: 1 }} />)
  expect(captured.props!.viewport).toEqual({ x: 0, y: 24, zoom: 1 })
})
