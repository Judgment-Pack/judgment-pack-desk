import { Tooltip } from '../ui/Tooltip'
import { useMemo, type KeyboardEvent } from 'react'
import { ReactFlow, Handle, Position, MarkerType, type Node, type NodeProps, type Viewport } from '@xyflow/react'
import '@xyflow/react/dist/base.css'
import { Button } from '../ui/Button'
import styles from './RelationshipMap.module.css'

export interface RelationshipNode { id: string; title: string; description: string; x: number; y: number; selected?: boolean; observation?: string }
export interface RelationshipEdge { id: string; source: string; target: string; label?: string }
type GroupNode = Node<{ title: string; description: string; observation?: string }, 'relationship'>
function Group({ data, selected }: NodeProps<GroupNode>) {
  return <div className={[styles.node, selected ? styles.selected : ''].join(' ')}>
    <Handle type="target" position={Position.Left} className={styles.handle} />
    <strong>{data.title}</strong><span>{data.description}</span>
    {data.observation && <span className={styles.observation}>{data.observation}</span>}
    <Handle type="source" position={Position.Right} className={styles.handle} />
  </div>
}
const nodeTypes = { relationship: Group }

/** Read-only canvas infrastructure; callers own document and edge semantics. */
export function RelationshipMap({ nodes, edges, unit, viewport, onViewportChange, onInspect }: {
  nodes: RelationshipNode[]; edges: RelationshipEdge[]; unit: number; viewport: Viewport
  onViewportChange: (next: Viewport) => void; onInspect: (id: string) => void
}) {
  const flowNodes = useMemo<GroupNode[]>(() => nodes.map(n => ({
    id: n.id, type: 'relationship', position: { x: n.x * unit, y: n.y * unit },
    data: { title: n.title, description: n.description, observation: n.observation },
    selected: n.selected, ariaLabel: n.title, ariaRole: 'button',
    domAttributes: { 'aria-pressed': Boolean(n.selected) },
    // Fixed nodes still own clicks/taps; a slight movement must not start panning.
    className: 'nopan', draggable: false, connectable: false
  })), [nodes, unit])
  const flowEdges = useMemo(() => edges.map(e => ({ ...e, type: 'smoothstep',
    markerEnd: { type: MarkerType.ArrowClosed, color: 'var(--ink-faint)' },
    labelStyle: { fill: 'var(--ink-soft)', fontSize: .75 * unit },
    labelBgStyle: { fill: 'var(--bg)' },
  })), [edges, unit])
  const keyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Enter' && event.key !== ' ') return
    const node = (event.target as HTMLElement).closest<HTMLElement>('.react-flow__node')
    const id = node?.dataset.id
    if (id) { event.preventDefault(); event.stopPropagation(); onInspect(id) }
  }
  return <div className={styles.map} onKeyDownCapture={keyDown} aria-label="Pack relationship map">
    <ReactFlow nodes={flowNodes} edges={flowEdges} nodeTypes={nodeTypes}
      proOptions={{ hideAttribution: true }}
      viewport={viewport} onViewportChange={onViewportChange}
      onNodeClick={(_event, node) => onInspect(node.id)}
      nodesDraggable={false} nodesConnectable={false} edgesReconnectable={false}
      edgesFocusable={false} elementsSelectable={false} nodesFocusable
      deleteKeyCode={null} selectionKeyCode={null} panOnScroll zoomOnScroll={false}
      minZoom={0.5} maxZoom={2} preventScrolling={false}
      ariaLabelConfig={{ 'node.a11yDescription.default': 'Press Enter or Space to inspect this group.' }} />
    <div className={styles.controls} aria-label="Map zoom">
      <Tooltip content="Zoom out"><Button aria-label="Zoom out" onClick={() => onViewportChange({ ...viewport, zoom: Math.max(.5, viewport.zoom - .25) })}>−</Button></Tooltip>
      <Tooltip content="Reset map view"><Button aria-label="Reset map view" onClick={() => onViewportChange({ x: 0, y: 24, zoom: 1 })}>{Math.round(viewport.zoom * 100)}%</Button></Tooltip>
      <Tooltip content="Zoom in"><Button aria-label="Zoom in" onClick={() => onViewportChange({ ...viewport, zoom: Math.min(2, viewport.zoom + .25) })}>+</Button></Tooltip>
    </div>
  </div>
}
