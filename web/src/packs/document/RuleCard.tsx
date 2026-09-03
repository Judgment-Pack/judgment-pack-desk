/**
 * One rule, in the document's own order.
 *
 * Order is §7-significant, so the card carries its index as well as its id:
 * "the second rule" is a fact about this document and `/rules/1` is the
 * address that says it.
 */
import type { Rule } from '../../mcp/types'
import { Block } from './Block'
import { ConditionTree } from './ConditionTree'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'

export function RuleCard({ rule, at }: { rule: Rule; at: string }) {
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
