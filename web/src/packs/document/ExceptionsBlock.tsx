/** The exceptions, in the document's own order. */
import type { Exception } from '../../mcp/types'
import { Block } from './Block'
import { ExceptionCard } from './ExceptionCard'
import styles from './PackDocument.module.css'

export function ExceptionsBlock({ exceptions, at }: { exceptions: Exception[]; at: string }) {
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Exceptions</h2>
      <ol className={styles.cards}>
        {exceptions.map((exception, index) => (
          <ExceptionCard
            key={`${exception.id}-${index}`}
            exception={exception}
            at={`${at}/${index}`}
          />
        ))}
      </ol>
    </Block>
  )
}
