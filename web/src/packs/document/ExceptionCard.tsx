/** One exception: a rule's card plus the effect and the rule it targets. */
import type { KeyboardEvent } from 'react'
import type { Exception } from '../../mcp/types'
import { ExceptionForm, MoveControls } from '../edit/CardForm'
import { useEditing } from '../edit/editingContext'
import { Block } from './Block'
import { ConditionTree } from './ConditionTree'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'

export function ExceptionCard({
  exception,
  at,
  order
}: {
  exception: Exception
  at: string
  order?: {
    index: number
    count: number
    move: (from: number, to: number) => void
    onKeyDown: (event: KeyboardEvent<HTMLElement>) => void
  }
}) {
  const { editing } = useEditing()
  if (editing) {
    return (
      <Block pointer={at} as="li" className={styles.card}>
        <p className={styles.cardHead}>
          {order !== undefined && (
            <MoveControls
              index={order.index}
              count={order.count}
              move={order.move}
              what="exception"
            />
          )}
        </p>
        <div onKeyDown={order?.onKeyDown}>
          <ExceptionForm at={at} />
        </div>
        <ExtensionsBlock extensions={exception.extensions} at={`${at}/extensions`} />
      </Block>
    )
  }
  return (
    <Block pointer={at} as="li" className={styles.card}>
      <p className={styles.cardHead}>
        <Block pointer={`${at}/id`} as="code" className={styles.id}>
          {exception.id}
        </Block>
        <Block pointer={`${at}/effect`} as="span" className={styles.tag}>
          {exception.effect}
        </Block>
        {exception.targetRule !== undefined && (
          <Block pointer={`${at}/targetRule`} as="span" className={styles.tagQuiet}>
            targets {exception.targetRule}
          </Block>
        )}
      </p>
      {exception.description !== undefined && (
        <Block pointer={`${at}/description`} as="p">
          {exception.description}
        </Block>
      )}
      {exception.when !== undefined && (
        <>
          <p className={styles.fieldLabel}>when</p>
          <ConditionTree condition={exception.when} at={`${at}/when`} />
        </>
      )}
      <p className={styles.outcomeLine}>
        {exception.outcome !== undefined && (
          <Block pointer={`${at}/outcome`} as="span" className={styles.outcomeRef}>
            → {exception.outcome}
          </Block>
        )}
        <Block pointer={`${at}/onUnknown`} as="span" className={styles.tagQuiet}>
          on unknown: {exception.onUnknown}
        </Block>
      </p>
      {(exception.sourceRefs?.length ?? 0) > 0 && (
        <Block pointer={`${at}/sourceRefs`} as="p" className={styles.refs}>
          <span className={styles.fieldLabel}>sources</span>
          {exception.sourceRefs!.map((ref) => (
            <code key={ref} className={styles.id}>
              {ref}
            </code>
          ))}
        </Block>
      )}
      <ExtensionsBlock extensions={exception.extensions} at={`${at}/extensions`} />
    </Block>
  )
}
