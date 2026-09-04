/**
 * The outcomes this pack can produce, as chips, with the fallback tagged.
 *
 * The tag comes from `fallbackOutcome` and is a **document fact**: it says
 * which outcome the document names as its fallback, not that the fallback is
 * the right one or that it will be reached. It is drawn in both modes, and in
 * neither is it editable here — which outcome is the fallback is `/fallbackOutcome`'s
 * own member and is edited at its own address.
 */
import type { Outcome } from '../../mcp/types'
import { useEditing } from '../edit/editingContext'
import { StringField, TextField } from '../edit/fields'
import { Block } from './Block'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'

export function OutcomesBlock({
  outcomes,
  fallback,
  at
}: {
  outcomes: Outcome[]
  fallback: string | undefined
  at: string
}) {
  const { editing } = useEditing()
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Outcomes</h2>
      <ul className={styles.chips}>
        {outcomes.map((outcome, index) => (
          <li key={`${outcome.id}-${index}`}>
            <Block pointer={`${at}/${index}`} as="div" className={styles.chip}>
              {editing ? (
                <OutcomeForm at={`${at}/${index}`} fallback={fallback === outcome.id} />
              ) : (
                <>
                  <span className={styles.chipLabel}>{outcome.label}</span>
                  <code className={styles.id}>{outcome.id}</code>
                  {fallback === outcome.id && <span className={styles.tag}>fallback</span>}
                  {outcome.description !== undefined && (
                    <p className={styles.chipNote}>{outcome.description}</p>
                  )}
                </>
              )}
              <ExtensionsBlock extensions={outcome.extensions} at={`${at}/${index}/extensions`} />
            </Block>
          </li>
        ))}
      </ul>
    </Block>
  )
}

function OutcomeForm({ at, fallback }: { at: string; fallback: boolean }) {
  return (
    <>
      <StringField pointer={`${at}/id`} label="id" />
      <StringField pointer={`${at}/label`} label="label" />
      <TextField pointer={`${at}/description`} label="description" rows={2} />
      {fallback && <span className={styles.tag}>fallback</span>}
    </>
  )
}
