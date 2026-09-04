/**
 * `/packs` with nothing selected.
 *
 * The pane is beside this, so the page says what to do with it and nothing
 * more. It states no count of its own: the pane is where the listing is
 * reported, including where the listing failed, and two places saying it would
 * be two places that can disagree.
 */
import styles from './PacksLayout.module.css'

export function PacksIndex() {
  return (
    <div className={styles.empty}>
      <h1>Select a pack</h1>
      <p>Its decision, outcomes and rules open here.</p>
    </div>
  )
}
