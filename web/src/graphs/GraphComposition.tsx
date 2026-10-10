import { lazy, Suspense, useMemo, useState } from 'react'
import type { Viewport } from '@xyflow/react'
import { msg } from '../i18n'
import { readGraphDocument, deriveWalkLayout } from '../mcp/graphDocument'
import type { ReadGraphDocument, GraphDocumentEdge } from '../mcp/types'
import { useDetailsPortal, useDetailsSlot } from '../shell/DetailsSlot'
import { Button, ButtonLink } from '../ui/Button'
import { Field, FieldGroup } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { InspectionRow } from '../ui/InspectionRow'
import { SegmentedControl } from '../ui/SegmentedControl'
import styles from './GraphWorkspace.module.css'

const RelationshipMap = lazy(() => import('../components/RelationshipMap').then(module => ({default: module.RelationshipMap})))
/** Editing changes the document; dragging cards changes only presentation. */
export function GraphComposition({content, onChange, packs, disabled = false}: {content: string; onChange: (text: string) => void; packs: string[]; disabled?: boolean}) {
  const [view, setView] = useState('diagram')
  const [pack, setPack] = useState('')
  const [selection, setSelection] = useState<{node?: string; edge?: number}>({})
  const [viewport, setViewport] = useState<Viewport>({x: 0, y: 24, zoom: 1})
  const [from, setFrom] = useState(''), [to, setTo] = useState(''), [fact, setFact] = useState(''), [evidence, setEvidence] = useState('')
  const [unresolved, setUnresolved] = useState('unknown')
  const parsed = useMemo(() => readGraphDocument(content), [content])
  // The empty authoring buffer can accept its first node; it is not a validated graph.
  const empty = useMemo(() => { try { const d = JSON.parse(content); return d && d.formatVersion === '1' && d.nodes && typeof d.nodes === 'object' && !Array.isArray(d.nodes) && !Object.keys(d.nodes).length && Array.isArray(d.edges) && !d.edges.length ? d as ReadGraphDocument : undefined } catch { return undefined } }, [content])
  const doc = parsed.ok ? parsed.document : empty
  const layout = useMemo(() => doc && Object.keys(doc.nodes).length ? deriveWalkLayout(doc, undefined) : undefined, [doc])
  const shape = layout?.drawn ? layout.shape : undefined
  const details = useDetailsSlot()
  const commit = (next: ReadGraphDocument) => onChange(JSON.stringify(next, null, 2) + '\n')
  const inspect = (next: typeof selection) => {setSelection(next); details.reveal()}
  const node = doc && selection.node ? doc.nodes[selection.node] : undefined
  const edge = doc && selection.edge !== undefined ? doc.edges[selection.edge] : undefined
  const portal = useDetailsPortal(doc && (node || edge) ? <section className={styles.inspector}>
    <h2>{node ? selection.node : msg('Connection details')}</h2>
    {node && selection.node && <FieldGroup>
      <Field label={msg('Pack')}>{w => <Select {...w} value={node.pack} disabled={disabled} options={[...new Set([...packs, node.pack])].map(value => ({value, label: value}))} onValueChange={value => commit({...doc, nodes: {...doc.nodes, [selection.node!]: {...node, pack: value}}})}/>}</Field>
      <ButtonLink to={`/packs/${encodeURIComponent(node.pack)}`}>{msg('Open pack')}</ButtonLink>
      <Field label={msg('Description')}>{w => <Input {...w} value={node.description ?? ''} disabled={disabled} onChange={e => commit({...doc, nodes: {...doc.nodes, [selection.node!]: {...node, description: e.target.value}}})}/>}</Field>
      <Button disabled={disabled || doc.result === selection.node} onClick={() => commit({...doc, result: selection.node})}>{doc.result === selection.node ? msg('Graph result') : msg('Set as result')}</Button>
      <Button variant="quiet" disabled={disabled} onClick={() => {const nodes = {...doc.nodes}; delete nodes[selection.node!]; commit({...doc, nodes, edges: doc.edges.filter(e => e.from !== selection.node && e.to !== selection.node), result: doc.result === selection.node ? '' : doc.result}); setSelection({})}}>{msg('Remove node')}</Button>
    </FieldGroup>}
    {edge && <FieldGroup>
      <p>{edge.from} → {edge.to}</p>
      <Field label={msg('Fact destination')} hint={msg('Receives the upstream outcome ID. No outcome leaves the fact missing.')}>{w => <Input {...w} value={edge.fact ?? ''} disabled={disabled} onChange={e => updateEdge({...edge, fact: e.target.value || undefined})}/>}</Field>
      <Field label={msg('Evidence requirement')} hint={msg('An upstream outcome contributes present; otherwise use the unresolved setting.')}>{w => <Input {...w} value={edge.evidence?.id ?? ''} disabled={disabled} onChange={e => updateEdge({...edge, evidence: e.target.value ? {id: e.target.value, onUnresolved: edge.evidence?.onUnresolved ?? 'unknown'} : undefined})}/>}</Field>
      {edge.evidence && <Field label={msg('When unresolved')}>{w => <Select {...w} value={edge.evidence?.onUnresolved ?? 'unknown'} disabled={disabled} options={[{value: 'unknown', label: msg('Unknown')}, {value: 'absent', label: msg('Absent')}]} onValueChange={value => updateEdge({...edge, evidence: {...edge.evidence!, onUnresolved: value}})}/>}</Field>}
      <Button variant="quiet" disabled={disabled} onClick={() => {commit({...doc, edges: doc.edges.filter((_, index) => index !== selection.edge)}); setSelection({})}}>{msg('Remove connection')}</Button>
    </FieldGroup>}
  </section> : null)
  function updateEdge(value: GraphDocumentEdge) {if (doc) commit({...doc, edges: doc.edges.map((edge, index) => index === selection.edge ? value : edge)})}
  const nodes = Object.keys(doc?.nodes ?? {})
  return <section className={styles.composition} aria-label={msg('Graph composition')}>
    {portal}
    <div className={styles.toolbar}>
      <SegmentedControl label={msg('Composition view')} value={view} onValueChange={setView} segments={[{value: 'diagram', label: msg('Diagram')}, {value: 'list', label: msg('List')}]}/>
      <Field label={msg('Add pack')}>{w => <Select {...w} value={pack} disabled={disabled || !doc} placeholder={msg('Choose a pack')} options={packs.map(value => ({value, label: value}))} onValueChange={setPack}/>}</Field>
      <Button disabled={disabled || !doc || !pack || nodes.length >= 64} onClick={() => {if (!doc) return; let id = pack, index = 2; while (Object.hasOwn(doc.nodes, id)) id = `${pack}-${index++}`; commit({...doc, nodes: {...doc.nodes, [id]: {pack}}, result: doc.result || id}); inspect({node: id})}}>{msg('Add')}</Button>
    </div>
    {!doc && <p role="status">{msg('Use Source to repair this document before editing its composition.')}</p>}
    {doc && <>
      <p className={styles.hint}>{msg('Every node runs. Connections pass outcomes or evidence availability; they do not skip steps.')}</p>
      {view === 'diagram' && shape ? <div className={styles.canvas}><Suspense fallback={<p role="status">{msg('Loading diagram…')}</p>}>
        <RelationshipMap unit={16} nodeWidth={260} columnGap={8} ariaLabel={msg('Graph composition')} viewport={viewport} onViewportChange={setViewport}
          nodes={shape.nodes.map(n => ({id: n.id, title: n.id, column: n.layer, selected: selection.node === n.id, action: msg('Edit node'), content: <><span>{n.pack === n.id ? msg('Pack') : n.pack}</span>{n.isResult && <p>{msg('Graph result')}</p>}</>}))}
          edges={shape.edges.filter(e => e.drawable).map(e => ({id: String(e.index), source: e.from, target: e.to, label: e.fact && e.evidence ? msg('Fact + evidence') : e.fact ? msg('Fact') : msg('Evidence')}))}
          onSelect={id => inspect({node: id})} onInspect={id => inspect({node: id})} onEdgeInspect={id => inspect({edge: Number(id)})}/>
      </Suspense></div> : <div>{nodes.length ? nodes.map(id => <InspectionRow key={id} label={id} description={`${doc.nodes[id]!.pack}${doc.result === id ? ' · ' + msg('Graph result') : ''}`} current={selection.node === id} onClick={() => inspect({node: id})}/>) : <p>{msg('Add a pack to start this composition.')}</p>}</div>}
      {layout && !layout.drawn && <p role="status">{layout.reason}</p>}
      <Field label={msg('Result node')}>{w => <Select {...w} value={doc.result ?? ''} disabled={disabled || !nodes.length} options={nodes.map(value => ({value, label: value}))} placeholder={msg('Choose the final decision')} onValueChange={value => commit({...doc, result: value})}/>}</Field>
      <h3>{msg('Connections')}</h3>
      {doc.edges.map((e, index) => <InspectionRow key={index} label={`${e.from} → ${e.to}`} description={[e.fact, e.evidence?.id].filter(Boolean).join(' · ')} current={selection.edge === index} onClick={() => inspect({edge: index})}/>)}
      <div className={styles.mapping}>
        <Field label={msg('From')}>{w => <Select {...w} disabled={disabled} value={from} options={nodes.map(value => ({value, label: value}))} onValueChange={setFrom}/>}</Field>
        <Field label={msg('To')}>{w => <Select {...w} disabled={disabled} value={to} options={nodes.filter(n => n !== from).map(value => ({value, label: value}))} onValueChange={setTo}/>}</Field>
        <Field label={msg('Fact destination')} hint={msg('JSON Pointer, for example /eligibility')}>{w => <Input {...w} value={fact} disabled={disabled} onChange={e => setFact(e.target.value)}/>}</Field>
        <Field label={msg('Evidence requirement')}>{w => <Input {...w} value={evidence} disabled={disabled} onChange={e => setEvidence(e.target.value)}/>}</Field>
        {evidence && <Field label={msg('When unresolved')}>{w => <Select {...w} value={unresolved} disabled={disabled} options={[{value: 'unknown', label: msg('Unknown')}, {value: 'absent', label: msg('Absent')}]} onValueChange={setUnresolved}/>}</Field>}
        <Button disabled={disabled || !nodes.includes(from) || !nodes.includes(to) || from === to || (!fact.trim() && !evidence.trim()) || (Boolean(fact.trim()) && !fact.trim().startsWith('/')) || doc.edges.length >= 256} onClick={() => {commit({...doc, edges: [...doc.edges, {from, to, ...(fact.trim() ? {fact: fact.trim()} : {}), ...(evidence.trim() ? {evidence: {id: evidence.trim(), onUnresolved: unresolved}} : {})}]}); setFact(''); setEvidence('')}}>{msg('Connect')}</Button>
      </div>
    </>}
  </section>
}
