import { lazy, Suspense, useLayoutEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'
import type { Viewport } from '@xyflow/react'
import type { TraceEntry } from '../mcp/types'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { InspectionRow } from '../ui/InspectionRow'
import { Popover } from '../ui/Popover'
import { SegmentedControl } from '../ui/SegmentedControl'
import { useMeasuredBox } from '../shell/measured'
import { useInspectorWorkingWidth } from '../shell/InspectorSlot'
import { evidenceSummary, itemTrace, matchingItems, selectedItem, type LogicGroup, type LogicItem, type LogicProjection } from './logicModel'
import { projectLogicGraph } from './logicGraph'
import { LogicDetails } from './LogicDetails'
import { initialLogicDisplay, rememberLogicDisplay, type LogicMode } from './logicState'
import styles from './PackLogic.module.css'

const RelationshipMap = lazy(() => import('../components/RelationshipMap').then(module => ({ default: module.RelationshipMap })))

export function PackLogic({ model, at, groupId, select, mode, onMode, query, onQuery, openOutline,
  viewport, onViewport, listScroll, mapUnavailable, trace }: {
  model: LogicProjection; at: string | null; groupId: string | null; select: (pointer: string) => void
  mode: LogicMode; onMode: (mode: LogicMode) => void; query: string; onQuery: (query: string) => void
  openOutline: () => void; viewport: Viewport; onViewport: (v: Viewport) => void
  listScroll: MutableRefObject<number>; mapUnavailable?: string; trace?: readonly TraceEntry[]
}) {
  const [ruler, setRuler] = useState<HTMLSpanElement | null>(null)
  const rem = useMeasuredBox(ruler)?.width || 16
  useInspectorWorkingWidth(mode === 'map' ? 51 * rem : 34 * rem)
  const root = useRef<HTMLElement>(null)
  const [display, setDisplay] = useState(initialLogicDisplay)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [focusRequest, setFocusRequest] = useState<{ id: string; sequence: number }>()
  const nextMatch = useRef(0)
  useLayoutEffect(() => { setExpanded(new Set()); nextMatch.current = 0 }, [model])
  useLayoutEffect(() => { nextMatch.current = 0 }, [query])
  // PageBody owns List scrolling; a second scroll box would trap the toolbar.
  useLayoutEffect(() => {
    const body = root.current?.closest<HTMLElement>('[data-page-scroll]')
    if (!body) return
    body.scrollTop = mode === 'list' ? listScroll.current : 0
    if (mode !== 'list') return
    const remember = () => { listScroll.current = body.scrollTop }
    body.addEventListener('scroll', remember)
    return () => body.removeEventListener('scroll', remember)
  }, [mode, listScroll])
  const current = selectedItem(model, at)
  const graph = useMemo(() => projectLogicGraph(model, display.grouped && !query.trim(), expanded), [model, display.grouped, expanded, query])
  const matchPointers = useMemo(() => new Set(model.groups.flatMap(g => matchingItems(g, query).map(i => i.pointer))), [model, query])
  const searching = Boolean(query.trim())
  const graphNodes = graph.nodes.map(node => {
    const aggregate = node.items.length > 1
    const item = node.items[0]!
    const observations = new Map<string, number>()
    node.items.forEach(child => {
      const observation = itemTrace(node.group, child, trace)
      if (observation) observations.set(observation, (observations.get(observation) ?? 0) + 1)
    })
    const observed = aggregate ? [...observations].map(([label, count]) => `${count} ${label}`).join(' · ') : itemTrace(node.group, item, trace)
    return { id: node.id, title: node.title, column: node.column,
      selected: node.items.some(i => i.pointer === current?.item.pointer),
      matched: searching && node.items.some(i => matchPointers.has(i.pointer)),
      observation: observed ? `Recorded condition${aggregate ? 's' : ''}: ${observed}` : undefined,
      action: aggregate ? 'Expand rules' : 'View details',
      content: aggregate ? <div className={styles.groupPreview}>
        <ul>{node.items.slice(0, 3).map(i => <li key={i.pointer}>{i.label}</li>)}</ul>
        {node.items.length > 3 && <p>+ {node.items.length - 3} more rules</p>}
        <p>Expand to read each rule’s conditions.</p>
      </div> : <LogicDetails document={model.document} group={node.group.id} item={item} conditions={display.conditions || searching} /> }
  })
  const updateDisplay = (next: typeof display) => { setDisplay(next); rememberLogicDisplay(next) }
  const requestMatch = () => {
    if (!searching) return
    const matches = model.groups.flatMap(g => matchingItems(g, query))
    if (!matches.length) return
    const match = matches[nextMatch.current++ % matches.length]!
    const node = graph.nodes.find(n => n.items.some(i => i.pointer === match.pointer))
    if (mode === 'map' && node) {
      if (node.items.length > 1) setExpanded(previous => new Set([...previous, node.id]))
      setFocusRequest(previous => ({ id: match.pointer, sequence: (previous?.sequence ?? 0) + 1 }))
      root.current?.querySelector('[data-logic-canvas]')?.scrollIntoView({ block: 'nearest' })
    } else {
      const target = Array.from(root.current?.querySelectorAll<HTMLElement>('[data-logic-pointer]') ?? []).find(el => el.dataset.logicPointer === match.pointer)
      target?.scrollIntoView({ block: 'center' }); target?.querySelector('button')?.focus({ preventScroll: true })
    }
  }
  const renderItem = (group: LogicGroup, item: LogicItem) => {
    const observation = itemTrace(group, item, trace)
    return <article key={item.pointer} className={styles.item} data-logic-pointer={item.pointer}
      data-current={current?.item.pointer === item.pointer || undefined} data-search-match={searching && matchPointers.has(item.pointer) || undefined}>
      <InspectionRow label={item.label} aria-label={`View details: ${item.label}`} current={current?.item.pointer === item.pointer}
        value={group.id === 'evidenceRequirements' ? evidenceSummary(item.value) : undefined}
        onClick={() => select(item.pointer)} />
      {group.id !== 'evidenceRequirements' && <div className={styles.itemBody}><LogicDetails document={model.document} group={group.id} item={item} conditions={display.conditions || searching} />
      {observation && <p className={styles.trace}>Recorded condition: {observation}</p>}</div>}
    </article>
  }
  const renderGroup = (id: string) => {
    const group = model.groups.find(g => g.id === id)!
    const items = mode === 'list' && searching ? matchingItems(group, query) : group.items
    if (searching && mode === 'list' && !items.length) return null
    const singleton = id === 'applicability'
    return <section key={id} className={styles.listGroup} aria-label={group.label} data-group={id} data-current={groupId === id || undefined}>
      {!singleton && <h2>{group.label}{id !== 'resolution' ? ` · ${group.items.length}` : ''}</h2>}
      {id === 'rules' && <p className={styles.note}>Rules contribute outcomes independently. Their order does not set priority.</p>}
      {items.length ? <div className={id === 'rules' || id === 'exceptions' ? styles.rules : styles.items}>{items.map(item => renderItem(group, item))}</div>
        : <p className={styles.note}>None declared.</p>}
    </section>
  }
  return <section ref={root} className={styles.logic} aria-label="Pack logic">
    <span ref={setRuler} className={styles.ruler} aria-hidden="true" />
    <div className={styles.toolbar}>
      <SegmentedControl label="Logic view" value={mode} onValueChange={v => onMode(v as LogicMode)} segments={[{ value: 'list', label: 'List' }, { value: 'map', label: 'Map' }]} />
      <form className={styles.search} onSubmit={e => { e.preventDefault(); requestMatch() }}>
        <Input type="search" aria-label="Find pack item" value={query} onChange={e => onQuery(e.target.value)} placeholder={mode === 'map' ? 'Find in map…' : 'Filter items…'} />
      </form>
      {searching && <Button variant="quiet" onClick={requestMatch} disabled={!matchPointers.size}>Next match</Button>}
      <Popover title="Display options" trigger={<Button variant="quiet">Display</Button>}>
        <label className={styles.option}><input type="checkbox" checked={display.conditions} onChange={e => updateDisplay({ ...display, conditions: e.target.checked })} />Show conditions</label>
        <label className={styles.option}><input type="checkbox" checked={display.grouped} onChange={e => { setExpanded(new Set()); updateDisplay({ ...display, grouped: e.target.checked }) }} />Group map rules by outcome</label>
        <p className={styles.note}>Display preferences are remembered. Search always reveals matching conditions.</p>
      </Popover>
      <Button variant="quiet" onClick={openOutline}>Outline</Button>
    </div>
    {searching && <p className={styles.searchStatus} role="status">{matchPointers.size ? `${matchPointers.size} matching items` : `No items match “${query}”.`}</p>}
    <div className={styles.context}>{renderGroup('applicability')}{renderGroup('evidenceRequirements')}</div>
    {mode === 'map' ? mapUnavailable ? <div className={styles.unavailable} role="status">
      <h2>Map unavailable</h2><p>{mapUnavailable}</p><Button onClick={() => onMode('list')}>Read List</Button>
    </div> : <>
      <p className={styles.note}>Rules contribute outcomes independently. Connections show declared outcomes and special-case targets.</p>
      <div className={styles.canvas} data-logic-canvas><Suspense fallback={<p role="status">Loading map…</p>}><RelationshipMap nodes={graphNodes} edges={graph.edges} unit={rem} viewport={viewport}
        focusRequest={focusRequest} onViewportChange={onViewport} onInspect={id => {
          const node = graph.nodes.find(n => n.id === id)
          if (!node) return
          if (node.items.length > 1) setExpanded(previous => new Set([...previous, id]))
          else select(node.items[0]!.pointer)
        }} /></Suspense></div>
      <p className={styles.caption}>{trace ? 'Recorded observations on these pack bytes.' : 'Declared logic. No test results are shown.'} {model.groups.find(g => g.id === 'exceptions')!.items.length === 0 && 'No special cases.'}</p>
    </> : <>{renderGroup('rules')}{renderGroup('exceptions')}{renderGroup('outcomes')}</>}
    {renderGroup('resolution')}{renderGroup('sources')}
  </section>
}
