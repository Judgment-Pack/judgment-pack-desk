import { Tooltip } from '../ui/Tooltip'
import { useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { ReactFlow, Handle, Position, MarkerType, type Node, type NodeProps, type Viewport, type NodeChange, type ReactFlowInstance } from '@xyflow/react'
import '@xyflow/react/dist/base.css'
import { Button } from '../ui/Button'
import styles from './RelationshipMap.module.css'

export interface RelationshipNode {
  id: string; title: string; column: number; content: ReactNode; action: string
  selected?: boolean; matched?: boolean; observation?: string
}
export interface RelationshipEdge { id: string; source: string; target: string; label?: string }
type ReadNode = Node<Omit<RelationshipNode, 'id' | 'column'> & { inspect: () => void; width?: number }, 'relationship'>
function ReadingNode({ data, selected }: NodeProps<ReadNode>) {
  return <div className={[styles.node, selected ? styles.selected : '', data.matched ? styles.matched : ''].join(' ')} style={data.width === undefined ? undefined : { width: data.width }}>
    <Handle type="target" position={Position.Left} className={styles.handle} />
    <strong className={styles.title}>{data.title}</strong>
    {data.matched && <span className={styles.match}>Search match</span>}
    <div className={styles.content}>{data.content}</div>
    {data.observation && <span className={styles.observation}>{data.observation}</span>}
    <Button variant="quiet" className={styles.action} aria-label={`${data.action}: ${data.title}`}
      onClick={event => { event.stopPropagation(); data.inspect() }}>{data.action} <span aria-hidden="true">›</span></Button>
    <Handle type="source" position={Position.Right} className={styles.handle} />
  </div>
}
const nodeTypes = { relationship: ReadingNode }

/** Stacks measured nodes in their declared columns; changing text, density or
 * display options never overlaps nodes or silently zooms the reader out. */
export function relationshipPositions(nodes: Pick<RelationshipNode, 'id' | 'column'>[], sizes: Record<string, { width: number; height: number }>, unit: number, columnGap = 5, nodeWidth = 22 * unit) {
  const widths = new Map<number, number>(), heights = new Map<number, number>()
  nodes.forEach(n => widths.set(n.column, Math.max(widths.get(n.column) ?? nodeWidth, sizes[n.id]?.width ?? 0)))
  const offsets = new Map<number, number>(); let x = unit
  for (const column of [...widths.keys()].sort((a, b) => a - b)) {
    offsets.set(column, x); x += widths.get(column)! + columnGap * unit
  }
  return new Map(nodes.map(n => {
    const y = heights.get(n.column) ?? 0
    heights.set(n.column, y + (sizes[n.id]?.height ?? 16 * unit) + 2 * unit)
    return [n.id, { x: offsets.get(n.column)!, y }]
  }))
}

/** Read-only canvas infrastructure; callers own document and edge semantics. */
export function RelationshipMap({ nodes, edges, unit, viewport, onViewportChange, onSelect, onInspect, focusRequest, ariaLabel = 'Pack relationship map', onEdgeInspect, columnGap = 5, nodeWidth }: {
  nodes: RelationshipNode[]; edges: RelationshipEdge[]; unit: number; viewport: Viewport
  onViewportChange: (next: Viewport) => void; onSelect: (id: string) => void; onInspect: (id: string) => void
  columnGap?: number; nodeWidth?: number
  ariaLabel?: string; onEdgeInspect?: (id: string) => void
  focusRequest?: { id: string; sequence: number }
}) {
  const [sizes, setSizes] = useState<Record<string, { width: number; height: number }>>({})
  const [instance, setInstance] = useState<ReactFlowInstance<ReadNode> | null>(null)
  const root = useRef<HTMLDivElement>(null)
  const positions = useMemo(() => relationshipPositions(nodes, sizes, unit, columnGap, nodeWidth), [nodes, sizes, unit, columnGap, nodeWidth])
  const flowNodes = useMemo<ReadNode[]>(() => nodes.map(n => ({
    id: n.id, type: 'relationship', position: positions.get(n.id)!,
    data: { ...n, width: nodeWidth, inspect: () => onInspect(n.id) }, selected: n.selected, ariaLabel: `Select: ${n.title}`, ariaRole: 'group',
    domAttributes: { 'aria-current': n.selected ? 'true' : undefined, 'data-search-match': n.matched ? 'true' : undefined },
    className: 'nopan', draggable: false, connectable: false
  })), [nodes, positions, onInspect, nodeWidth])
  const flowEdges = useMemo(() => edges.map(e => ({ ...e, type: 'smoothstep',
    ariaLabel: `Connection: ${e.source} to ${e.target}`,
    markerEnd: { type: MarkerType.ArrowClosed, color: 'var(--ink-faint)' },
    labelStyle: { fill: 'var(--ink-soft)', fontSize: .75 * unit },
    labelBgStyle: { fill: 'var(--bg)' },
  })), [edges, unit])
  const onNodesChange = (changes: NodeChange<ReadNode>[]) => {
    const dimensions = changes.filter(c => c.type === 'dimensions' && c.dimensions)
    if (!dimensions.length) return
    setSizes(previous => {
      let next = previous
      for (const change of dimensions) if (change.type === 'dimensions' && change.dimensions) {
        const size = change.dimensions
        if (previous[change.id]?.width !== size.width || previous[change.id]?.height !== size.height) {
          if (next === previous) next = { ...previous }
          next[change.id] = size
        }
      }
      return next
    })
  }
  const focused = useRef<number>(-1)
  useLayoutEffect(() => {
    if (!focusRequest || focused.current === focusRequest.sequence || !instance) return
    const position = positions.get(focusRequest.id), size = sizes[focusRequest.id]
    if (!position || !size) return
    // Expanding a group changes React Flow's nodes in a later commit. Wait for
    // that real node and its measurement before consuming the focus request.
    let frame = 0
    const focus = () => {
      const target = Array.from(root.current?.querySelectorAll<HTMLElement>('.react-flow__node') ?? []).find(el => el.dataset.id === focusRequest.id)
      if (!target || getComputedStyle(target).visibility === 'hidden') { frame = requestAnimationFrame(focus); return }
      focused.current = focusRequest.sequence
      target.focus({ preventScroll: true })
      void instance.setCenter(position.x + size.width / 2, position.y + size.height / 2, { zoom: 1 })
    }
    frame = requestAnimationFrame(() => { frame = requestAnimationFrame(focus) })
    return () => cancelAnimationFrame(frame)
  }, [focusRequest, instance, positions, sizes])
  const keyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Enter' && event.key !== ' ') return
    if ((event.target as HTMLElement).closest('button, a, input')) return
    const edge = (event.target as HTMLElement).closest<HTMLElement>('.react-flow__edge')
    if (edge?.dataset.id && onEdgeInspect) { event.preventDefault(); event.stopPropagation(); onEdgeInspect(edge.dataset.id); return }
    const node = (event.target as HTMLElement).closest<HTMLElement>('.react-flow__node')
    const id = node?.dataset.id
    if (id) { event.preventDefault(); event.stopPropagation(); onSelect(id) }
  }
  return <div ref={root} className={styles.map} onKeyDownCapture={keyDown} aria-label={ariaLabel}>
    <ReactFlow nodes={flowNodes} edges={flowEdges} nodeTypes={nodeTypes} onNodesChange={onNodesChange} onInit={setInstance}
      proOptions={{ hideAttribution: true }}
      viewport={viewport} onViewportChange={onViewportChange}
      onNodeClick={(_event, node) => {
        const selection = root.current?.ownerDocument.getSelection()
        if (selection && !selection.isCollapsed && selection.toString()) return
        onSelect(node.id)
      }}
      onEdgeClick={(_event, edge) => onEdgeInspect?.(edge.id)}
      nodesDraggable={false} nodesConnectable={false} edgesReconnectable={false}
      edgesFocusable={Boolean(onEdgeInspect)} elementsSelectable={false} nodesFocusable
      deleteKeyCode={null} selectionKeyCode={null} panOnScroll zoomOnScroll={false}
      minZoom={0.5} maxZoom={2} preventScrolling={false}
      ariaLabelConfig={{ 'node.a11yDescription.default': 'Press Enter or Space to select. Use View details for supporting information or Expand rules to read a group.' }} />
    <div className={styles.controls} aria-label="Map zoom">
      <Tooltip content="Zoom out"><Button aria-label="Zoom out" onClick={() => onViewportChange({ ...viewport, zoom: Math.max(.5, viewport.zoom - .25) })}>−</Button></Tooltip>
      <Tooltip content="Reset map view"><Button aria-label="Reset map view" onClick={() => onViewportChange({ x: 0, y: 24, zoom: 1 })}>{Math.round(viewport.zoom * 100)}%</Button></Tooltip>
      <Tooltip content="Zoom in"><Button aria-label="Zoom in" onClick={() => onViewportChange({ ...viewport, zoom: Math.min(2, viewport.zoom + .25) })}>+</Button></Tooltip>
    </div>
  </div>
}
