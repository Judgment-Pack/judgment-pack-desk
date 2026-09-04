/**
 * A condition, rendered as an indented tree and **never paraphrased**.
 *
 * `"5000"` keeps its quotes, because a decimal operand is a *string* and the
 * difference between `"5000"` and `5000` is the difference between a document
 * the runtime accepts and one it refuses by name. `greater-than` stays the
 * document's own word, because "is greater than" is English about what the
 * policy means and only the document says that. A view that paraphrased would
 * be authoring a second, unversioned statement of the rule.
 *
 * The five node kinds are the schema's own (`$defs/condition`): `literal`,
 * `all`/`any`, `not`, `fact`, `evidence-present`, recursing through `$ref`. A
 * node this desk does not recognise is printed as the JSON it is rather than
 * dropped — a runtime may grow a sixth, and a view that silently skipped it
 * would show a condition that is not the one on disk.
 *
 * Every node carries its own pointer, down to the operand, so
 * `/rules/1/when/conditions/0/value` is a real element: a diagnostic anchors
 * on it, a deep link reaches it, and the phase-2 form field is already
 * addressed by it.
 */
import type { ReactNode } from 'react'
import type { Condition } from '../../mcp/types'
import { conditionKind } from '../edit/conditionOps'
import { Block } from './Block'
import styles from './PackDocument.module.css'

export function ConditionTree({ condition, at }: { condition: unknown; at: string }) {
  return (
    <div className={styles.tree}>
      <ConditionNode condition={condition} at={at} depth={0} />
    </div>
  )
}

function ConditionNode({
  condition,
  at,
  depth
}: {
  condition: unknown
  at: string
  depth: number
}) {
  // **The one discrimination.** `edit/conditionOps.ts` decides what kind a node
  // is, and the builder reads it from there too: two spellings of "what makes a
  // node a `fact`" is a tree that draws one thing while the form edits another,
  // which for a condition is the difference between the policy on screen and
  // the policy on disk.
  const kind = conditionKind(condition)
  if (kind === 'other') {
    // A node this desk has never seen — or a value that is not a node at all.
    // Printed, not dropped.
    return (
      <Row at={at} depth={depth}>
        <code className={styles.literal}>{JSON.stringify(condition)}</code>
      </Row>
    )
  }
  const node = condition as Condition

  if (kind === 'all' || kind === 'any') {
    const children = Array.isArray(node.conditions) ? node.conditions : []
    return (
      <>
        <Row at={at} depth={depth}>
          <span className={styles.op}>{node.op} of</span>
        </Row>
        {children.map((child, index) => (
          <ConditionNode
            key={index}
            condition={child}
            at={`${at}/conditions/${index}`}
            depth={depth + 1}
          />
        ))}
      </>
    )
  }

  if (kind === 'not') {
    return (
      <>
        <Row at={at} depth={depth}>
          <span className={styles.op}>not</span>
        </Row>
        <ConditionNode condition={node.condition} at={`${at}/condition`} depth={depth + 1} />
      </>
    )
  }

  if (kind === 'fact') {
    return (
      <Row at={at} depth={depth}>
        <Block pointer={`${at}/path`} as="code" className={styles.factPath}>
          {String(node.path ?? '')}
        </Block>{' '}
        <Block pointer={`${at}/operator`} as="span" className={styles.op}>
          {String(node.operator ?? '')}
        </Block>{' '}
        <Block pointer={`${at}/value`} as="code" className={styles.literal}>
          {JSON.stringify(node.value)}
        </Block>
      </Row>
    )
  }

  if (kind === 'evidence-present') {
    return (
      <Row at={at} depth={depth}>
        <span className={styles.op}>evidence-present</span>{' '}
        <Block pointer={`${at}/evidenceRequirement`} as="code" className={styles.literal}>
          {String(node.evidenceRequirement ?? '')}
        </Block>
      </Row>
    )
  }

  if (kind === 'literal') {
    return (
      <Row at={at} depth={depth}>
        <span className={styles.op}>literal</span>{' '}
        <Block pointer={`${at}/value`} as="code" className={styles.literal}>
          {JSON.stringify(node.value)}
        </Block>
      </Row>
    )
  }

  return null
}

function Row({
  at,
  depth,
  children
}: {
  at: string
  depth: number
  children: ReactNode
}) {
  return (
    <Block pointer={at} as="div" className={styles.treeRow}>
      <span className={styles.indent} aria-hidden="true">
        {'  '.repeat(depth)}
      </span>
      {children}
    </Block>
  )
}
