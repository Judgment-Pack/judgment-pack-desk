/**
 * Retain the collection while a pack is open so Back preserves its controls,
 * selection and scroll position. Only the active page publishes to Inspector.
 * Evaluation and matrix pages remain separate route branches.
 */
import { Outlet, useParams } from 'react-router-dom'
import { PacksPane } from '../packs/PacksPane'
import styles from './PacksLayout.module.css'

export function PacksLayout() {
  const { packId } = useParams()
  return (
    <div className={styles.layout} data-measure="full">
      <div hidden={Boolean(packId)}><PacksPane active={!packId} /></div>
      <div className={styles.main}>
        <Outlet />
      </div>
    </div>
  )
}
