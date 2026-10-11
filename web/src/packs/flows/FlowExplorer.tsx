import { Message } from '../../i18n/Message'
import { msg, useLocale } from '../../i18n'
import { lazy, Suspense, useMemo, useRef, useState } from 'react'
import type { Viewport } from '@xyflow/react'
import { useMcp } from '../../mcp/McpProvider'
import { useGraphDocument } from '../../mcp/queries'
import { deriveWalkLayout, edgeCarries } from '../../mcp/graphDocument'
import { BuilderSplit } from '../../builders/BuilderSplit'
import { SegmentedControl } from '../../ui/SegmentedControl'
import { Dialog, DialogActions } from '../../ui/Dialog'
import { GraphPlanView } from '../../graphs/GraphPlan'
import { Disclosure } from '../../ui/Disclosure'
import shared from '../../definitions/DefinitionWorkspace.module.css'
import { useInspectorWorkingWidth } from '../../shell/InspectorSlot'
import { useMeasuredBox } from '../../shell/measured'
import { Button, ButtonLink } from '../../ui/Button'
import { CodeBlock } from '../../ui/CodeBlock'
import { InspectionRow } from '../../ui/InspectionRow'
import { Empty, ErrorBox, Loading } from '../../components/primitives'
import { shownMessage, shownPath } from '../../graphs/client'
import styles from './FlowExplorer.module.css'

const RelationshipMap = lazy(() => import('../../components/RelationshipMap').then(module => ({ default: module.RelationshipMap })))

/** The diagram is projected only from the served graph document. No suite or
 * coverage payload is needed and no test verdict is inferred from structure. */
export function FlowExplorer({ graphId, initialPlan = false }: { graphId: string; initialPlan?: boolean }) {
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
  const [view, setView] = useState('diagram'), [detailsOpen, setDetailsOpen] = useState(false)
  const [planOpen, setPlanOpen] = useState(initialPlan), planTrigger = useRef<HTMLButtonElement>(null)
  const inspect = (selected: { node?: string; edge?: number }) => {
    setSelection({ ...selected, epoch: connectionEpoch, digest: served.data?.meta.sha256 })
    setDetailsOpen(true)
  }
  const detail = node || edge ? <section className={styles.details}>
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
    <Disclosure title={msg("Source details")}>
      <p>{served.data?.meta.path !== undefined && shownPath(served.data.meta.path)}</p>
      <CodeBlock expandable text={JSON.stringify(node ? doc?.nodes[node.id] : doc?.edges[edge!.index], null, 2)} />
    </Disclosure>
  </section> : null

  const planDialog = <Dialog open={planOpen} onOpenChange={setPlanOpen} title={msg('Runtime plan')} size="wide" openerRef={planTrigger}
      footer={<DialogActions><Button onClick={()=>setPlanOpen(false)}>{msg('Close')}</Button></DialogActions>}>
      {planOpen && <GraphPlanView graphId={graphId}/>}
    </Dialog>
  const unavailable = !graphDocumentSupported ? <Empty>{msg("This runtime cannot serve graph diagrams. The Tests tab can still run saved cases.")}</Empty>
    : served.error ? <ErrorBox title={msg("Could not read this graph")} error={served.error}/>
    : served.isPending ? <Loading what={msg("the graph")}/>
    : !doc || !shape ? <p className="note">{(served.data?.unreadable !== undefined ? shownMessage(served.data.unreadable, [served.data.meta.path]) : undefined) ?? (layout && !layout.drawn ? layout.reason : msg("No readable graph document was returned."))}</p> : null
  return <div className={styles.frame}>{unavailable || !shape ? <><div className={shared.toolbar}><Button ref={planTrigger} variant="quiet" onClick={()=>setPlanOpen(true)}>{msg('View runtime plan')}</Button></div>{unavailable}</> : <BuilderSplit preference="graph" title={node?.id ?? msg("Connection details")} editor={detail} open={detailsOpen} onClose={()=>setDetailsOpen(false)}><section className={styles.explorer} aria-label={msg("Graph diagram")}>
    <div className={shared.toolbar}><SegmentedControl label={msg('Logic view')} value={view} onValueChange={setView} segments={[{value:'diagram',label:msg('Diagram')},{value:'list',label:msg('List')}]}/>
      <Button ref={planTrigger} variant="quiet" onClick={()=>setPlanOpen(true)}>{msg('View runtime plan')}</Button></div>
    <span ref={setRuler} className={styles.ruler} aria-hidden="true" />
    {shape.nodes.length === 0 ? <Empty>{msg("This graph declares no pack nodes.")}</Empty> : view === 'diagram' ? <div ref={setCanvas} className={styles.canvas}>
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
    </div> : <div className={styles.list}>
      <section aria-label={msg('Graph steps')}>{shape.nodes.map(n=><InspectionRow key={n.id} label={n.id} description={n.pack} value={n.isResult?msg('Result'):undefined} current={node?.id===n.id} onClick={()=>inspect({node:n.id})}/>)}</section>
      <section aria-label={msg('Graph connections')}><h2>{msg('Connections')}</h2>
        {shape.edges.length === 0 ? <p className="quiet">{msg('No connections are declared.')}</p> : shape.edges.map(e=><InspectionRow key={e.index} label={`${e.from} → ${e.to}`} description={edgeCarries(e)} current={edge?.index===e.index} onClick={()=>inspect({edge:e.index})}/>)}</section>
    </div>}
    {shape.resultDangling && <p className="note"><Message text={"The graph result names an undeclared node: <0/>."} slots={[shape.result]} /></p>}
    <p className="quiet">{msg("Declared connections · Experimental runtime graph format")}</p>
  </section></BuilderSplit>}
    {planDialog}
  </div>
}
