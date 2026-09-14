import { lazy, Suspense, useMemo, useState } from 'react'
import type { Viewport } from '@xyflow/react'
import { useMcp } from '../../mcp/McpProvider'
import { useGraphDocument } from '../../mcp/queries'
import { deriveWalkLayout, edgeCarries } from '../../mcp/graphDocument'
import { useInspectorPortal, useInspectorSlot, useInspectorWorkingWidth } from '../../shell/InspectorSlot'
import { useShellState } from '../../shell/paneState'
import { useMeasuredBox } from '../../shell/measured'
import { ButtonLink } from '../../ui/Button'
import { ExpandableText } from '../../ui/ExpandableText'
import { CodeBlock } from '../../ui/CodeBlock'
import { InspectionRow } from '../../ui/InspectionRow'
import { Empty, ErrorBox, Loading } from '../../components/primitives'
import styles from './FlowExplorer.module.css'

const RelationshipMap = lazy(() => import('../../components/RelationshipMap').then(module => ({ default: module.RelationshipMap })))

/** The diagram is projected only from the served graph document. No suite or
 * coverage payload is needed and no test verdict is inferred from structure. */
export function FlowExplorer({ graphId }: { graphId: string }) {
  const { graphDocumentSupported, connectionEpoch } = useMcp()
  const served = useGraphDocument(graphId)
  const doc = graphDocumentSupported && !served.error ? served.data?.document : undefined
  const layout = useMemo(() => doc && deriveWalkLayout(doc, undefined), [doc])
  const shape = layout?.drawn ? layout.shape : undefined
  const [selection, setSelection] = useState<{ epoch: number; digest?: string; node?: string; edge?: number }>()
  const current = selection?.epoch === connectionEpoch && selection?.digest === served.data?.meta.sha256 ? selection : undefined
  const node = shape?.nodes.find(node => node.id === current?.node)
  const edge = current?.edge === undefined ? undefined : shape?.edges[current.edge]
  const [viewport, setViewport] = useState<Viewport>({ x: 0, y: 24, zoom: 1 })
  const [ruler, setRuler] = useState<HTMLSpanElement | null>(null)
  const unit = useMeasuredBox(ruler)?.width || 16
  useInspectorWorkingWidth(54 * unit)
  const [canvas, setCanvas] = useState<HTMLDivElement | null>(null)
  const canvasWidth = useMeasuredBox(canvas)?.width
  const nodeWidth = Math.min(22 * unit, Math.max(12 * unit, (canvasWidth ?? 24 * unit) - 2 * unit))
  const inspector = useInspectorSlot()
  const shell = useShellState()
  const inspect = (selected: { node?: string; edge?: number }) => {
    setSelection({ ...selected, epoch: connectionEpoch, digest: served.data?.meta.sha256 })
    inspector.reveal()
  }
  const portal = useInspectorPortal(node || edge ? <section className={styles.details}>
    <h2>{node ? node.id : 'Connection details'}</h2>
    {node?.pack && <ButtonLink to={`/packs/${encodeURIComponent(node.pack)}`} onClick={() => {
      if (inspector.open && inspector.target?.closest('[role="dialog"]')) shell.toggleInspector()
    }}>Open pack</ButtonLink>}
    {edge && <dl>
      <div><dt>From</dt><dd>{edge.from}</dd></div>
      <div><dt>To</dt><dd>{edge.to}</dd></div>
      {edge.fact !== undefined && <div><dt>Fact destination</dt><dd><code>{edge.fact}</code></dd></div>}
      {edge.evidence && <>
        <div><dt>Evidence requirement</dt><dd>{edge.evidence.id}</dd></div>
        <div><dt>When unresolved</dt><dd>{edge.evidence.onUnresolved ?? 'Not declared'}</dd></div>
      </>}
    </dl>}
    <details><summary>Source details</summary>
      <p>{served.data?.meta.path}</p>
      <CodeBlock text={JSON.stringify(node ? doc?.nodes[node.id] : doc?.edges[edge!.index], null, 2)} />
    </details>
  </section> : null)

  if (!graphDocumentSupported) return <Empty>This runtime cannot serve flow diagrams. The Tests tab can still run saved cases.</Empty>
  if (served.error) return <ErrorBox title="Could not read this pack flow" error={served.error} />
  if (served.isPending) return <Loading what="the pack flow" />
  if (!doc || !shape) return <p className="note">{served.data?.unreadable ?? (layout && !layout.drawn ? layout.reason : 'No readable flow document was returned.')}</p>

  return <section className={styles.explorer} aria-label="Flow diagram">
    {portal}
    <span ref={setRuler} className={styles.ruler} aria-hidden="true" />
    {doc.description && <ExpandableText text={doc.description} label="flow description" />}
    {shape.nodes.length === 0 ? <Empty>This flow declares no pack nodes.</Empty> : <div ref={setCanvas} className={styles.canvas}>
      <Suspense fallback={<Loading what="the diagram" />}>
        <RelationshipMap columnGap={8} nodeWidth={nodeWidth} ariaLabel="Pack flow diagram" nodes={shape.nodes.map(n => ({
          id: n.id, title: n.id, column: n.layer, selected: node?.id === n.id, action: 'View details',
          content: <div className={styles.nodeContent}><span>{n.pack}</span>{n.description && <p>{n.description}</p>}{n.isResult && <span className="quiet">Flow result</span>}</div>
        }))} edges={shape.edges.filter(e => e.drawable).map(e => ({
          id: String(e.index), source: e.from, target: e.to,
          label: e.fact && e.evidence ? 'Fact + evidence' : e.fact ? 'Fact' : 'Evidence'
        }))} unit={unit} viewport={viewport} onViewportChange={setViewport}
          onSelect={id => inspect({ node: id })} onInspect={id => inspect({ node: id })}
          onEdgeInspect={id => inspect({ edge: Number(id) })} />
      </Suspense>
    </div>}
    {shape.resultDangling && <p className="note">The flow result names an undeclared node: {shape.result}.</p>}
    <section aria-label="Flow connections"><h2>Connections</h2>
      {shape.edges.length === 0 ? <p className="quiet">No connections are declared.</p>
        : shape.edges.map(e => <InspectionRow key={e.index} label={`${e.from} → ${e.to}`}
          description={<>{edgeCarries(e)}{!e.drawable && ' · An endpoint is not declared'}</>}
          current={edge?.index === e.index} onClick={() => inspect({ edge: e.index })} />)}
    </section>
    <p className="quiet">Declared connections · Experimental runtime graph format</p>
  </section>
}
