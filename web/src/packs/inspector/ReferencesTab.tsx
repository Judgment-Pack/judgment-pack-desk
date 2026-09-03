/**
 * What the selected member refers to, and what refers back to it.
 *
 * Every line is a **document fact**. Where an id resolves to nothing the line
 * says "no declared outcome carries this id" and stops there: the runtime
 * issues `JPS-SEMANTIC-UNRESOLVED-OUTCOME` and this panel must not shadow it
 * with a verdict of its own.
 */
import { Link } from 'react-router-dom'
import type { Reference } from '../references'
import styles from './PackInspector.module.css'

export function ReferencesTab({
  references,
  packId
}: {
  references: readonly Reference[]
  packId: string
}) {
  if (references.length === 0) {
    return <p className={styles.empty}>This member names nothing, and nothing names it.</p>
  }
  return (
    <ul className={styles.references}>
      {references.map((reference, index) => (
        <li key={`${reference.relation}-${reference.id}-${index}`} className={styles.reference}>
          <span className={styles.relation}>{reference.relation}</span>
          <span aria-hidden="true"> </span>
          {reference.candidates !== undefined ? (
            // **Every candidate, and no choice between them.** A last-wins map
            // linked one of two identically named outcomes as though the
            // document had said which; it had said the id twice. The runtime
            // refuses a duplicate id — until it does, this says what is there
            // and offers each place to go and look.
            <>
              <code className={styles.id}>{reference.id}</code>
              <span className={styles.unresolved}>
                {' '}
                is declared {reference.candidates.length} times — this document does not say which
              </span>
              {reference.candidates.map((candidate) => (
                <span key={candidate}>
                  {' '}
                  <Link
                    to={`/packs/${encodeURIComponent(packId)}?at=${encodeURIComponent(candidate)}`}
                    replace
                  >
                    <code className={styles.id}>{candidate}</code>
                  </Link>
                </span>
              ))}
            </>
          ) : reference.target === undefined ? (
            <>
              <code className={styles.id}>{reference.id}</code>
              <span className={styles.unresolved}> {reference.unresolved}</span>
            </>
          ) : (
            <Link
              to={`/packs/${encodeURIComponent(packId)}?at=${encodeURIComponent(reference.target)}`}
              replace
            >
              <code className={styles.id}>{reference.id}</code>
            </Link>
          )}
        </li>
      ))}
    </ul>
  )
}
