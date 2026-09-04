/**
 * The packs pane, and whatever is selected beside it.
 *
 * A **layout route**, so the pane survives every change to the child: choosing
 * a different pack, and — when it lands — switching into `?edit`. A pane that
 * remounted on selection would lose its filter, its sort and its scroll
 * position every time someone used it.
 *
 * `/packs/:packId/evaluate` and `/packs/:packId/matrix` stay outside this
 * layout, as their own branches. Those two views are untouched by this work
 * and nesting them here would hand them a pane they never asked for and a
 * column width they were not drawn at.
 */
import { Outlet } from 'react-router-dom'
import { PacksPane } from '../packs/PacksPane'
import styles from './PacksLayout.module.css'

export function PacksLayout() {
  return (
    <div className={styles.layout}>
      <PacksPane />
      <div className={styles.main}>
        <Outlet />
      </div>
    </div>
  )
}
