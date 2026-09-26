import { msg, useLocale } from '../i18n'
import { Link } from 'react-router-dom'
import type { ReactNode } from 'react'
import styles from './PackWorkspace.module.css'

/** The collection header keeps its folder toggle in the same place. */
export function PacksNavigation({ count, leading }: { count?: number; leading?: ReactNode }) {
  useLocale()
  return <nav className={styles.navigation} aria-label={msg("Packs workspace")}>
    {leading}
    <Link to="/packs" aria-current="page">{msg('Browse')}
      {count !== undefined && <span className={styles.count}>{count}</span>}
    </Link>
  </nav>
}
