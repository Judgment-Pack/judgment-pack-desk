/**
 * The condition that says where this pack decides at all.
 *
 * Rendered as the same unparaphrased tree a rule's `when` gets, because it is
 * the same kind of object and reading it should not depend on where it sits.
 */
import type { Condition } from '../../mcp/types'
import { Block } from './Block'
import { ConditionTree } from './ConditionTree'
import styles from './PackDocument.module.css'

export function ApplicabilityBlock({
  applicability,
  at
}: {
  applicability: Condition
  at: string
}) {
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Applicability</h2>
      <p className={styles.note}>The pack decides only where this holds.</p>
      <ConditionTree condition={applicability} at={at} />
    </Block>
  )
}
