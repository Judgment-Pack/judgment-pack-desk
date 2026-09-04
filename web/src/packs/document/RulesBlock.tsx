/** The rules, in the document's own order — which is the order they apply in. */
import type { Rule } from '../../mcp/types'
import { Block } from './Block'
import { RuleCard } from './RuleCard'
import styles from './PackDocument.module.css'

export function RulesBlock({ rules, at }: { rules: Rule[]; at: string }) {
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Rules — document order</h2>
      <ol className={styles.cards}>
        {rules.map((rule, index) => (
          <RuleCard key={`${rule.id}-${index}`} rule={rule} at={`${at}/${index}`} />
        ))}
      </ol>
    </Block>
  )
}
