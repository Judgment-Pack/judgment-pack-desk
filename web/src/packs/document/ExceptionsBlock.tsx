/** The exceptions, in the document's own order. */
import type { Exception } from '../../mcp/types'
import { OrderAnnouncement, useCardOrder } from '../edit/CardForm'
import { useEditing } from '../edit/editingContext'
import { Block } from './Block'
import { ExceptionCard } from './ExceptionCard'
import styles from './PackDocument.module.css'
import { MisshapenMember, isRecord } from './MisshapenMember'

export function ExceptionsBlock({ exceptions, at }: { exceptions: Exception[]; at: string }) {
  const { editing } = useEditing()
  const { move, announcement, onCardKey } = useCardOrder(at, exceptions.length)
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Exceptions</h2>
      {editing && <OrderAnnouncement text={announcement} />}
      <ol className={styles.cards}>
        {exceptions.map((exception, index) =>
          !isRecord(exception) ? (
            <li key={`misshapen-${index}`}>
              <MisshapenMember
                pointer={`${at}/${index}`}
                label={`Exception ${index + 1}`}
                expected="an object"
                value={exception}
              />
            </li>
          ) : (
          <ExceptionCard
            key={`${exception.id}-${index}`}
            exception={exception}
            at={`${at}/${index}`}
            order={
              editing
                ? { index, count: exceptions.length, move, onKeyDown: onCardKey(index) }
                : undefined
            }
          />
          )
        )}
      </ol>
    </Block>
  )
}
