import { useLayoutEffect, useRef, useState } from 'react'
import { msg } from '../i18n'
import { Menu } from './Menu'
import { Button } from './Button'
import { IconChevronDown } from '../shell/icons'
import styles from './TabOverflow.module.css'

type Item = { value: string; label: string }
type Measurements = { available: number; widths: number[]; more: number; gap: number }

/** Keep the active section visible, preserving the canonical order of the rest. */
export function fittingTabs(items: readonly Item[], active: string, measurements?: Measurements): Set<string> {
  const visible = new Set(items.map(item => item.value))
  if (!measurements || measurements.widths.some(width => width <= 0)) return visible
  const {available, widths, more, gap} = measurements
  if (widths.reduce((sum, width) => sum + width, 0) + gap * (items.length - 1) <= available) return visible
  visible.clear()
  let remaining = available - more
  const selected = items.findIndex(item => item.value === active)
  if (selected >= 0) {
    visible.add(active)
    remaining -= widths[selected]! + gap
  }
  items.forEach((item, index) => {
    if (index !== selected && widths[index]! + gap <= remaining) {
      visible.add(item.value)
      remaining -= widths[index]! + gap
    }
  })
  return visible
}

export function useTabOverflow(items: readonly Item[], active: string, enabled: boolean) {
  const container = useRef<HTMLDivElement>(null), measure = useRef<HTMLDivElement>(null)
  const [sizes, setSizes] = useState<Measurements>()
  const labels = items.map(item => item.label).join('\0')
  useLayoutEffect(() => {
    if (!enabled) return
    const update = () => {
      if (!container.current || !measure.current || container.current.clientWidth <= 0) return
      const elements = Array.from(measure.current.children)
      const widths = elements.slice(0, -1).map(element => element.getBoundingClientRect().width)
      const next = {available: container.current.clientWidth, widths, more: elements.at(-1)!.getBoundingClientRect().width, gap: parseFloat(getComputedStyle(measure.current).columnGap) || 0}
      setSizes(previous => JSON.stringify(previous) === JSON.stringify(next) ? previous : next)
    }
    update()
    if (typeof ResizeObserver === 'undefined' || !container.current) return
    const observer = new ResizeObserver(update)
    observer.observe(container.current)
    if (measure.current) observer.observe(measure.current)
    return () => observer.disconnect()
  }, [labels, enabled])
  return {container, visible: fittingTabs(items, active, enabled ? sizes : undefined), measurements:
    <div ref={measure} className={styles.measure} aria-hidden="true">
      {items.map(item => <span key={item.value}>{item.label}</span>)}
      <Button variant="quiet" tabIndex={-1}>{msg('More')}<IconChevronDown/></Button>
    </div>}
}

export function TabOverflowMenu({ items, onSelect }: { items: readonly Item[]; onSelect: (value: string) => void }) {
  if (!items.length) return null
  return <Menu.Root><Menu.Trigger asChild><Button variant="quiet" className={styles.more}>{msg('More')}<IconChevronDown/></Button></Menu.Trigger>
    <Menu.Portal><Menu.Content align="end" aria-label={msg('More sections')}>
      {items.map(item => <Menu.Item key={item.value} onSelect={() => onSelect(item.value)}>{item.label}</Menu.Item>)}
    </Menu.Content></Menu.Portal>
  </Menu.Root>
}
