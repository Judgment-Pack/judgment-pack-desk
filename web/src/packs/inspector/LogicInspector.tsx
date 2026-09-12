import { useLayoutEffect, useRef, type MutableRefObject, type ReactNode } from 'react'
import type { TraceEntry } from '../../mcp/types'
import { Button } from '../../ui/Button'
import { CodeBlock } from '../../ui/CodeBlock'
import { MemberValue } from './MemberValue'
import { ConditionTree } from '../document/ConditionTree'
import { isRecord } from '../document/MisshapenMember'
import { valueAt } from '../pointers'
import { itemTrace, matchingItems, outcomeLabel, selectedItem, text, type LogicProjection } from '../logicModel'
import styles from './LogicInspector.module.css'

export function LogicInspector({ model, at, pane, query, onSelect, onOutline, outlineScroll, trace, advanced }: {
  model: LogicProjection; at: string | null; pane: 'outline' | 'detail'; query: string
  onSelect: (pointer: string) => void; onOutline: () => void; outlineScroll: MutableRefObject<number>
  trace?: readonly TraceEntry[]; advanced: ReactNode
}) {
  const outline = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => { if (outline.current) outline.current.scrollTop = outlineScroll.current }, [pane, outlineScroll])
  const selected = selectedItem(model, at)
  if (pane === 'outline') return <div className={styles.outline} ref={outline} onScroll={e => { outlineScroll.current = e.currentTarget.scrollTop }}>
    <h2>Pack outline</h2>
    {model.groups.map(group => {
      const matches = matchingItems(group, query)
      if (!matches.length) return null
      return <section key={group.id} className={styles.group}>
        <h3>{group.label}{group.items.length > 1 ? ` · ${group.items.length}` : ''}</h3>
        {matches.map(item => <button type="button" key={item.pointer} className={styles.outlineRow}
          aria-pressed={selected?.item.pointer === item.pointer} data-outline-pointer={item.pointer}
          onClick={() => onSelect(item.pointer)}>{item.label}
          {item.effect && <span>{item.effect}</span>}
          {itemTrace(group, item, trace) && <span className={styles.observation}>{itemTrace(group, item, trace)}</span>}
        </button>)}
      </section>
    })}
    {!model.groups.some(g => matchingItems(g, query).length) && <p role="status">No matching items.</p>}
  </div>

  if (at === null) return <div className={styles.details}><p>Select an item to inspect its definition.</p><Button onClick={onOutline}>Open Outline</Button></div>
  const value = selected?.item.value ?? valueAt(model.document, at)
  const condition = selected?.group.id === 'applicability' ? value : isRecord(value) ? value.when : undefined
  const pointer = selected?.item.pointer ?? at
  const observed = selected && itemTrace(selected.group, selected.item, trace)
  const group = model.groups.find(g => '/' + g.id === at)
  return <div className={styles.details}>
    <Button variant="quiet" onClick={onOutline}>← Outline</Button>
    <h2>{selected?.item.label ?? group?.label ?? (at.slice(1) || 'Document')}</h2>
    <p className={styles.meta}><code>{pointer || '/'}</code></p>
    {observed && <p className={styles.observation}>Recorded condition: <strong>{observed}</strong></p>}
    {condition !== undefined && <section className={styles.group}><h3>Condition</h3>
      <ConditionTree readOnly condition={condition} at={selected?.group.id === 'applicability' ? '/applicability' : `${pointer}/when`} />
    </section>}
    {isRecord(value) && (value.outcome !== undefined || value.effect !== undefined) && <section className={styles.group}>
      <h3>{value.effect === undefined ? 'Candidate outcome' : 'Effect'}</h3>
      <p>{selected?.item.effect}</p>
      <h3>If unknown</h3><p>{text(value.onUnknown)}</p>
    </section>}
    {pointer === '/fallbackOutcome' && <section className={styles.group}><h3>Fallback outcome</h3><p>{outcomeLabel(model.document, value)}</p><p className={styles.meta}>A fallback does not itself request a handoff.</p></section>}
    {isRecord(value) && typeof value.description === 'string' && <details className={styles.group}><summary>Author description</summary><p>{value.description}</p></details>}
    {group && !selected && <section className={styles.group}>{group.items.map(item => <Button key={item.pointer} variant="quiet" onClick={() => onSelect(item.pointer)}>{item.label}</Button>)}{!group.items.length && <p>None declared.</p>}</section>}
    {condition === undefined && pointer !== '/fallbackOutcome' && <Definition value={value} />}
    <details className={styles.group}><summary>Exact {condition !== undefined ? 'condition' : 'definition'} JSON</summary><CodeBlock text={JSON.stringify(condition ?? value, null, 2) ?? 'Not declared'} /></details>
    <section className={styles.group} aria-label="References, checks and metadata">{advanced}</section>
  </div>
}

function Definition({ value }: { value: unknown }) {
  if (value === undefined) return <p>Not declared.</p>
  if (!isRecord(value)) return <MemberValue value={value} />
  return <dl className={styles.definition}>{Object.entries(value).filter(([key]) => !['description', 'extensions'].includes(key)).map(([key, child]) => <div key={key}>
    <dt>{key}</dt><dd><MemberValue value={child} /></dd>
  </div>)}</dl>
}
