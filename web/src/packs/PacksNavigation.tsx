import { Link } from 'react-router-dom'
import { useSyncExternalStore } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { PackInventory } from '../mcp/types'
import styles from './PackWorkspace.module.css'

export type PacksSection = 'packs' | 'tests' | 'flows'

/** Project-wide views of packs. URLs stay compatible with existing bookmarks
 * and never reserve an otherwise valid pack ID such as `tests` or `flows`. */
export function PacksNavigation({ current, count }: { current: PacksSection; count?: number }) {
  const cache = useQueryClient().getQueryCache()
  // Keep the tab label stable between collection views without fetching packs
  // just to decorate navigation (including on an older runtime's flow page).
  const cachedCount = useSyncExternalStore(listener => cache.subscribe(listener), () => {
    const state = cache.find({ queryKey: ['list_packs'], exact: true })?.state
    return state?.status === 'success' ? (state.data as PackInventory)?.packs?.length : undefined
  })
  const total = count ?? cachedCount
  return <nav className={styles.navigation} aria-label="Packs workspace">
    {([['packs', '/packs', 'All packs'], ['tests', '/matrix', 'Tests'], ['flows', '/graphs', 'Pack flows']] as const)
      .map(([id, to, label]) => <Link key={id} to={to} aria-current={id === current ? 'page' : undefined}>{label}
        {id === 'packs' && total !== undefined && <span className={styles.count}>{total}</span>}
      </Link>)}
  </nav>
}
