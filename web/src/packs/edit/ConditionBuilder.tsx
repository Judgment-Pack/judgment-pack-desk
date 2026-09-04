/**
 * A condition, edited as the tree it is.
 *
 * The five kinds are the schema's own and they recurse through `$ref`, so this
 * component draws itself: a group's children are conditions, a `not`'s child
 * is a condition, and the nesting has no depth limit because the schema
 * declares none.
 *
 * **It shapes and it never refuses.** The operand control changes with the
 * operator — the four ordered comparisons write a decimal *string*, `in` a
 * list, the two equalities any JSON — and an empty `in`, an unquoted `5000`
 * and an id nothing declares are all writable. `validate` names those, by
 * code, at the pointer, on the field. A form with an opinion the runtime does
 * not share would refuse the intermediate states every edit passes through.
 *
 * **A kind this desk has never seen is printed and not editable**, exactly as
 * the reading tree holds it. A runtime may grow a sixth kind; offering
 * controls for it would be editing a condition this desk does not understand.
 *
 * **Nothing here paraphrases.** `greater-than` stays the document's word and
 * `"5000"` keeps its quotes in the bytes; the operator is a Select over the
 * schema's own list, not "is greater than".
 *
 * One deviation from "every keystroke reaches the buffer", and it is here
 * rather than hidden: the operand controls that take **arbitrary JSON** hold
 * what is typed until it parses. Writing each intermediate keystroke would put
 * unscannable bytes in the buffer, which withholds form mode with a parse
 * error — so typing `{"a": 1}` would eject the author to raw mode at the first
 * character. Nothing is refused and nothing is corrected: the text stays on
 * screen, the field says it is not written yet, and it reaches the buffer
 * verbatim the moment it is JSON.
 */
import { useState, type ReactNode } from 'react'
import { Button } from '../../ui/Button'
import { Input } from '../../ui/Input'
import { Select } from '../../ui/Select'
import { SuggestInput } from '../../ui/SuggestInput'
import { TextArea } from '../../ui/TextArea'
import { Block } from '../document/Block'
import { valueAt } from '../pointers'
import {
  addChild,
  changeKind,
  conditionKind,
  removeNode,
  setOperator,
  wrapInGroup,
  type ConditionKind
} from './conditionOps'
import { useEditing } from './editingContext'
import { PointerField } from './PointerField'
import { DECIMAL_STRING, ENUMS, operandControl } from './shape'
import { bytesAt, setRawJson, type Buffered } from './writes'
import styles from './ConditionBuilder.module.css'

export function ConditionBuilder({ at }: { at: string }) {
  return (
    <div className={styles.tree}>
      <ConditionNode at={at} depth={0} removable={false} />
    </div>
  )
}

function ConditionNode({
  at,
  depth,
  removable
}: {
  at: string
  depth: number
  /** False for the member itself: taking that out is the card's edit, not the tree's. */
  removable: boolean
}) {
  const { buffer, write, ids } = useEditing()
  const node = valueAt(buffer.index.value, at)
  const kind = conditionKind(node)
  const [collapsed, setCollapsed] = useState(false)

  if (kind === 'other') {
    return (
      <Block pointer={at} as="div" className={styles.node}>
        <p className={styles.unknown}>
          <code>{JSON.stringify(node)}</code>
        </p>
        <p className={styles.unknownNote}>
          This desk has no controls for this condition. It is shown as the bytes it is, and the
          JSON view edits it.
        </p>
      </Block>
    )
  }

  const children = kind === 'all' || kind === 'any' ? childCount(node) : 0
  const label = `${kind}${kind === 'all' || kind === 'any' ? ' of' : ''} — ${at}`

  return (
    <Block pointer={at} as="div" className={styles.node}>
      <div role="group" aria-label={label} className={styles.frame} data-depth={depth}>
        <div className={styles.head}>
          <label className={styles.kindLabel} htmlFor={`${at}-kind`}>
            kind
          </label>
          <Select
            id={`${at}-kind`}
            value={kind}
            options={ENUMS.conditionOp.map((op) => ({ value: op, label: op }))}
            onValueChange={(next) => write((current) => changeKind(current, at, next))}
          />
          {(kind === 'all' || kind === 'any') && (
            <>
              <Button variant="quiet" onClick={() => write((current) => addChild(current, at))}>
                Add
              </Button>
              {depth > 0 && (
                <Button
                  variant="quiet"
                  aria-expanded={!collapsed}
                  onClick={() => setCollapsed((was) => !was)}
                >
                  {collapsed ? `collapsed · ${children} conditions` : 'Collapse'}
                </Button>
              )}
            </>
          )}
          <Button variant="quiet" onClick={() => write((current) => wrapInGroup(current, at, 'all'))}>
            Wrap
          </Button>
          {removable && (
            <Button variant="quiet" onClick={() => write((current) => removeNode(current, at))}>
              Remove
            </Button>
          )}
        </div>

        {kind === 'literal' && (
          <PointerField pointer={`${at}/value`} label="value">
            {(wiring) => (
              <Select
                {...wiring}
                value={literalText(valueAt(buffer.index.value, `${at}/value`))}
                placeholder="not declared"
                options={[
                  { value: 'true', label: 'true' },
                  { value: 'false', label: 'false' }
                ]}
                onValueChange={(next) =>
                  write((current) => setRawJson(current, `${at}/value`, next))
                }
              />
            )}
          </PointerField>
        )}

        {kind === 'evidence-present' && (
          <PointerField pointer={`${at}/evidenceRequirement`} label="evidence requirement">
            {(wiring) => (
              <Select
                {...wiring}
                value={stringAt(buffer, `${at}/evidenceRequirement`)}
                placeholder="not declared"
                options={withHeld(ids.evidence, stringAt(buffer, `${at}/evidenceRequirement`))}
                onValueChange={(next) =>
                  write((current) => setRawJson(current, `${at}/evidenceRequirement`, JSON.stringify(next)))
                }
              />
            )}
          </PointerField>
        )}

        {kind === 'fact' && <FactNode at={at} />}

        {kind === 'not' && (
          <div className={styles.children}>
            <ConditionNode at={`${at}/condition`} depth={depth + 1} removable />
          </div>
        )}

        {(kind === 'all' || kind === 'any') && !collapsed && (
          <div className={styles.children}>
            {children === 0 && (
              <p className={styles.empty}>
                This group holds no conditions. <em>Add</em> writes one.
              </p>
            )}
            {Array.from({ length: children }, (_, index) => (
              <ConditionNode
                key={index}
                at={`${at}/conditions/${index}`}
                depth={depth + 1}
                removable
              />
            ))}
          </div>
        )}
      </div>
    </Block>
  )
}

/**
 * The `fact` node: a path, an operator, and an operand whose control the
 * operator chooses.
 *
 * The path is a free-text input with the project's consulted paths as
 * suggestions and not as a list to choose from: a rule about a fact nothing
 * consults yet is the ordinary case for a new rule.
 */
function FactNode({ at }: { at: string }) {
  const { buffer, write, ids } = useEditing()
  const operator = stringAt(buffer, `${at}/operator`)
  const control = operandControl(operator)
  const path = stringAt(buffer, `${at}/path`)
  return (
    <div className={styles.fact}>
      <PointerField pointer={`${at}/path`} label="path">
        {(wiring) => (
          <SuggestInput
            {...wiring}
            suggestions={ids.factPaths}
            value={path}
            onChange={(event) =>
              write(
                (current) => setRawJson(current, `${at}/path`, JSON.stringify(event.target.value)),
                { coalesceKey: `${at}/path` }
              )
            }
          />
        )}
      </PointerField>
      <PointerField pointer={`${at}/operator`} label="operator">
        {(wiring) => (
          <Select
            {...wiring}
            value={operator}
            placeholder="not declared"
            options={withHeld(ENUMS.factOperator, operator)}
            onValueChange={(next) => write((current) => setOperator(current, at, next))}
          />
        )}
      </PointerField>
      {control === 'decimal' && <DecimalOperand at={`${at}/value`} />}
      {control === 'list' && (
        <JsonOperand
          at={`${at}/value`}
          label="value"
          rows={3}
          hint="a list. One entry is the fewest this operator reads."
        />
      )}
      {control === 'json' && (
        <JsonOperand at={`${at}/value`} label="value" rows={1} hint="any JSON value." />
      )}
    </div>
  )
}

/**
 * An ordered comparison's operand: written as a decimal **string**.
 *
 * `"5000"` and `5000` are the difference between a document the runtime
 * accepts and one it refuses by name, and this control writes the first. It
 * does not *convert* the second: a document already holding a number keeps it
 * until the author types, and until then the hint says what the document holds
 * and the check says what the runtime thinks of it.
 */
function DecimalOperand({ at }: { at: string }) {
  const { buffer, write } = useEditing()
  const held = valueAt(buffer.index.value, at)
  const raw = bytesAt(buffer, at)
  const shown = typeof held === 'string' ? held : (raw ?? '')
  const shaped = typeof held === 'string' && DECIMAL_STRING.test(held)
  return (
    <PointerField
      pointer={at}
      label="value"
      hint={
        shaped
          ? 'a decimal string.'
          : `a decimal string. The document holds ${raw ?? 'nothing'} here.`
      }
    >
      {(wiring) => (
        <Input
          {...wiring}
          value={shown}
          onChange={(event) =>
            write((current) => setRawJson(current, at, JSON.stringify(event.target.value)), {
              coalesceKey: at
            })
          }
        />
      )}
    </PointerField>
  )
}

/**
 * An operand that is arbitrary JSON, edited as the bytes it is.
 *
 * The document's own bytes are shown — not a re-serialization — so `5.0`,
 * `5` and `"5"` are three different things on screen, which is what they are
 * on disk. What is typed reaches the buffer verbatim the moment it parses;
 * until then it is held here and said to be unwritten, for the reason the
 * module doc gives.
 */
function JsonOperand({
  at,
  label,
  rows,
  hint
}: {
  at: string
  label: string
  rows: number
  hint: ReactNode
}) {
  const { buffer, write } = useEditing()
  const held = bytesAt(buffer, at) ?? ''
  const [draft, setDraft] = useState<{ text: string; from: string } | null>(null)
  // A draft is only about the bytes it started from. Anything that moves them
  // underneath it — undo, the JSON view, a kind change — retires it, rather
  // than leaving a stale word over a member it is no longer about.
  const pending = draft !== null && draft.from === held
  const shown = pending ? draft.text : held

  const change = (next: string) => {
    if (isJson(next)) {
      setDraft(null)
      write((current) => setRawJson(current, at, next), { coalesceKey: at })
      return
    }
    setDraft({ text: next, from: held })
  }

  return (
    <PointerField
      pointer={at}
      label={label}
      hint={
        pending ? (
          <>
            Not written yet — this is not JSON. The document still holds <code>{held}</code>.
          </>
        ) : (
          hint
        )
      }
    >
      {(wiring) =>
        rows > 1 ? (
          <TextArea {...wiring} rows={rows} value={shown} onChange={(event) => change(event.target.value)} />
        ) : (
          <Input {...wiring} value={shown} onChange={(event) => change(event.target.value)} />
        )
      }
    </PointerField>
  )
}

function isJson(text: string): boolean {
  try {
    JSON.parse(text)
    return true
  } catch {
    return false
  }
}

function stringAt(buffer: Buffered, pointer: string): string {
  const value = valueAt(buffer.index.value, pointer)
  return typeof value === 'string' ? value : ''
}

function literalText(value: unknown): string {
  return value === true ? 'true' : value === false ? 'false' : ''
}

/** How many conditions a group carries, without asserting the member is an array. */
function childCount(node: unknown): number {
  if (typeof node !== 'object' || node === null) return 0
  const conditions = (node as { conditions?: unknown }).conditions
  return Array.isArray(conditions) ? conditions.length : 0
}

/**
 * The schema's list, plus whatever the document already holds.
 *
 * Radix refuses a value it was never offered — it blanks the trigger and
 * reports the empty string straight back — so a word from a later spec, or an
 * id nothing declares, would be silently replaced by nothing on first render.
 */
function withHeld(
  offered: readonly string[],
  held: string
): { value: string; label: string }[] {
  const rows = offered.map((word) => ({ value: word, label: word }))
  if (held !== '' && !offered.includes(held)) rows.unshift({ value: held, label: held })
  return rows
}

export type { ConditionKind }
