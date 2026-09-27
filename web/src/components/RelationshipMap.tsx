import { msg, useLocale } from '../i18n'
import { Tooltip } from '../ui/Tooltip'
import { useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { IconRule, IconException, IconOutcome, IconFocus } from '../shell/icons'
import { ReactFlow, ViewportPortal, BaseEdge, EdgeLabelRenderer, getSmoothStepPath, type EdgeProps, type Edge, Handle, Position, MarkerType, type Node, type NodeProps, type Viewport, type NodeChange, type ReactFlowInstance } from '@xyflow/react'
import '@xyflow/react/dist/base.css'
import { Button } from '../ui/Button'
import styles from './RelationshipMap.module.css'

export interface RelationshipNode {
  id: string; title: string; column: number; content: ReactNode; action: string
  selected?: boolean; matched?: boolean; observation?: string
  kind?: 'rule' | 'exception' | 'outcome'; kindLabel?: string
}
export type NodePositions = Record<string, { x: number; y: number }>
export interface RelationshipEdge { id: string; source: string; target: string; label?: string; semantic?: 'contributes' | 'forces' | 'excludes' }
type ReadNode = Node<Omit<RelationshipNode, 'id' | 'column'> & { inspect: () => void; width?: number }, 'relationship'>
function ReadingNode({ data, selected }: NodeProps<ReadNode>) {
  useLocale()
  return <div className={[styles.node, selected ? styles.selected : '', data.matched ? styles.matched : ''].join(' ')} style={data.width === undefined ? undefined : { width: data.width }}>
    <Handle type="target" position={Position.Left} className={styles.handle} />
    {data.kind && <span className={styles.kind} data-kind={data.kind}>{data.kind === 'rule' ? <IconRule /> : data.kind === 'exception' ? <IconException /> : <IconOutcome />}{data.kindLabel}</span>}
    <strong className={`${styles.title} relationship-drag-handle`}>{data.title}</strong>
    {data.matched && <span className={styles.match}>{msg("Search match")}</span>}
    <div className={styles.content}>{data.content}</div>
    {data.observation && <span className={styles.observation}>{data.observation}</span>}
    {!data.kind && <Button variant="quiet" className={styles.action} aria-label={`${data.action}: ${data.title}`}
      onClick={event => { event.stopPropagation(); data.inspect() }}>{data.action} <span aria-hidden="true">›</span></Button>}
    <Handle type="source" position={Position.Right} className={styles.handle} />
  </div>
}
const nodeTypes = { relationship: ReadingNode }
type RoutedEdge = Edge<{lane?:number; gutterOffset?:number}>
function RelationshipLine(props:EdgeProps<RoutedEdge>) {
  const {sourceX,sourceY,targetX,targetY,data,label,style,markerEnd}=props
  const lane=data?.lane
  const [normal,labelX,labelY]=getSmoothStepPath(props)
  // A special case may bypass the rule column. Route it above the cards,
  // through the column gutters, so the connection never cuts through a rule.
  const offset=data?.gutterOffset??20
  const left=sourceX+offset,right=targetX-offset
  const path=lane===undefined?normal:`M ${sourceX},${sourceY} H ${left} V ${lane} H ${right} V ${targetY} H ${targetX}`
  return <><BaseEdge id={props.id} path={path} style={style} markerEnd={markerEnd}/>{label&&<EdgeLabelRenderer><span className={styles.edgeLabel} style={{transform:`translate(-50%, -50%) translate(${lane===undefined?labelX:(left+right)/2}px,${lane===undefined?labelY:lane}px)`}}>{label}</span></EdgeLabelRenderer>}</>
}
const edgeTypes={relationship:RelationshipLine}

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
export function RelationshipMap({ nodes, edges, unit, viewport, onViewportChange, onSelect, onInspect, focusRequest, ariaLabel = msg('Pack relationship map'), onEdgeInspect, columnGap = 5, nodeWidth, nodePositions, onNodePositionsChange }: {
  nodes: RelationshipNode[]; edges: RelationshipEdge[]; unit: number; viewport: Viewport
  onViewportChange: (next: Viewport) => void; onSelect: (id: string) => void; onInspect: (id: string) => void
  columnGap?: number; nodeWidth?: number
  nodePositions?: NodePositions; onNodePositionsChange?: (positions: NodePositions) => void
  ariaLabel?: string; onEdgeInspect?: (id: string) => void
  focusRequest?: { id: string; sequence: number }
}) {
  const locale = useLocale()
  const [sizes, setSizes] = useState<Record<string, { width: number; height: number }>>({})
  const [instance, setInstance] = useState<ReactFlowInstance<ReadNode> | null>(null)
  const root = useRef<HTMLDivElement>(null)
  const [localPositions, setLocalPositions] = useState<NodePositions>({})
  const moved = nodePositions ?? localPositions
  const movedRef = useRef(moved); movedRef.current = moved
  const moveNodes = (next: NodePositions) => { movedRef.current = next; setLocalPositions(next); onNodePositionsChange?.(next) }
  const positions = useMemo(() => {
    const result = relationshipPositions(nodes, sizes, unit, columnGap, nodeWidth)
    if(nodes.some(n=>n.kind))for(const [id,p] of result)result.set(id,{...p,y:p.y+2*unit})
    const bypass=edges.filter(e=>{const a=nodes.find(n=>n.id===e.source),b=nodes.find(n=>n.id===e.target);return e.semantic&&a&&b&&b.column-a.column>1})
    if(bypass.length)for(const [id,p] of result)result.set(id,{...p,y:p.y+(2+bypass.length*.8)*unit})
    for (const node of nodes) if (moved[node.id]) result.set(node.id, moved[node.id]!)
    return result
  }, [nodes, edges, sizes, unit, columnGap, nodeWidth, moved])
  const flowNodes = useMemo<ReadNode[]>(() => nodes.map(n => ({
    id: n.id, type: 'relationship', position: positions.get(n.id)!,
    // Controlled nodes must return their measurements on every render. Otherwise
    // React Flow hides them until ResizeObserver measures them again while panning.
    measured: sizes[n.id],
    data: { ...n, width: nodeWidth, inspect: () => onInspect(n.id) }, selected: n.selected, ariaLabel: n.kind ? `${n.kindLabel}: ${n.title}. ${n.action}` : msg('Select: {{title}}', { title: n.title }), ariaRole: n.kind ? 'button' : 'group',
    domAttributes: { 'aria-current': n.selected ? 'true' : undefined, 'data-search-match': n.matched ? 'true' : undefined },
    className: 'nopan', draggable: true, dragHandle: '.relationship-drag-handle', connectable: false
  })), [nodes, positions, sizes, onInspect, nodeWidth, locale])
  const flowEdges = useMemo(() => edges.map(e => {
    const related = nodes.some(n => n.selected && (n.id === e.source || n.id === e.target))
    const source = nodes.find(n => n.id === e.source), target = nodes.find(n => n.id === e.target)
    const bypass=Boolean(e.semantic&&source&&target&&target.column-source.column>1)
    const bypassEdges=edges.filter(candidate=>{const a=nodes.find(n=>n.id===candidate.source),b=nodes.find(n=>n.id===candidate.target);return candidate.semantic&&a&&b&&b.column-a.column>1})
    const laneIndex=bypassEdges.findIndex(candidate=>candidate.id===e.id)
    const lane=bypass?Math.min(...[...positions.values()].map(p=>p.y))-(1+laneIndex*.8)*unit:undefined
    // Separate vertical tracks too: shared tracks can falsely imply a branch.
    const gutterOffset=10+(laneIndex+1)/(bypassEdges.length+1)*Math.max(0,columnGap*unit-20)
    return { ...e, type: e.semantic?'relationship':'smoothstep', data:{lane,gutterOffset},
      label: e.semantic === 'contributes' && !related ? undefined : e.label,
      ariaLabel: `${e.label ?? msg('Connection')}: ${source?.title ?? e.source} → ${target?.title ?? e.target}`,
      zIndex: related ? 1 : 0,
      style: { stroke: related ? 'var(--ink)' : 'var(--ink-faint)', strokeWidth: related ? 2 : 1.5, strokeDasharray: e.semantic === 'excludes' ? '5 4' : undefined },
      markerEnd: { type: MarkerType.ArrowClosed, color: related ? 'var(--ink)' : 'var(--ink-faint)' },
      labelStyle: { fill: 'var(--ink-soft)', fontSize: .75 * unit },
      labelBgStyle: { fill: 'var(--bg)' },
    }
  }), [edges, nodes, positions, unit, columnGap, locale])
  const onNodesChange = (changes: NodeChange<ReadNode>[]) => {
    const movement = changes.filter(change => change.type === 'position' && change.position)
    if (movement.length) {
      const next = { ...movedRef.current }
      for (const change of movement) if (change.type === 'position' && change.position && Number.isFinite(change.position.x) && Number.isFinite(change.position.y)) next[change.id] = change.position
      moveNodes(next)
    }
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
    if (id) { event.preventDefault(); event.stopPropagation(); (nodes.find(n => n.id === id)?.kind ? onInspect : onSelect)(id) }
  }
  return <div ref={root} className={styles.map} onKeyDownCapture={keyDown} aria-label={ariaLabel}>
    <ReactFlow nodes={flowNodes} edges={flowEdges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} onNodesChange={onNodesChange} onInit={setInstance}
      proOptions={{ hideAttribution: true }}
      viewport={viewport} onViewportChange={onViewportChange}
      onNodeClick={(_event, node) => {
        const selection = root.current?.ownerDocument.getSelection()
        if (selection && !selection.isCollapsed && selection.toString()) return
        (nodes.find(n => n.id === node.id)?.kind ? onInspect : onSelect)(node.id)
      }}
      onEdgeClick={(_event, edge) => onEdgeInspect?.(edge.id)}
      nodesDraggable nodesConnectable={false} edgesReconnectable={false}
      edgesFocusable={Boolean(onEdgeInspect)} elementsSelectable={false} nodesFocusable
      deleteKeyCode={null} selectionKeyCode={null} panOnScroll zoomOnScroll={false}
      minZoom={0.5} maxZoom={2} preventScrolling
      ariaLabelConfig={{ 'node.a11yDescription.default': msg("Drag the title to move this node. Press Enter or Space to open details.") }} >
      <ViewportPortal>{nodes.filter((n, i) => n.kind && nodes.findIndex(other => other.column === n.column) === i).map(n => <span key={n.column} className={styles.columnLabel} style={{ left: positions.get(n.id)?.x, top: 0 }}>{n.kind === 'rule' ? msg('Rules') : n.kind === 'exception' ? msg('Special cases') : msg('Outcomes')}</span>)}</ViewportPortal>
    </ReactFlow>
    <div className={styles.controls} aria-label={msg("Map zoom")}>
      <Button onClick={() => { void instance?.fitView({ padding: .16, minZoom: .5, maxZoom: 1 }) }}>{msg('Fit')}</Button>
      {nodes.some(n => n.selected) && <Tooltip content={msg('Focus selected')}><Button aria-label={msg('Focus selected')} onClick={() => { void instance?.fitView({ nodes: nodes.filter(n => n.selected).map(n => ({ id: n.id })), padding: .3, minZoom: .5, maxZoom: 1 }) }}><IconFocus /></Button></Tooltip>}
      <Tooltip content={msg("Zoom out")}><Button aria-label={msg("Zoom out")} onClick={() => onViewportChange({ ...viewport, zoom: Math.max(.5, viewport.zoom - .25) })}>−</Button></Tooltip>
      <Tooltip content={msg("Reset map view")}><Button aria-label={msg("Reset map view")} onClick={() => onViewportChange({ x: 0, y: 24, zoom: 1 })}>{Math.round(viewport.zoom * 100)}%</Button></Tooltip>
      <Tooltip content={msg("Zoom in")}><Button aria-label={msg("Zoom in")} onClick={() => onViewportChange({ ...viewport, zoom: Math.min(2, viewport.zoom + .25) })}>+</Button></Tooltip>
      {Object.keys(moved).length > 0 && <Button onClick={() => moveNodes({})}>{msg('Reset layout')}</Button>}
    </div>
  </div>
}
