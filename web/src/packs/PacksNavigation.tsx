import { Link } from 'react-router-dom'
import styles from './PackWorkspace.module.css'

export type PacksSection = 'packs' | 'tests' | 'flows'

/** Project-wide views of packs. URLs stay compatible with existing bookmarks
 * and never reserve an otherwise valid pack ID such as `tests` or `flows`. */
export function PacksNavigation({ current }: { current: PacksSection }) {
  return <nav className={styles.navigation} aria-label="Packs workspace">
    {([['packs', '/packs', 'All packs'], ['tests', '/matrix', 'Tests'], ['flows', '/graphs', 'Pack flows']] as const)
      .map(([id, to, label]) => <Link key={id} to={to} aria-current={id === current ? 'page' : undefined}>{label}</Link>)}
  </nav>
}
