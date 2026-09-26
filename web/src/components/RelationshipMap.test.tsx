import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ReactFlowProps } from '@xyflow/react'
import { RelationshipMap } from './RelationshipMap'

const captured = vi.hoisted(() => ({ props: null as ReactFlowProps | null }))
vi.mock('@xyflow/react', () => ({
  ReactFlow: (props: ReactFlowProps) => { captured.props = props; return <div /> },
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

it('retains a dragged node through measurement, selection and panning, then resets only layout', () => {
  const viewport = { x: 50, y: 20, zoom: .8 }
  const view = (selected = false) => <RelationshipMap nodes={[{ id: 'one', title: 'Rule', column: 0, content: null, action: 'Inspect', selected }]} edges={[]} unit={16} viewport={viewport} onSelect={() => {}} onInspect={() => {}} onViewportChange={() => {}} />
  const { rerender } = render(view())
  act(() => captured.props!.onNodesChange!([{ id: 'one', type: 'position', position: { x: 250, y: 320 }, dragging: true }]))
  act(() => captured.props!.onNodesChange!([{ id: 'one', type: 'dimensions', dimensions: { width: 256, height: 200 } }]))
  rerender(view(true))
  expect(captured.props!.nodes![0]!.position).toEqual({ x: 250, y: 320 })
  expect(captured.props!.nodes![0]!.measured).toEqual({ width: 256, height: 200 })
  fireEvent.click(screen.getByRole('button', { name: 'Reset layout' }))
  expect(captured.props!.nodes![0]!.position).toEqual({ x: 16, y: 0 })
  expect(captured.props!.viewport).toEqual(viewport)
})
