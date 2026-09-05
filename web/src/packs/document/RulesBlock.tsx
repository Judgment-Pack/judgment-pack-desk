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
import { MisshapenMember, isRecord } from './MisshapenMember'

export function RulesBlock({ rules, at }: { rules: Rule[]; at: string }) {
  const { editing } = useEditing()
  const { move, announcement, onCardKey } = useCardOrder(at, rules.length)
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Rules — document order</h2>
      {editing && <OrderAnnouncement text={announcement} />}
      <ol className={styles.cards}>
        {rules.map((rule, index) =>
          // A rule that is not an object has no fields to draw and no card to
          // put them in — `rule.id` on a `null` is where the route used to end.
          !isRecord(rule) ? (
            <li key={`misshapen-${index}`}>
              <MisshapenMember
                pointer={`${at}/${index}`}
                label={`Rule ${index + 1}`}
                expected="an object"
                value={rule}
              />
            </li>
          ) : (
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
          )
        )}
      </ol>
    </Block>
  )
}
