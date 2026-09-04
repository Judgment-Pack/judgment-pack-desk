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
import type { ReactNode } from 'react'
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
  tabs
}: {
  /** The tab list's accessible name. */
  label: string
  value: string
  onValueChange: (value: string) => void
  tabs: readonly TabDefinition[]
}) {
  return (
    <RadixTabs.Root
      className={styles.root}
      value={value}
      onValueChange={onValueChange}
      activationMode="automatic"
    >
      <RadixTabs.List className={styles.list} aria-label={label}>
        {tabs.map((tab) => (
          <RadixTabs.Trigger key={tab.value} className={styles.trigger} value={tab.value}>
            {tab.label}
          </RadixTabs.Trigger>
        ))}
      </RadixTabs.List>
      {tabs.map((tab) => (
        <RadixTabs.Content key={tab.value} className={styles.content} value={tab.value}>
          {tab.panel}
        </RadixTabs.Content>
      ))}
    </RadixTabs.Root>
  )
}
