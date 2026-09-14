/**
 * A condition, rendered as an indented tree with shared operator labels.
 *
 * `"5000"` keeps its quotes, because a decimal operand is a *string* and the
 * difference between `"5000"` and `5000` is the difference between a document
 * the runtime accepts and one it refuses by name. `greater-than` stays the
 * exact value in the document. Display labels never change operand types,
 * author values or the machine representation available in the Inspector.
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
import { createContext, useContext, type ReactNode } from 'react'
import type { Condition } from '../../mcp/types'
import { conditionKind } from '../edit/conditionOps'
import { Block } from './Block'
import { ReadOnlyBlocks } from './Block'
import { valueLabel } from '../terminology'
import styles from './PackDocument.module.css'
import reading from './ConditionTree.module.css'

const ArrayLayout = createContext<'stacked' | 'inline'>('stacked')
const Structured = createContext(false)
export function ConditionTree({ condition, at, readOnly = false, arrayLayout = 'stacked', structured = false }: { condition: unknown; at: string; readOnly?: boolean; arrayLayout?: 'stacked' | 'inline'; structured?: boolean }) {
  return (
    <div className={structured ? reading.tree : styles.tree} data-condition-tree={structured ? 'structured' : 'document'}>
      <ReadOnlyBlocks.Provider value={readOnly}>
        <Structured.Provider value={structured}><ArrayLayout.Provider value={arrayLayout}><ConditionNode condition={condition} at={at} depth={0} /></ArrayLayout.Provider></Structured.Provider>
      </ReadOnlyBlocks.Provider>
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
  const structured = useContext(Structured)
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

  if (structured && (kind === 'all' || kind === 'any' || kind === 'not')) {
    const children = kind === 'not' ? [node.condition] : Array.isArray(node.conditions) ? node.conditions : []
    return <Block pointer={at} as="div" className={reading.group}>
      <span className={reading.groupLabel}>{valueLabel('op', kind)}</span>
      <div className={reading.children}>{children.map((child, index) => <ConditionNode key={index} condition={child}
        at={kind === 'not' ? `${at}/condition` : `${at}/conditions/${index}`} depth={depth + 1} />)}
        {!children.length && <span className={reading.comparison}>{Array.isArray(node.conditions) ? 'No conditions declared' : JSON.stringify(node.conditions) ?? 'Conditions not declared'}</span>}
      </div>
    </Block>
  }

  if (kind === 'all' || kind === 'any') {
    const children = Array.isArray(node.conditions) ? node.conditions : []
    return (
      <>
        <Row at={at} depth={depth}>
          <span className={styles.op}>{valueLabel('op', node.op)}</span>
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
          <span className={styles.op}>{valueLabel('op', 'not')}</span>
        </Row>
        <ConditionNode condition={node.condition} at={`${at}/condition`} depth={depth + 1} />
      </>
    )
  }

  if (kind === 'fact') {
    if (structured) return <Block pointer={at} as="div" className={reading.fact}>
      <div className={reading.field}>
        <span>{factLabel(String(node.path ?? ''))}</span>
        <Block pointer={`${at}/path`} as="code" className={reading.path}>{String(node.path ?? '')}</Block>
      </div>
      <div className={reading.test}>
        <Block pointer={`${at}/operator`} as="span" className={reading.comparison}>{valueLabel('operator', String(node.operator ?? ''))}</Block>
        <Block pointer={`${at}/value`} as="div" className={reading.operand}><Operand value={node.value} /></Block>
      </div>
    </Block>
    return (
      <Row at={at} depth={depth}>
        <Block pointer={`${at}/path`} as="code" className={styles.factPath}>
          {String(node.path ?? '')}
        </Block>{' '}
        <Block pointer={`${at}/operator`} as="span" className={styles.op}>
          {valueLabel('operator', String(node.operator ?? ''))}
        </Block>{' '}
        <Block pointer={`${at}/value`} as="code" className={styles.literal}>
          <Operand value={node.value} />
        </Block>
      </Row>
    )
  }

  if (kind === 'evidence-present') {
    if (structured) return <Block pointer={at} as="div" className={reading.fact}>
      <span className={reading.comparison}>{valueLabel('op', kind)}</span>
      <Block pointer={`${at}/evidenceRequirement`} as="code" className={reading.operand}>{String(node.evidenceRequirement ?? '')}</Block>
    </Block>
    return (
      <Row at={at} depth={depth}>
        <span className={styles.op}>{valueLabel('op', 'evidence-present')}</span>{' '}
        <Block pointer={`${at}/evidenceRequirement`} as="code" className={styles.literal}>
          {String(node.evidenceRequirement ?? '')}
        </Block>
      </Row>
    )
  }

  if (kind === 'literal') {
    return (
      <Row at={at} depth={depth}>
        <span className={styles.op}>{valueLabel('op', 'literal')}</span>{' '}
        <Block pointer={`${at}/value`} as="code" className={styles.literal}>
          <Operand value={node.value} />
        </Block>
      </Row>
    )
  }

  return null
}

/** Break between complete array entries first. Quotes, types and order stay exact. */
function Operand({ value }: { value: unknown }) {
  const layout = useContext(ArrayLayout)
  const structured = useContext(Structured)
  if (structured && Array.isArray(value)) return <>
    <span className={reading.count}>{value.length} {value.length === 1 ? 'value' : 'values'}</span>
    {value.length ? <span className={reading.values} role="list" aria-label="Exact values">{value.map((entry, index) =>
      <span role="listitem" key={index}><code>{JSON.stringify(entry)}</code></span>)}</span> : <code>[]</code>}
  </>
  if (!Array.isArray(value)) return <>{JSON.stringify(value) ?? (structured ? 'Value not declared' : undefined)}</>
  const expanded = layout === 'stacked' && (value.length > 3 || JSON.stringify(value).length > 80)
  return <span className={expanded ? styles.arrayExpanded : undefined}>[
    {value.map((entry, index) => <span className={styles.arrayEntry} key={index}>
      {JSON.stringify(entry)}{index < value.length - 1 ? ',' : ''}<wbr />
    </span>)}
  ]</span>
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
  const structured = useContext(Structured)
  return (
    <Block pointer={at} as="div" className={structured ? reading.row : styles.treeRow}>
      <span className={styles.indent} aria-hidden="true">
        {'  '.repeat(depth)}
      </span>
      <span className={styles.treeContent}>{children}</span>
    </Block>
  )
}

/** Mechanical display label only; the complete, exact pointer stays visible. */
function factLabel(path: string): string {
  const words = path.replace(/^\//, '').split('/').map(part => part.replace(/~1/g, '/').replace(/~0/g, '~'))
    .join(' ').replace(/([a-z0-9])([A-Z])/g, (_match, before: string, after: string) => `${before} ${after.toLowerCase()}`).replace(/[-_]/g, ' ')
  return words ? words[0]!.toUpperCase() + words.slice(1) : 'Fact'
}
