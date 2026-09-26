/**
 * A Radix Tabs, styled through this component's own module.
 *
 * The Inspector's three panels are one tab set, and this is the one control
 * this phase needs that `ui/` did not already have. Radix is what gives the
 * roving tabindex, the arrow keys, Home/End and the `role="tab"` /
 * `role="tabpanel"` relationship the tests query by — none of which a set of
 * buttons has by accident.
 *
 * The value is **controlled by the caller**, because the Inspector's selection
 * lives in the route rather than in the pane: `RightPane` swaps its wrapper at
 * 1100px and remounts the subtree, so anything this component held for itself
 * would be lost at that breakpoint.
 */
import { Tabs as RadixTabs } from 'radix-ui'
import { useLayoutEffect, useRef, type ReactNode } from 'react'
import styles from './Tabs.module.css'

export interface TabDefinition {
  value: string
  label: string
  panel: ReactNode
}

export function Tabs({
  label,
  value,
  onValueChange,
  tabs,
  scrollable = false,
  resetScrollKey,
  keepMounted = false,
  tools,
  fillPanel,
  variant = 'pane'
}: {
  /** The tab list's accessible name. */
  label: string
  value: string
  onValueChange: (value: string) => void
  tabs: readonly TabDefinition[]
  /** Keep this tab list outside the pane's scrolling active panel. */
  scrollable?: boolean
  /** A new inspected item starts at its heading, independent of prior scroll. */
  resetScrollKey?: string
  keepMounted?: boolean
  tools?: ReactNode
  fillPanel?: string
  /** Document tabs share the page gutter; nested panes keep their compact inset. */
  variant?: 'pane' | 'page'
}) {
  const root = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    if (!scrollable || keepMounted) return
    const active = root.current?.querySelector<HTMLElement>('[data-pane-scroll][data-state="active"]')
    if (active) active.scrollTop = 0
  }, [scrollable, resetScrollKey, value, keepMounted])
  return (
    <RadixTabs.Root
      ref={root}
      className={styles.root}
      data-pane-tabs={scrollable || undefined}
      data-variant={variant}
      value={value}
      onValueChange={onValueChange}
      activationMode="automatic"
    >
      <div className={styles.heading}><RadixTabs.List className={styles.list} aria-label={label}>
        {tabs.map((tab) => (
          <RadixTabs.Trigger key={tab.value} className={styles.trigger} value={tab.value}>
            {tab.label}
          </RadixTabs.Trigger>
        ))}
      </RadixTabs.List>{tools}</div>
      {tabs.map((tab) => (
        <RadixTabs.Content key={tab.value} forceMount={keepMounted || undefined} hidden={value !== tab.value} data-fill={fillPanel === tab.value || undefined} className={styles.content} data-pane-scroll={scrollable || undefined} value={tab.value}>
          {tab.panel}
        </RadixTabs.Content>
      ))}
    </RadixTabs.Root>
  )
}
