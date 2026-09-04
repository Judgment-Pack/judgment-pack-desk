/**
 * The rules, in the document's own order — which is the order they apply in.
 *
 * The order is editable here and nowhere else, because it is a fact about the
 * list rather than about any one rule: the moving state has to outlive the two
 * cards that exchange places, and both of them remount when they do.
 */
import type { Rule } from '../../mcp/types'
import { OrderAnnouncement, useCardOrder } from '../edit/CardForm'
import { useEditing } from '../edit/editingContext'
import { Block } from './Block'
import { RuleCard } from './RuleCard'
import styles from './PackDocument.module.css'

export function RulesBlock({ rules, at }: { rules: Rule[]; at: string }) {
  const { editing } = useEditing()
  const { move, announcement, onCardKey } = useCardOrder(at, rules.length)
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Rules — document order</h2>
      {editing && <OrderAnnouncement text={announcement} />}
      <ol className={styles.cards}>
        {rules.map((rule, index) => (
          <RuleCard
            key={`${rule.id}-${index}`}
            rule={rule}
            at={`${at}/${index}`}
            order={
              editing
                ? { index, count: rules.length, move, onKeyDown: onCardKey(index) }
                : undefined
            }
          />
        ))}
      </ol>
    </Block>
  )
}
