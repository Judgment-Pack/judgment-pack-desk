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
 * about the absent member has somewhere to land and the form field is already
 * addressed by it.
 *
 * **In edit mode the line is also the way to write one.** The bytes it writes
 * are the schema's required members, empty — see `edit/shape.ts` — and it is
 * one splice into the document's own layout, at the position the schema's
 * property order gives the member. Where this desk knows no shape for the
 * member, there is no button rather than a button that writes a guess.
 */
import { Button } from '../../ui/Button'
import { useEditing } from '../edit/editingContext'
import { starterFor } from '../edit/shape'
import { setRawJson } from '../edit/writes'
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
  const { editing, write } = useEditing()
  const starter = starterFor(pointer)
  return (
    <Block pointer={pointer} className={styles.omitted}>
      <h2 className={styles.heading}>{label}</h2>
      <p className={styles.omittedLine}>
        <span className={styles.omittedTag}>not declared</span>
        {note !== undefined && <span> {note}</span>}
        {editing && starter !== undefined && (
          <Button
            variant="quiet"
            onClick={() => write((current) => setRawJson(current, pointer, starter))}
          >
            Declare it
          </Button>
        )}
      </p>
    </Block>
  )
}
