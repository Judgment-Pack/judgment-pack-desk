/**
 * The condition that says where this pack decides at all.
 *
 * Rendered as the same unparaphrased tree a rule's `when` gets, because it is
 * the same kind of object and reading it should not depend on where it sits.
 *
 * **The tree carries `/applicability`, and nothing else does.** This used to
 * wrap the tree in a `Block` at the same pointer the tree's own root row
 * carries, so a document declaring `applicability` rendered two elements with
 * that `id` and, when it was selected, two with `aria-current="true"`:
 * duplicate ids, a doubled announcement, and a `getElementById` answering by
 * tree order. `RuleCard` never had this because it labels its condition with a
 * plain paragraph and roots the tree at `/rules/N/when`; this does the same.
 */
import type { Condition } from '../../mcp/types'
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
    <section>
      <h2 className={styles.heading}>Applicability</h2>
      <p className={styles.note}>The pack decides only where this holds.</p>
      <ConditionTree condition={applicability} at={at} />
    </section>
  )
}
