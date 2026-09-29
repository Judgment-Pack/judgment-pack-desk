import { relationshipPaths } from './relationshipPaths'
import { DecisionSymbol } from './DecisionSymbol'
import type { DecisionMeaning } from '../packs/decisionAppearance'
import { msg, useLocale } from '../i18n'
import { Tooltip } from '../ui/Tooltip'
import { memo, useCallback, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { IconRule, IconException, IconFocus, IconAutoArrange } from '../shell/icons'
import { ReactFlow, applyNodeChanges, ViewportPortal, BaseEdge, EdgeLabelRenderer, getSmoothStepPath, type EdgeProps, type Edge, Handle, Position, MarkerType, type Node, type NodeProps, type Viewport, type NodeChange, type ReactFlowInstance } from '@xyflow/react'
import '@xyflow/react/dist/base.css'
import { Button } from '../ui/Button'
import styles from './RelationshipMap.module.css'

export interface RelationshipNode {
  id: string; title: string; column: number; content: ReactNode; action: string
  selected?: boolean; matched?: boolean; observation?: string
  kind?: 'rule' | 'exception' | 'outcome' | 'handoff'; kindLabel?: string
  accent?: string; meaning?: DecisionMeaning; meaningLabel?: string; related?: boolean; muted?: boolean
}
export type NodePositions = Record<string, { x: number; y: number }>
export interface RelationshipEdge { id: string; source: string; target: string; label?: string; semantic?: 'contributes' | 'forces' | 'excludes' | 'requests-handoff' }
type ReadNode = Node<Omit<RelationshipNode, 'id' | 'column'> & { inspect: () => void; width?: number }, 'relationship'>
const ReadingNode = memo(function ReadingNode({ data, selected }: NodeProps<ReadNode>) {
  useLocale()
  return <div className={[styles.node, 'relationship-drag-handle', selected ? styles.selected : '', data.matched ? styles.matched : ''].join(' ')} data-related={data.related || undefined} data-muted={data.muted || undefined} data-accent={Boolean(data.accent) || undefined} style={{ width: data.width, '--node-accent': data.accent } as import('react').CSSProperties}>
    <Handle type="target" position={Position.Left} className={styles.handle} />
    {data.kind && <span className={styles.kind} data-kind={data.kind}>{data.kind === 'rule' ? <IconRule /> : data.kind === 'exception' ? <IconException /> : <DecisionSymbol meaning={data.meaning} />}{data.kindLabel}{data.meaningLabel && <> · {data.meaningLabel}</>}</span>}
    <strong className={styles.title}>{data.title}</strong>
    {data.matched && <span className={styles.match}>{msg("Search match")}</span>}
    <div className={styles.content}>{data.content}</div>
    {data.observation && <span className={styles.observation}>{data.observation}</span>}
    {!data.kind && <Button variant="quiet" className={`${styles.action} nodrag`} aria-label={`${data.action}: ${data.title}`}
      onClick={event => { event.stopPropagation(); data.inspect() }}>{data.action} <span aria-hidden="true">›</span></Button>}
    <Handle type="source" position={Position.Right} className={styles.handle} />
  </div>
}, (previous, next) => previous.data === next.data && previous.selected === next.selected)
const nodeTypes = { relationship: ReadingNode }
type RoutedEdge = Edge<{lane?:number; gutterOffset?:number}>
const RelationshipLine = memo(function RelationshipLine(props:EdgeProps<RoutedEdge>) {
  const {sourceX,sourceY,targetX,targetY,data,label,style,markerEnd}=props
  const lane=data?.lane === undefined ? undefined : Math.min(data.lane, sourceY - 20, targetY - 20)
  const [normal,labelX,labelY]=getSmoothStepPath(props)
  // A special case may bypass the rule column. Route it above the cards,
  // through the column gutters, so the connection never cuts through a rule.
  const offset=data?.gutterOffset??20
  const left=sourceX+offset,right=targetX-offset
  const path=lane===undefined?normal:`M ${sourceX},${sourceY} H ${left} V ${lane} H ${right} V ${targetY} H ${targetX}`
  return <><BaseEdge id={props.id} path={path} style={style} markerEnd={markerEnd}/>{label&&<EdgeLabelRenderer><span className={styles.edgeLabel} style={{transform:`translate(-50%, -50%) translate(${lane===undefined?labelX:(left+right)/2}px,${lane===undefined?labelY:lane}px)`}}>{label}</span></EdgeLabelRenderer>}</>
})
const edgeTypes={relationship:RelationshipLine}
const proOptions = { hideAttribution: true }

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
export function RelationshipMap({ nodes, edges, unit, viewport, onViewportChange, onSelect, onInspect, focusRequest, ariaLabel = msg('Pack relationship map'), onEdgeInspect, onClearSelection, columnGap = 5, nodeWidth, nodePositions, onNodePositionsChange }: {
  nodes: RelationshipNode[]; edges: RelationshipEdge[]; unit: number; viewport: Viewport
  onViewportChange: (next: Viewport) => void; onSelect: (id: string) => void; onInspect: (id: string) => void
  columnGap?: number; nodeWidth?: number
  nodePositions?: NodePositions; onNodePositionsChange?: (positions: NodePositions) => void
  ariaLabel?: string; onEdgeInspect?: (id: string) => void; onClearSelection?: () => void
  focusRequest?: { id: string; sequence: number }
}) {
  const locale = useLocale()
  const [sizes, setSizes] = useState<Record<string, { width: number; height: number }>>({})
  const [instance, setInstance] = useState<ReactFlowInstance<ReadNode> | null>(null)
  const root = useRef<HTMLDivElement>(null)
  const handlers = useRef({ onInspect, onSelect, onEdgeInspect, onClearSelection, onNodePositionsChange, onViewportChange })
  handlers.current = { onInspect, onSelect, onEdgeInspect, onClearSelection, onNodePositionsChange, onViewportChange }
  const movedRef = useRef<NodePositions>(nodePositions ?? {})
  useLayoutEffect(() => { if (nodePositions) movedRef.current = nodePositions }, [nodePositions])
  const [liveViewport, setLiveViewport] = useState(viewport)
  useLayoutEffect(() => { setLiveViewport(current => current.x === viewport.x && current.y === viewport.y && current.zoom === viewport.zoom ? current : viewport) }, [viewport])
  const commitViewport = useCallback((_event: unknown, next: Viewport) => handlers.current.onViewportChange(next), [])
  const changeViewport = useCallback((next: Viewport) => { setLiveViewport(next); handlers.current.onViewportChange(next) }, [])
  const inspect = useCallback((id: string) => handlers.current.onInspect(id), [])
  const nodeLookup = useMemo(() => new Map(nodes.map(node => [node.id, node])), [nodes])
  // Index the topology once. Moving a node must not rescan every edge for every edge.
  const bypassLanes = useMemo(() => {
    const lanes = new Map<string, number>()
    for (const edge of edges) {
      const source = nodeLookup.get(edge.source), target = nodeLookup.get(edge.target)
      if (edge.semantic && source && target && target.column - source.column > 1) lanes.set(edge.id, lanes.size)
    }
    return lanes
  }, [edges, nodeLookup])
  const layoutTop = useMemo(() => (nodes.some(node => node.kind) ? 2 : 0) + (bypassLanes.size ? 2 + bypassLanes.size * 1.5 : 0), [nodes, bypassLanes])
  const positions = useMemo(() => {
    const result = relationshipPositions(nodes, sizes, unit, columnGap, nodeWidth)
    if (layoutTop) for (const [id, position] of result) result.set(id, { ...position, y: position.y + layoutTop * unit })
    return result
  }, [nodes, sizes, unit, columnGap, nodeWidth, layoutTop])
  const [arrangeRequest, setArrangeRequest] = useState(0)
  const arranged = useRef(0)
  useLayoutEffect(() => {
    if (!instance || !arrangeRequest || arranged.current === arrangeRequest) return
    // Fit after React Flow has committed the restored positions and measurements.
    let frame = requestAnimationFrame(() => { frame = requestAnimationFrame(() => {
      arranged.current = arrangeRequest
      void instance.fitView({ padding: .16, minZoom: .5, maxZoom: 1 })
    }) })
    return () => cancelAnimationFrame(frame)
  }, [arrangeRequest, instance, positions, sizes])
  const autoArrange = () => {
    movedRef.current = {}
    setFlowNodes(previous => previous.map(node => ({ ...node, position: positions.get(node.id)!, dragging: false })))
    handlers.current.onNodePositionsChange?.({})
    setArrangeRequest(value => value + 1)
  }
  const paths = useMemo(() => relationshipPaths(nodes, edges), [nodes, edges])
  const plannedNodes = useMemo<ReadNode[]>(() => nodes.map(n => ({
    id: n.id, type: 'relationship', position: positions.get(n.id)!,
    // Controlled nodes must return their measurements on every render. Otherwise
    // React Flow hides them until ResizeObserver measures them again while panning.
    measured: sizes[n.id],
    data: { ...n, related: paths.active && paths.nodeIds.has(n.id), muted: paths.active && !paths.nodeIds.has(n.id), width: nodeWidth, inspect: () => inspect(n.id) }, selected: n.selected, ariaLabel: n.kind ? `${n.kindLabel}${n.meaningLabel ? ` · ${n.meaningLabel}` : ''}: ${n.title}. ${n.action}` : msg('Select: {{title}}', { title: n.title }), ariaRole: n.kind ? 'button' : 'group',
    domAttributes: { 'aria-current': n.selected ? 'true' : undefined, 'data-search-match': n.matched ? 'true' : undefined },
    className: 'nopan', draggable: true, dragHandle: '.relationship-drag-handle', connectable: false
  })), [nodes, positions, sizes, inspect, nodeWidth, locale, paths])
  const [flowNodes, setFlowNodes] = useState<ReadNode[]>(plannedNodes)
  useLayoutEffect(() => {
    setFlowNodes(previous => {
      const before = new Map(previous.map(node => [node.id, node]))
      return plannedNodes.map(node => {
        const old = before.get(node.id), position = movedRef.current[node.id] ?? node.position
        if (old && old.data === node.data && old.selected === node.selected && old.position.x === position.x && old.position.y === position.y && old.measured?.width === node.measured?.width && old.measured?.height === node.measured?.height) return old
        return { ...node, position, dragging: old?.dragging }
      })
    })
  }, [plannedNodes, nodePositions])
  const flowEdges = useMemo(() => edges.map(e => {
    const related = paths.edgeIds.has(e.id)
    const source = nodeLookup.get(e.source), target = nodeLookup.get(e.target)
    const laneIndex = bypassLanes.get(e.id)
    // Stable routing lanes during a gesture. Connected edges follow their endpoints
    // inside React Flow; unrelated paths do not jump when one node moves upward.
    const lane = laneIndex === undefined ? undefined : (layoutTop - 1 - laneIndex * 1.5) * unit
    const gutterOffset = 10 + ((laneIndex ?? -1) + 1) / (bypassLanes.size + 1) * Math.max(0, columnGap * unit - 20)
    return { ...e, type: e.semantic?'relationship':'smoothstep', data:{lane,gutterOffset},
      label: e.semantic === 'contributes' ? undefined : e.label,
      ariaLabel: `${e.label ?? msg('Connection')}: ${source?.title ?? e.source} → ${target?.title ?? e.target}`,
      zIndex: related ? 1 : 0,
      style: { stroke: related ? 'var(--ink)' : 'var(--ink-faint)', strokeWidth: related ? 2.5 : 1.5, opacity: paths.active && !related ? .3 : 1, strokeDasharray: e.semantic === 'excludes' ? '5 4' : undefined },
      markerEnd: { type: MarkerType.ArrowClosed, color: related ? 'var(--ink)' : 'var(--ink-faint)' },
      labelStyle: { fill: 'var(--ink-soft)', fontSize: .75 * unit },
      labelBgStyle: { fill: 'var(--bg)' },
    }
  }), [edges, nodeLookup, bypassLanes, layoutTop, unit, columnGap, locale, paths])
  const onNodesChange = useCallback((changes: NodeChange<ReadNode>[]) => {
    const safe = changes.filter(change => change.type === 'dimensions' || change.type === 'position' && (!change.position || Number.isFinite(change.position.x) && Number.isFinite(change.position.y)))
    if (!safe.length) return
    // applyNodeChanges preserves every unaffected node and its data by identity.
    // Keep the hot path in the canvas; publish positions only at gesture end.
    setFlowNodes(previous => applyNodeChanges(safe, previous))
    const movement = safe.filter(change => change.type === 'position' && change.position)
    if (movement.length) {
      const next = { ...movedRef.current }
      for (const change of movement) if (change.type === 'position' && change.position) next[change.id] = change.position
      movedRef.current = next
      if (movement.some(change => change.type === 'position' && change.dragging !== true)) handlers.current.onNodePositionsChange?.(next)
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
  }, [])
  const focused = useRef<number>(-1)
  useLayoutEffect(() => {
    if (!focusRequest || focused.current === focusRequest.sequence || !instance) return
    const position = movedRef.current[focusRequest.id] ?? positions.get(focusRequest.id), size = sizes[focusRequest.id]
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
    if (event.key === 'Escape' && onClearSelection && paths.active) { event.preventDefault(); event.stopPropagation(); onClearSelection(); return }
    if (event.key !== 'Enter' && event.key !== ' ') return
    if ((event.target as HTMLElement).closest('button, a, input')) return
    const edge = (event.target as HTMLElement).closest<HTMLElement>('.react-flow__edge')
    if (edge?.dataset.id && onEdgeInspect) { event.preventDefault(); event.stopPropagation(); onEdgeInspect(edge.dataset.id); return }
    const node = (event.target as HTMLElement).closest<HTMLElement>('.react-flow__node')
    const id = node?.dataset.id
    if (id) { event.preventDefault(); event.stopPropagation(); (nodes.find(n => n.id === id)?.kind ? onInspect : onSelect)(id) }
  }
  const onNodeClick = useCallback((_event: unknown, node: ReadNode) => {
    const selection = root.current?.ownerDocument.getSelection()
    if (selection && !selection.isCollapsed && selection.toString()) return
    const action = node.data.kind ? handlers.current.onInspect : handlers.current.onSelect
    action(node.id)
  }, [])
  const onPaneClick = useCallback(() => handlers.current.onClearSelection?.(), [])
  const onEdgeClick = useCallback((_event: unknown, edge: Edge) => handlers.current.onEdgeInspect?.(edge.id), [])
  const ariaLabelConfig = useMemo(() => ({ 'node.a11yDescription.default': msg('Drag the card to move it. Press Enter or Space to open details.') }), [locale])
  const columns = useMemo(() => {
    const first = new Map<number, RelationshipNode>(), handoff = new Set<number>()
    for (const node of nodes) {
      if (node.kind && !first.has(node.column)) first.set(node.column, node)
      if (node.kind === 'handoff') handoff.add(node.column)
    }
    return [...first.values()].map(node => <span key={node.column} className={styles.columnLabel} style={{ left: positions.get(node.id)?.x, top: 0 }}>{node.kind === 'rule' ? msg('Rules') : node.kind === 'exception' ? msg('Special cases') : node.kind === 'handoff' ? msg('Handoff') : handoff.has(node.column) ? msg('Outcomes and handoff') : msg('Outcomes')}</span>)
  }, [nodes, positions, locale])
  return <div ref={root} className={styles.map} onKeyDownCapture={keyDown} aria-label={ariaLabel}>
    <ReactFlow nodes={flowNodes} edges={flowEdges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} onNodesChange={onNodesChange} onInit={setInstance}
      proOptions={proOptions}
      viewport={liveViewport} onViewportChange={setLiveViewport} onMoveEnd={commitViewport}
      onNodeClick={onNodeClick} onPaneClick={onPaneClick} onEdgeClick={onEdgeClick}
      nodeDragThreshold={4} nodeClickDistance={4}
      nodesDraggable nodesConnectable={false} edgesReconnectable={false}
      edgesFocusable={Boolean(onEdgeInspect)} elementsSelectable={false} nodesFocusable
      deleteKeyCode={null} selectionKeyCode={null} panOnScroll zoomOnScroll={false}
      minZoom={0.5} maxZoom={2} preventScrolling
      ariaLabelConfig={ariaLabelConfig}>
      <ViewportPortal>{columns}</ViewportPortal>
    </ReactFlow>
    <div className={styles.controls} aria-label={msg("Map zoom")}>
      <Button onClick={() => { void instance?.fitView({ padding: .16, minZoom: .5, maxZoom: 1 }) }}>{msg('Fit')}</Button>
      {nodes.some(n => n.selected) && <Tooltip content={msg('Fit highlighted path')}><Button aria-label={msg('Fit highlighted path')} onClick={() => { void instance?.fitView({ nodes: nodes.filter(n => paths.nodeIds.has(n.id)).map(n => ({ id: n.id })), padding: .3, minZoom: .5, maxZoom: 1 }) }}><IconFocus /></Button></Tooltip>}
      <Tooltip content={msg("Zoom out")}><Button aria-label={msg("Zoom out")} onClick={() => changeViewport({ ...liveViewport, zoom: Math.max(.5, liveViewport.zoom - .25) })}>−</Button></Tooltip>
      <Tooltip content={msg("Reset map view")}><Button aria-label={msg("Reset map view")} onClick={() => changeViewport({ x: 0, y: 24, zoom: 1 })}>{Math.round(liveViewport.zoom * 100)}%</Button></Tooltip>
      <Tooltip content={msg("Zoom in")}><Button aria-label={msg("Zoom in")} onClick={() => changeViewport({ ...liveViewport, zoom: Math.min(2, liveViewport.zoom + .25) })}>+</Button></Tooltip>
      <Tooltip content={msg('Auto arrange')}><Button aria-label={msg('Auto arrange')} disabled={!instance || nodes.length === 0} onClick={autoArrange}><IconAutoArrange /></Button></Tooltip>
    </div>
  </div>
}
