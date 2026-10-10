import { lazy, Suspense, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { Viewport } from '@xyflow/react'
import type { NodePositions } from '../components/RelationshipMap'
import { msg } from '../i18n'
import { readGraphDocument, deriveWalkLayout } from '../mcp/graphDocument'
import type { ReadGraphDocument, GraphDocumentEdge } from '../mcp/types'
import { BuilderSplit, useBuilderPreference } from '../builders/BuilderSplit'
import { Button, ButtonLink } from '../ui/Button'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { Dialog, DialogActions } from '../ui/Dialog'
import { InspectionRow } from '../ui/InspectionRow'
import { SegmentedControl } from '../ui/SegmentedControl'
import { Pill } from '../components/primitives'
import styles from './GraphWorkspace.module.css'

const RelationshipMap = lazy(() => import('../components/RelationshipMap').then(module => ({default: module.RelationshipMap})))
type Selection = {node?: string; edge?: number; connect?: boolean}
/** All edits use the graph buffer. Dragging and resizing change presentation only. */
export function GraphComposition({content, onChange, packs, disabled = false, tools, onSelection}: {
  content: string; onChange: (text: string) => void; packs: string[]; disabled?: boolean; tools?: ReactNode; onSelection?: (label: string) => void
}) {
  const [view, setView] = useBuilderPreference<string>('graph:view', typeof matchMedia === 'function' && matchMedia('(max-width: 700px)').matches ? 'list' : 'diagram')
  const [selection, setSelection] = useState<Selection>({})
  const [editorOpen, setEditorOpen] = useState(false)
  const addOpener = useRef<HTMLButtonElement | null>(null)
  const [adding, setAdding] = useState(false), [query, setQuery] = useState('')
  const [viewport, setViewport] = useState<Viewport>({x: 0, y: 24, zoom: 1})
  const [positions, setPositions] = useState<NodePositions>({})
  const [from, setFrom] = useState(''), [to, setTo] = useState(''), [fact, setFact] = useState(''), [evidence, setEvidence] = useState('')
  const [unresolved, setUnresolved] = useState('unknown')
  const parsed = useMemo(() => readGraphDocument(content), [content])
  const empty = useMemo(() => {try {const d = JSON.parse(content); return d && d.formatVersion === '1' && d.nodes && typeof d.nodes === 'object' && !Array.isArray(d.nodes) && !Object.keys(d.nodes).length && Array.isArray(d.edges) && !d.edges.length ? d as ReadGraphDocument : undefined} catch {return undefined}}, [content])
  const doc = parsed.ok ? parsed.document : empty
  const layout = useMemo(() => doc && Object.keys(doc.nodes).length ? deriveWalkLayout(doc, undefined) : undefined, [doc])
  const shape = layout?.drawn ? layout.shape : undefined
  const commit = (next: ReadGraphDocument) => onChange(JSON.stringify(next, null, 2) + '\n')
  const inspect = (next: Selection) => {setSelection(next); setEditorOpen(true)}
  const node = doc && selection.node ? doc.nodes[selection.node] : undefined
  const edge = doc && selection.edge !== undefined ? doc.edges[selection.edge] : undefined
  const title = node ? selection.node! : edge ? `${edge.from} → ${edge.to}` : msg('Connect packs')
  useEffect(() => onSelection?.(node || edge || selection.connect ? title : ''), [onSelection, title, !!node, !!edge, selection.connect])
  function updateEdge(value: GraphDocumentEdge) {if (doc) commit({...doc, edges: doc.edges.map((edge, index) => index === selection.edge ? value : edge)})}
  const nodes = Object.keys(doc?.nodes ?? {})
  const matchingPacks = packs.filter(id => id.toLocaleLowerCase().includes(query.toLocaleLowerCase()))
  const connect = () => {setFrom(selection.node ?? ''); setTo(''); inspect({connect: true})}
  const editor = doc && (node || edge || selection.connect) ? <div className={styles.selectionEditor}>
    {node && selection.node && <>
      <div className={styles.metadata}>
        <Field label={msg('Pack')}>{w => <Select {...w} value={node.pack} disabled={disabled} options={[...new Set([...packs, node.pack])].map(value => ({value, label: value}))} onValueChange={value => commit({...doc, nodes: {...doc.nodes, [selection.node!]: {...node, pack: value}}})}/>}</Field>
        <Field label={msg('Description')}>{w => <Input {...w} value={node.description ?? ''} disabled={disabled} onChange={e => commit({...doc, nodes: {...doc.nodes, [selection.node!]: {...node, description: e.target.value}}})}/>}</Field>
      </div>
      <div className={styles.toolbar}>
        <Button disabled={disabled || doc.result === selection.node} onClick={() => commit({...doc, result: selection.node})}>{doc.result === selection.node ? msg('Graph result') : msg('Set as result')}</Button>
        <Button disabled={disabled || nodes.length < 2} onClick={connect}>{msg('Connect packs')}</Button>
        <ButtonLink to={`/packs/${encodeURIComponent(node.pack)}`}>{msg('Open pack')}</ButtonLink>
        <Button variant="quiet" disabled={disabled} onClick={() => {const next = {...doc.nodes}; delete next[selection.node!]; commit({...doc, nodes: next, edges: doc.edges.filter(e => e.from !== selection.node && e.to !== selection.node), result: doc.result === selection.node ? '' : doc.result}); setSelection({})}}>{msg('Remove node')}</Button>
      </div>
      <p className={styles.hint}>{msg('This node references a shared pack. Editing that pack can affect other graphs.')}</p>
      <h3>{msg('Connections')}</h3>
      {doc.edges.map((e, index) => e.from === selection.node || e.to === selection.node ? <InspectionRow key={index} label={`${e.from} → ${e.to}`} description={[e.fact, e.evidence?.id].filter(Boolean).join(' · ')} onClick={() => inspect({edge: index})}/> : null)}
    </>}
    {edge && <>
      <div className={styles.mapping}>
        <Field label={msg('Fact destination')} hint={msg('Receives the upstream outcome ID. No outcome leaves the fact missing.')}>{w => <Input {...w} value={edge.fact ?? ''} disabled={disabled} onChange={e => updateEdge({...edge, fact: e.target.value || undefined})}/>}</Field>
        <Field label={msg('Evidence requirement')} hint={msg('An upstream outcome contributes present; otherwise use the unresolved setting.')}>{w => <Input {...w} value={edge.evidence?.id ?? ''} disabled={disabled} onChange={e => updateEdge({...edge, evidence: e.target.value ? {id: e.target.value, onUnresolved: edge.evidence?.onUnresolved ?? 'unknown'} : undefined})}/>}</Field>
        {edge.evidence && <Field label={msg('When unresolved')}>{w => <Select {...w} value={edge.evidence?.onUnresolved ?? 'unknown'} disabled={disabled} options={[{value: 'unknown', label: msg('Unknown')}, {value: 'absent', label: msg('Absent')}]} onValueChange={value => updateEdge({...edge, evidence: {...edge.evidence!, onUnresolved: value}})}/>}</Field>}
      </div>
      <Button variant="quiet" disabled={disabled} onClick={() => {commit({...doc, edges: doc.edges.filter((_, index) => index !== selection.edge)}); setSelection({})}}>{msg('Remove connection')}</Button>
    </>}
    {selection.connect && <>
      <div className={styles.mapping}>
        <Field label={msg('From')}>{w => <Select {...w} disabled={disabled} value={from} options={nodes.map(value => ({value, label: value}))} onValueChange={setFrom}/>}</Field>
        <Field label={msg('To')}>{w => <Select {...w} disabled={disabled} value={to} options={nodes.filter(n => n !== from).map(value => ({value, label: value}))} onValueChange={setTo}/>}</Field>
        <Field label={msg('Fact destination')} hint={msg('JSON Pointer, for example /eligibility')}>{w => <Input {...w} value={fact} disabled={disabled} onChange={e => setFact(e.target.value)}/>}</Field>
        <Field label={msg('Evidence requirement')}>{w => <Input {...w} value={evidence} disabled={disabled} onChange={e => setEvidence(e.target.value)}/>}</Field>
        {evidence && <Field label={msg('When unresolved')}>{w => <Select {...w} value={unresolved} disabled={disabled} options={[{value: 'unknown', label: msg('Unknown')}, {value: 'absent', label: msg('Absent')}]} onValueChange={setUnresolved}/>}</Field>}
      </div>
      <Button disabled={disabled || !nodes.includes(from) || !nodes.includes(to) || from === to || (!fact.trim() && !evidence.trim()) || (Boolean(fact.trim()) && !fact.trim().startsWith('/')) || doc.edges.length >= 256} onClick={() => {
        commit({...doc, edges: [...doc.edges, {from, to, ...(fact.trim() ? {fact: fact.trim()} : {}), ...(evidence.trim() ? {evidence: {id: evidence.trim(), onUnresolved: unresolved}} : {})}]})
        setFact(''); setEvidence(''); inspect({edge: doc.edges.length})
      }}>{msg('Connect')}</Button>
    </>}
  </div> : null
  return <BuilderSplit preference="graph" title={title} editor={editor} open={editorOpen} onClose={() => setEditorOpen(false)}>
    <section className={styles.composition} aria-label={msg('Graph composition')}>
      <div className={styles.buildToolbar}>
        <SegmentedControl label={msg('Composition view')} value={view === 'list' ? 'list' : 'diagram'} onValueChange={setView} segments={[{value: 'diagram', label: msg('Diagram')}, {value: 'list', label: msg('List')}]}/>
        {tools}
        <Button variant="quiet" disabled={!editor} aria-pressed={editorOpen} onClick={() => setEditorOpen(!editorOpen)}>{msg('Editor')}</Button>
        <Button disabled={disabled || !doc || nodes.length < 2} onClick={connect}>{msg('Connect packs')}</Button>
        <Button disabled={disabled || !doc || !packs.length || nodes.length >= 64} onClick={event => {addOpener.current = event.currentTarget; setAdding(true)}}>{msg('Add pack')}</Button>
      </div>
      <div className={styles.compositionBody}>
        {!doc ? <p role="status">{msg('Use Source to repair this document before editing its composition.')}</p> : !nodes.length ? <div className={styles.empty}>
          <h2>{msg('Compose a decision from packs')}</h2><p>{msg('Add a pack, connect its result to another pack, and choose the final decision.')}</p>
          <Button disabled={disabled || !packs.length} onClick={event => {addOpener.current = event.currentTarget; setAdding(true)}}>{msg('Add first pack')}</Button>
          {!packs.length && <p>{msg('Create a pack before composing a graph.')}</p>}
        </div> : <>
          {view !== 'list' && shape ? <div className={styles.canvas}><Suspense fallback={<p role="status">{msg('Loading diagram…')}</p>}>
            <RelationshipMap unit={16} nodeWidth={240} columnGap={5} ariaLabel={msg('Graph composition')} viewport={viewport} onViewportChange={setViewport} nodePositions={positions} onNodePositionsChange={setPositions}
              nodes={shape.nodes.map(n => ({id: n.id, title: n.id, column: n.layer, selected: selection.node === n.id, action: msg('Edit node'), content: <><span>{n.pack === n.id ? msg('Pack') : n.pack}</span>{n.isResult && <Pill tone="quiet">{msg('Result')}</Pill>}</>}))}
              edges={shape.edges.filter(e => e.drawable).map(e => ({id: String(e.index), source: e.from, target: e.to, label: e.fact && e.evidence ? msg('Fact + evidence') : e.fact ? msg('Fact') : msg('Evidence')}))}
              onSelect={id => inspect({node: id})} onInspect={id => inspect({node: id})} onEdgeInspect={id => inspect({edge: Number(id)})}/>
          </Suspense></div> : <div className={styles.list}>
            {nodes.map(id => <InspectionRow key={id} label={id} description={doc.nodes[id]!.pack} value={doc.result === id ? msg('Result') : undefined} current={selection.node === id} onClick={() => inspect({node: id})}/>)}
            <h3>{msg('Connections')}</h3>
            {doc.edges.map((e, index) => <InspectionRow key={index} label={`${e.from} → ${e.to}`} description={[e.fact, e.evidence?.id].filter(Boolean).join(' · ')} current={selection.edge === index} onClick={() => inspect({edge: index})}/>)}
            {layout && !layout.drawn && <p role="status">{layout.reason}</p>}
          </div>}
        </>}
      </div>
      <p className={styles.compositionHint}>{msg('Every node runs. Connections pass outcomes or evidence availability; they do not skip steps.')}</p>
    </section>
    <Dialog openerRef={addOpener} open={adding} onOpenChange={setAdding} title={msg('Add pack')} description={msg('Choose a declared pack to reuse in this graph.')}>
      <Field label={msg('Search packs')}>{w => <Input {...w} value={query} onChange={e => setQuery(e.target.value)}/>}</Field>
      <div className={styles.picker}>{matchingPacks.map(pack => <InspectionRow key={pack} label={pack} disabled={disabled} onClick={() => {
        if (!doc || nodes.length >= 64) return
        let id = pack, index = 2; while (Object.hasOwn(doc.nodes, id)) id = `${pack}-${index++}`
        commit({...doc, nodes: {...doc.nodes, [id]: {pack}}, result: doc.result || id}); inspect({node: id}); setAdding(false); setQuery('')
      }}/>) }{!matchingPacks.length && <p role="status" className={styles.hint}>{msg('No results found.')}</p>}</div>
      <DialogActions><Button onClick={() => setAdding(false)}>{msg('Cancel')}</Button></DialogActions>
    </Dialog>
  </BuilderSplit>
}
