import { lazy, Suspense, useLayoutEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'
import type { TraceEntry } from '../mcp/types'
import type { Viewport } from '@xyflow/react'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { SegmentedControl } from '../ui/SegmentedControl'
import type { RelationshipEdge } from '../components/RelationshipMap'
import { useMeasuredBox } from '../shell/measured'
import { useInspectorWorkingWidth } from '../shell/InspectorSlot'
import { isRecord } from './document/MisshapenMember'
import { itemTrace, matchingItems, selectedItem, text, type LogicProjection } from './logicModel'
import type { LogicMode } from './logicState'
import styles from './PackLogic.module.css'

const RelationshipMap = lazy(() => import('../components/RelationshipMap').then(module => ({ default: module.RelationshipMap })))

const positions: Record<string, [number, number]> = {
  applicability: [0, 0], evidenceRequirements: [0, 9], rules: [0, 18],
  exceptions: [0, 27], resolution: [16, 13.5], outcomes: [32, 13.5], sources: [32, 27]
}
const relations: RelationshipEdge[] = [
  { id: 'scope', source: 'applicability', target: 'resolution' },
  { id: 'evidence', source: 'evidenceRequirements', target: 'resolution' },
  { id: 'candidates', source: 'rules', target: 'resolution' },
  { id: 'effects', source: 'exceptions', target: 'resolution' },
  { id: 'outcomes', source: 'resolution', target: 'outcomes' }
]

export function PackLogic({ model, at, select, mode, onMode, query, onQuery, openOutline,
  viewport, onViewport, listScroll, mapUnavailable, trace }: {
  model: LogicProjection; at: string | null; select: (pointer: string) => void
  mode: LogicMode; onMode: (mode: LogicMode) => void; query: string; onQuery: (query: string) => void
  openOutline: () => void; viewport: Viewport; onViewport: (v: Viewport) => void
  listScroll: MutableRefObject<number>; mapUnavailable?: string; trace?: readonly TraceEntry[]
}) {
  const [ruler, setRuler] = useState<HTMLSpanElement | null>(null)
  const rem = useMeasuredBox(ruler)?.width || 16
  useInspectorWorkingWidth(mode === 'map' ? 48 * rem : 34 * rem)
  const list = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => { if (list.current) list.current.scrollTop = listScroll.current }, [mode, listScroll])
  const current = selectedItem(model, at)
  const nodes = useMemo(() => model.groups.filter(g => g.id !== 'exceptions' || g.items.length).map(g => {
    const observed = g.items.map(item => itemTrace(g, item, trace)).filter((s): s is string => s !== undefined)
    const counts = new Map<string, number>(); observed.forEach(s => counts.set(s, (counts.get(s) ?? 0) + 1))
    return { id: g.id, title: g.label + (['applicability', 'resolution'].includes(g.id) ? '' : ` · ${g.items.length}`),
      description: g.description, x: positions[g.id]![0], y: positions[g.id]![1] * (trace ? 4 / 3 : 1), selected: current?.group.id === g.id || at === '/' + g.id,
      observation: counts.size ? [...counts].map(([label, count]) => `${count} ${label}`).join(' · ') : undefined }
  }), [model, current?.group.id, at, trace])
  const edges = useMemo(() => relations.filter(e => nodes.some(n => n.id === e.source)), [nodes])
  return <section className={styles.logic} aria-label="Pack logic">
    <span ref={setRuler} className={styles.ruler} aria-hidden="true" />
    <div className={styles.toolbar}>
      <SegmentedControl label="Logic view" value={mode} onValueChange={v => onMode(v as LogicMode)} segments={[{ value: 'map', label: 'Map' }, { value: 'list', label: 'List' }]} />
      <form className={styles.search} onSubmit={e => { e.preventDefault(); if (mode === 'map') openOutline() }}>
        <Input type="search" aria-label="Find pack item" value={query} onChange={e => onQuery(e.target.value)} placeholder={mode === 'map' ? 'Find items… (Enter)' : 'Filter items…'} />
      </form>
      {mode === 'map' && <Button onClick={openOutline}>Outline</Button>}
    </div>
    {mode === 'map' ? mapUnavailable ? <div className={styles.unavailable} role="status">
      <h2>Map unavailable</h2><p>{mapUnavailable}</p><Button onClick={() => onMode('list')}>Inspect List</Button>
    </div> : <>
      <div className={styles.canvas}><Suspense fallback={<p role="status">Loading map…</p>}><RelationshipMap nodes={nodes} edges={edges} unit={rem} viewport={viewport}
        onViewportChange={onViewport} onInspect={id => {
          const group = model.groups.find(g => g.id === id)!
          if (group.items.length === 1) select(group.items[0]!.pointer)
          else openOutline()
        }} /></Suspense></div>
      <p className={styles.caption}>{current ? `Selected in ${current.group.label}: ${current.item.label}. ` : ''}{trace ? 'Recorded observations. Select an item for its exact trace.' : 'Declared relationships. Select a group to explore.'}</p>
    </> : <div className={styles.list} ref={list} onScroll={e => { listScroll.current = e.currentTarget.scrollTop }}>
      {!query && <section className={styles.listGroup}><h2>Context</h2><div className={styles.compact}>
        <Button onClick={() => select('/applicability')}>Applicability</Button>
        <Button onClick={() => select('/evidenceRequirements')}>Evidence requirements</Button>
      </div></section>}
      {model.groups.filter(group => query || ['rules', 'exceptions'].includes(group.id)).map(group => {
        const rows = matchingItems(group, query)
        if (query && !rows.length) return null
        return <section className={styles.listGroup} key={group.id}>
          <h2>{group.label}{['rules', 'exceptions'].includes(group.id) ? ` · ${group.items.length}` : ''}</h2>
          {rows.length ? rows.map(item => <button type="button" key={item.pointer} data-logic-pointer={item.pointer}
            className={styles.row} aria-pressed={current?.item.pointer === item.pointer} onClick={() => select(item.pointer)}>
            <span className={styles.rowTitle}>{item.label}</span>
            <span className={styles.rowSummary}>{isRecord(item.value) && isRecord(item.value.when) ? <>{text(item.value.when.op)}{Array.isArray(item.value.when.conditions) ? ` · ${item.value.when.conditions.length} branches` : ''}<br />Inspect exact condition</> : item.value === undefined ? 'Not declared' : 'Inspect definition'}</span>
            <span>{item.effect}{itemTrace(group, item, trace) && <span className={styles.trace}>{itemTrace(group, item, trace)}</span>}</span>
          </button>) : <p className={styles.muted}>None declared.</p>}
        </section>
      })}
      {!query && <section className={styles.listGroup}><h2>Resolution and references</h2><div className={styles.compact}>
        <Button onClick={() => select('/fallbackOutcome')}>Fallback</Button><Button onClick={() => select('/escalation')}>Handoff</Button>
        <Button onClick={() => select('/outcomes')}>Outcomes</Button><Button onClick={() => select('/sources')}>Sources</Button>
      </div></section>}
      {query && !model.groups.some(g => matchingItems(g, query).length) && <p role="status">No items match “{query}”.</p>}
    </div>}
  </section>
}
