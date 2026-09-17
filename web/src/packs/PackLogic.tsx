import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
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
import { PackJumpTo } from './PackJumpTo'
import { initialLogicDisplay, rememberLogicDisplay, type LogicMode } from './logicState'
import styles from './PackLogic.module.css'

const RelationshipMap = lazy(() => import('../components/RelationshipMap').then(module => ({ default: module.RelationshipMap })))

export function PackLogic({ model, at, groupId, select, inspect, mode, onMode, query, onQuery, display, onDisplay,
  viewport, onViewport, listScroll, mapUnavailable, trace }: {
  model: LogicProjection; at: string | null; groupId: string | null; select: (pointer: string) => void
  mode: LogicMode; onMode: (mode: LogicMode) => void; query: string; onQuery: (query: string) => void
  inspect: (pointer: string) => void; display: ReturnType<typeof initialLogicDisplay>; onDisplay: (display: ReturnType<typeof initialLogicDisplay>) => void
  viewport: Viewport; onViewport: (v: Viewport) => void
  listScroll: MutableRefObject<number>; mapUnavailable?: string; trace?: readonly TraceEntry[]
}) {
  useLocale()
  const [ruler, setRuler] = useState<HTMLSpanElement | null>(null)
  const rem = useMeasuredBox(ruler)?.width || 16
  useInspectorWorkingWidth(mode === 'map' ? 51 * rem : 34 * rem)
  const root = useRef<HTMLElement>(null)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [focusRequest, setFocusRequest] = useState<{ id: string; sequence: number }>()
  const nextMatch = useRef(0)
  const [jump, setJump] = useState<string | null>(null)
  useLayoutEffect(() => { setExpanded(new Set()); nextMatch.current = 0 }, [model.document])
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
  useLayoutEffect(() => {
    if (jump === null) return
    const node = graph.nodes.find(node => node.items.some(item => item.pointer === jump))
    if (mode === 'map' && node) {
      if (node.items.length > 1) setExpanded(previous => new Set([...previous, node.id]))
      setFocusRequest(previous => ({ id: jump, sequence: (previous?.sequence ?? 0) + 1 }))
      root.current?.querySelector('[data-logic-canvas]')?.scrollIntoView({ block: 'nearest' })
    } else {
      const target = Array.from(root.current?.querySelectorAll<HTMLElement>('[data-logic-pointer]') ?? []).find(el => el.dataset.logicPointer === jump)
      target?.scrollIntoView({ block: 'center' }); target?.querySelector('button')?.focus({ preventScroll: true })
    }
    setJump(null)
  }, [jump, mode, graph])
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
      observation: observed ? aggregate ? msg('Recorded conditions: {{observed}}', { observed }) : msg('Recorded condition: {{observed}}', { observed }) : undefined,
      action: aggregate ? msg('Expand rules') : msg('View details'),
      content: aggregate ? <div className={styles.groupPreview}>
        <ul>{node.items.slice(0, 3).map(i => <li key={i.pointer}>{i.label}</li>)}</ul>
        {node.items.length > 3 && <p><Message text={"+ <0/> more rules"} slots={[node.items.length - 3]} /></p>}
        <p>{msg("Expand to read each rule’s conditions.")}</p>
      </div> : <LogicDetails document={model.document} group={node.group.id} item={item} conditions={display.conditions || searching} /> }
  })
  const updateDisplay = (next: typeof display) => { onDisplay(next); rememberLogicDisplay(next) }
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
      <InspectionRow label={item.label} aria-label={msg("View details: {{value0}}", { value0: item.label })} current={current?.item.pointer === item.pointer}
        description={group.id === 'evidenceRequirements' ? evidenceSummary(item.value) : undefined}
        onClick={() => inspect(item.pointer)} />
      {group.id !== 'evidenceRequirements' && <div className={styles.itemBody}><LogicDetails document={model.document} group={group.id} item={item} conditions={display.conditions || searching} />
      {observation && <p className={styles.trace}><Message text={"Recorded condition: <0/>"} slots={[observation]} /></p>}</div>}
    </article>
  }
  const renderGroup = (id: string) => {
    const group = model.groups.find(g => g.id === id)!
    const items = mode === 'list' && searching ? matchingItems(group, query) : group.items
    if (searching && mode === 'list' && !items.length) return null
    const singleton = id === 'applicability'
    return <section key={id} className={styles.listGroup} aria-label={group.label} data-group={id} data-current={groupId === id || undefined}>
      {!singleton && <h2>{group.label}{id !== 'resolution' ? ` · ${group.items.length}` : ''}</h2>}
      {id === 'rules' && <p className={styles.note}>{msg("Rules contribute outcomes independently. Their order does not set priority.")}</p>}
      {items.length ? <div className={id === 'rules' || id === 'exceptions' ? styles.rules : styles.items}>{items.map(item => renderItem(group, item))}</div>
        : <p className={styles.note}>{msg("None declared.")}</p>}
    </section>
  }
  return <section ref={root} className={styles.logic} aria-label={msg("Pack logic")}>
    <span ref={setRuler} className={styles.ruler} aria-hidden="true" />
    <div className={styles.toolbar}>
      <SegmentedControl label={msg("Logic view")} value={mode} onValueChange={v => onMode(v as LogicMode)} segments={[{ value: 'list', label: "List" }, { value: 'map', label: "Map" }]} />
      <form className={styles.search} onSubmit={e => { e.preventDefault(); requestMatch() }}>
        <Input type="search" aria-label={msg("Find pack item")} value={query} onChange={e => onQuery(e.target.value)} placeholder={mode === 'map' ? msg("Find in map…") : msg("Filter items…")} />
      </form>
      {searching && <Button variant="quiet" onClick={requestMatch} disabled={!matchPointers.size}>{msg("Next match")}</Button>}
      <Popover title={msg("Display options")} trigger={<Button variant="quiet">{msg("Display")}</Button>}>
        <label className={styles.option}><Message text={"<0/>Show conditions"} slots={[<input type="checkbox" checked={display.conditions} onChange={e => updateDisplay({ ...display, conditions: e.target.checked })} />]} /></label>
        <label className={styles.option}><Message text={"<0/>Group map rules by outcome"} slots={[<input type="checkbox" checked={display.grouped} onChange={e => { setExpanded(new Set()); updateDisplay({ ...display, grouped: e.target.checked }) }} />]} /></label>
        <p className={styles.note}>{msg("Display preferences are remembered. Search always reveals matching conditions.")}</p>
      </Popover>
      <PackJumpTo model={model} at={at} onJump={pointer => { onQuery(''); select(pointer); setJump(pointer) }} />
    </div>
    {searching && <p className={styles.searchStatus} role="status">{matchPointers.size ? msg("{{value0}} matching items", { value0: matchPointers.size }) : msg("No items match “{{value0}}”.", { value0: query })}</p>}
    <div className={styles.context}>{renderGroup('applicability')}{renderGroup('evidenceRequirements')}</div>
    {mode === 'map' ? mapUnavailable ? <div className={styles.unavailable} role="status">
      <h2>{msg("Map unavailable")}</h2><p>{mapUnavailable}</p><Button onClick={() => onMode('list')}>{msg("Read List")}</Button>
    </div> : <>
      <p className={styles.note}>{msg("Rules contribute outcomes independently. Connections show declared outcomes and special-case targets.")}</p>
      <div className={styles.canvas} data-logic-canvas><Suspense fallback={<p role="status">{msg("Loading map…")}</p>}><RelationshipMap nodes={graphNodes} edges={graph.edges} unit={rem} viewport={viewport}
        focusRequest={focusRequest} onViewportChange={onViewport} onSelect={id => {
          const node = graph.nodes.find(n => n.id === id)
          if (!node) return
          if (node.items.length > 1) setExpanded(previous => new Set([...previous, id]))
          else select(node.items[0]!.pointer)
        }} onInspect={id => {
          const node = graph.nodes.find(n => n.id === id)
          if (node && node.items.length > 1) setExpanded(previous => new Set([...previous, id]))
          else inspect(id)
        }} /></Suspense></div>
      <p className={styles.caption}>{trace ? msg("Recorded observations on these pack bytes.") : msg("Declared logic. No test results are shown.")} {model.groups.find(g => g.id === 'exceptions')!.items.length === 0 && msg("No special cases.")}</p>
    </> : <>{renderGroup('rules')}{renderGroup('exceptions')}{renderGroup('outcomes')}</>}
    {renderGroup('resolution')}{renderGroup('sources')}
  </section>
}
