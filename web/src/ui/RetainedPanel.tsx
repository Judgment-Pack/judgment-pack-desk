import { useState, type ReactNode } from 'react'
import styles from './RetainedPanel.module.css'

/** Mount on first visit; keep drafts and revision guards alive between tabs.
 * Hidden panels remain outside layout, keyboard navigation and the a11y tree.
 * Unmounting the containing page releases all state, including DOM-only inputs.
 */
export function RetainedPanel({ active, children }: { active: boolean; children: ReactNode }) {
  const [visited, setVisited] = useState(active)
  if (active && !visited) setVisited(true)
  if (!active && !visited) return null
  return <div className={styles.panel} hidden={!active}>{children}</div>
}
