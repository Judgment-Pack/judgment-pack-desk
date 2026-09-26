import { Message } from '../../i18n/Message'
import { msg, useLocale } from '../../i18n'
import { lazy, Suspense, useMemo, useState } from 'react'
import type { Viewport } from '@xyflow/react'
import { useMcp } from '../../mcp/McpProvider'
import { useGraphDocument } from '../../mcp/queries'
import { deriveWalkLayout, edgeCarries } from '../../mcp/graphDocument'
import { useDetailsPortal, useDetailsSlot } from '../../shell/DetailsSlot'
import { useInspectorWorkingWidth } from '../../shell/InspectorSlot'
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
  useLocale()
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
  const details = useDetailsSlot()
  const inspect = (selected: { node?: string; edge?: number }) => {
    setSelection({ ...selected, epoch: connectionEpoch, digest: served.data?.meta.sha256 })
    details.reveal()
  }
  const portal = useDetailsPortal(node || edge ? <section className={styles.details}>
    <h2>{node ? node.id : msg("Connection details")}</h2>
    {node?.pack && <ButtonLink to={`/packs/${encodeURIComponent(node.pack)}`}>{msg("Open pack")}</ButtonLink>}
    {edge && <dl>
      <div><dt>{msg("From")}</dt><dd>{edge.from}</dd></div>
      <div><dt>{msg("To")}</dt><dd>{edge.to}</dd></div>
      {edge.fact !== undefined && <div><dt>{msg("Fact destination")}</dt><dd><code>{edge.fact}</code></dd></div>}
      {edge.evidence && <>
        <div><dt>{msg("Evidence requirement")}</dt><dd>{edge.evidence.id}</dd></div>
        <div><dt>{msg("When unresolved")}</dt><dd>{edge.evidence.onUnresolved ?? msg("Not declared")}</dd></div>
      </>}
    </dl>}
    <details><summary>{msg("Source details")}</summary>
      <p>{served.data?.meta.path}</p>
      <CodeBlock text={JSON.stringify(node ? doc?.nodes[node.id] : doc?.edges[edge!.index], null, 2)} />
    </details>
  </section> : null)

  if (!graphDocumentSupported) return <Empty>{msg("This runtime cannot serve graph diagrams. The Tests tab can still run saved cases.")}</Empty>
  if (served.error) return <ErrorBox title={msg("Could not read this graph")} error={served.error} />
  if (served.isPending) return <Loading what={msg("the graph")} />
  if (!doc || !shape) return <p className="note">{served.data?.unreadable ?? (layout && !layout.drawn ? layout.reason : msg("No readable graph document was returned."))}</p>

  return <section className={styles.explorer} aria-label={msg("Graph diagram")}>
    {portal}
    <span ref={setRuler} className={styles.ruler} aria-hidden="true" />
    {doc.description && <ExpandableText text={doc.description} label={msg("graph description")} />}
    {shape.nodes.length === 0 ? <Empty>{msg("This graph declares no pack nodes.")}</Empty> : <div ref={setCanvas} className={styles.canvas}>
      <Suspense fallback={<Loading what={msg("the diagram")} />}>
        <RelationshipMap columnGap={8} nodeWidth={nodeWidth} ariaLabel={msg("Graph diagram")} nodes={shape.nodes.map(n => ({
          id: n.id, title: n.id, column: n.layer, selected: node?.id === n.id, action: msg("View details"),
          content: <div className={styles.nodeContent}><span>{n.pack}</span>{n.description && <p>{n.description}</p>}{n.isResult && <span className="quiet">{msg("Graph result")}</span>}</div>
        }))} edges={shape.edges.filter(e => e.drawable).map(e => ({
          id: String(e.index), source: e.from, target: e.to,
          label: e.fact && e.evidence ? msg('Fact + evidence') : e.fact ? msg('Fact') : msg('Evidence')
        }))} unit={unit} viewport={viewport} onViewportChange={setViewport}
          onSelect={id => inspect({ node: id })} onInspect={id => inspect({ node: id })}
          onEdgeInspect={id => inspect({ edge: Number(id) })} />
      </Suspense>
    </div>}
    {shape.resultDangling && <p className="note"><Message text={"The graph result names an undeclared node: <0/>."} slots={[shape.result]} /></p>}
    <section aria-label={msg("Graph connections")}><h2>{msg("Connections")}</h2>
      {shape.edges.length === 0 ? <p className="quiet">{msg("No connections are declared.")}</p>
        : shape.edges.map(e => <InspectionRow key={e.index} label={`${e.from} → ${e.to}`}
          description={<>{edgeCarries(e)}{!e.drawable && msg(" · An endpoint is not declared")}</>}
          current={edge?.index === e.index} onClick={() => inspect({ edge: e.index })} />)}
    </section>
    <p className="quiet">{msg("Declared connections · Experimental runtime graph format")}</p>
  </section>
}
