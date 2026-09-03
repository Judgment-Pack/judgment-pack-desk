/** One exception: a rule's card plus the effect and the rule it targets. */
import type { Exception } from '../../mcp/types'
import { Block } from './Block'
import { ConditionTree } from './ConditionTree'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'

export function ExceptionCard({ exception, at }: { exception: Exception; at: string }) {
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
