/**
 * One rule, in the document's own order.
 *
 * Order is §7-significant, so the card carries its index as well as its id:
 * "the second rule" is a fact about this document and `/rules/1` is the
 * address that says it. In edit mode the index is also a control — the two
 * move buttons, and `Alt+ArrowUp`/`Alt+ArrowDown` inside the card.
 */
import type { KeyboardEvent } from 'react'
import type { Rule } from '../../mcp/types'
import { MoveControls, RuleForm } from '../edit/CardForm'
import { useEditing } from '../edit/editingContext'
import { Block } from './Block'
import { ConditionTree } from './ConditionTree'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'

export function RuleCard({
  rule,
  at,
  order
}: {
  rule: Rule
  at: string
  /** Present in edit mode: where this card sits, and how it moves. */
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
      /*
        The chord is on the card itself, which is the element the move effect
        focuses. Bound one level in, it fired once and then never again: the
        second press came from the `li` the first press had moved focus to,
        and a keydown on the `li` never reaches a handler on its child.
      */
      <Block pointer={at} as="li" className={styles.card} onKeyDown={order?.onKeyDown}>
        <p className={styles.cardHead}>
          {order !== undefined && (
            <MoveControls
              index={order.index}
              count={order.count}
              move={order.move}
              what="rule"
            />
          )}
        </p>
        <RuleForm at={at} />
        <ExtensionsBlock extensions={rule.extensions} at={`${at}/extensions`} />
      </Block>
    )
  }
  return (
    <Block pointer={at} as="li" className={styles.card}>
      <p className={styles.cardHead}>
        <Block pointer={`${at}/id`} as="code" className={styles.id}>
          {rule.id}
        </Block>
      </p>
      {rule.description !== undefined && (
        <Block pointer={`${at}/description`} as="p">
          {rule.description}
        </Block>
      )}
      {rule.when !== undefined && (
        <>
          <p className={styles.fieldLabel}>when</p>
          <ConditionTree condition={rule.when} at={`${at}/when`} />
        </>
      )}
      <p className={styles.outcomeLine}>
        <Block pointer={`${at}/outcome`} as="span" className={styles.outcomeRef}>
          → {rule.outcome}
        </Block>
        <Block pointer={`${at}/onUnknown`} as="span" className={styles.tagQuiet}>
          on unknown: {rule.onUnknown}
        </Block>
      </p>
      {(rule.evidenceRequirementRefs?.length ?? 0) > 0 && (
        <Block pointer={`${at}/evidenceRequirementRefs`} as="p" className={styles.refs}>
          <span className={styles.fieldLabel}>evidence</span>
          {rule.evidenceRequirementRefs!.map((ref) => (
            <code key={ref} className={styles.id}>
              {ref}
            </code>
          ))}
        </Block>
      )}
      {(rule.sourceRefs?.length ?? 0) > 0 && (
        <Block pointer={`${at}/sourceRefs`} as="p" className={styles.refs}>
          <span className={styles.fieldLabel}>sources</span>
          {rule.sourceRefs!.map((ref) => (
            <code key={ref} className={styles.id}>
              {ref}
            </code>
          ))}
        </Block>
      )}
      {rule.rationale !== undefined && (
        <Block pointer={`${at}/rationale`} as="p" className={styles.rationale}>
          {rule.rationale}
        </Block>
      )}
      <ExtensionsBlock extensions={rule.extensions} at={`${at}/extensions`} />
    </Block>
  )
}
