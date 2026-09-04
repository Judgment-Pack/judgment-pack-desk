/**
 * The line a member the document does not declare gets.
 *
 * This is the whole reason the old view was replaced. `components/primitives.tsx`
 * has a `Section` that returns null when it has nothing to render, so a pack
 * with no `applicability` and a view that had simply forgotten to draw one
 * looked identical. An omission is a fact about the document — it is what
 * "this pack does not narrow its own scope" means — and a reading view that
 * cannot state it is a view a reader cannot trust about anything it does not
 * show.
 *
 * It carries the member's pointer like every other block, so a diagnostic
 * about the absent member has somewhere to land and the future form field is
 * already addressed.
 */
import { Block } from './Block'
import styles from './PackDocument.module.css'

export function OmittedMember({
  pointer,
  label,
  note
}: {
  pointer: string
  label: string
  /** What the absence means, where the document's own vocabulary says. */
  note?: string
}) {
  return (
    <Block pointer={pointer} className={styles.omitted}>
      <h2 className={styles.heading}>{label}</h2>
      <p className={styles.omittedLine}>
        <span className={styles.omittedTag}>not declared</span>
        {note !== undefined && <span> {note}</span>}
      </p>
    </Block>
  )
}
