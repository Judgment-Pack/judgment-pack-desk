import { useState, type ReactNode } from 'react'
import { msg } from '../i18n'
import { SegmentedControl } from '../ui/SegmentedControl'
import { GraphRehearsal } from './GraphRehearsal'
import styles from '../definitions/DefinitionWorkspace.module.css'

/** Both modes use saved files. Changing modes never starts a run. */
export function GraphTestsWorkspace({ graphId, initialTry = false, actions, children }: { graphId: string; initialTry?: boolean; actions?: ReactNode; children: ReactNode }) {
  const [view, setView] = useState(initialTry ? 'try' : 'cases')
  return <section aria-label={msg('Graph test workspace')}>
    <div className={styles.toolbar}><SegmentedControl label={msg('Test views')} value={view} onValueChange={setView} segments={[
      {value: 'cases', label: msg('Saved cases')}, {value: 'try', label: msg('Try inputs')}
    ]}/>{view==='cases' && <div className={styles.actions}>{actions}</div>}</div>
    <div className={styles.panel} hidden={view !== 'cases'}>{children}</div>
    <div className={styles.panel} hidden={view !== 'try'}><GraphRehearsal graphId={graphId}/></div>
  </section>
}
