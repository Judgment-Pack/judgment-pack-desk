/**
 * The controls a member's card becomes, one per shape the schema declares.
 *
 * Each one is a `PointerField` — so it keeps the block's address and prints
 * the diagnostics about it — wrapped around a `ui/` primitive. Nothing here
 * knows what a save is, what a buffer is, or what the document means: it reads
 * the value at its pointer, and it hands `writes.ts` an edit.
 *
 * **Typing is coalesced per field and nothing else is.** `commit` takes the
 * pointer as its coalesce key, so a sentence typed into a description is one
 * undo entry and moving to the next field starts another. A Select, a
 * checkbox and a reorder are discrete actions and each pushes its own entry:
 * they are the gestures a viewer would expect Undo to take back one at a time.
 *
 * **Nothing here refuses a value.** An id that does not match `localId`, an
 * empty `in` list, a number where the schema asks for a decimal string — all
 * of them are written, and `validate` is what names them, at the pointer, on
 * the field. A form that refused would be a second validator with an opinion
 * the runtime does not share, and an author mid-edit would be unable to type
 * the intermediate states every edit passes through.
 */
import type { ReactNode } from 'react'
import { Button } from '../../ui/Button'
import { Input } from '../../ui/Input'
import { SegmentedControl } from '../../ui/SegmentedControl'
import { Select } from '../../ui/Select'
import { TextArea } from '../../ui/TextArea'
import { elementIdFor, valueAt } from '../pointers'
import { useEditing } from './editingContext'
import { PointerField } from './PointerField'
import { starterFor } from './shape'
import { setBoolean, setEnum, setRawJson, setString, setStringList } from './writes'
import styles from './PointerField.module.css'

/**
 * What a Select offers for "the document does not say".
 *
 * Not the empty string. `ui/Select` drops a value it never offered, because
 * Radix reports `""` straight back through `onValueChange` when a controlled
 * value changes while the list is closed — and that guard is written down as
 * safe precisely because no option is ever `""`. An optional member's blank
 * option would have broken that invariant from the outside, so the blank
 * carries a value nothing else can spell and it is mapped back here.
 */
const NOT_DECLARED = '\u0000not-declared'

/** The string a control shows for a member the document may not declare. */
function stringAt(document: unknown, pointer: string): string {
  const value = valueAt(document, pointer)
  return typeof value === 'string' ? value : ''
}

function listAt(document: unknown, pointer: string): string[] {
  const value = valueAt(document, pointer)
  if (!Array.isArray(value)) return []
  return value.filter((entry): entry is string => typeof entry === 'string')
}

export function StringField({
  pointer,
  label,
  hint
}: {
  pointer: string
  label: string
  hint?: ReactNode
}) {
  const { buffer, write } = useEditing()
  const value = stringAt(buffer.index.value, pointer)
  return (
    <PointerField pointer={pointer} label={label} hint={hint}>
      {(wiring) => (
        <Input
          {...wiring}
          value={value}
          onChange={(event) =>
            write((current) => setString(current, pointer, event.target.value), {
              coalesceKey: pointer
            })
          }
        />
      )}
    </PointerField>
  )
}

export function TextField({
  pointer,
  label,
  hint,
  rows = 3
}: {
  pointer: string
  label: string
  hint?: ReactNode
  rows?: number
}) {
  const { buffer, write } = useEditing()
  const value = stringAt(buffer.index.value, pointer)
  return (
    <PointerField pointer={pointer} label={label} hint={hint}>
      {(wiring) => (
        <TextArea
          {...wiring}
          rows={rows}
          value={value}
          onChange={(event) =>
            write((current) => setString(current, pointer, event.target.value), {
              coalesceKey: pointer
            })
          }
        />
      )}
    </PointerField>
  )
}

/**
 * One of a closed list, or none.
 *
 * The blank option is offered only where the member is optional, and it is
 * spelled "not declared" rather than left empty: an option with no words is
 * one a reader cannot tell from a control that has not loaded. Choosing it
 * removes the member, which is what "the document does not say" is.
 *
 * A value the document carries that is **not** in the schema's list is added
 * to the options rather than dropped. Radix refuses a value it was never
 * offered — it would blank the trigger and report the empty string straight
 * back — so a document written against a later spec would have had its own
 * word silently replaced by nothing on first render.
 */
export function EnumField({
  pointer,
  label,
  options,
  optional,
  hint
}: {
  pointer: string
  label: string
  options: readonly string[]
  /** True where the member may be absent, which offers the blank. */
  optional?: boolean
  hint?: ReactNode
}) {
  const { buffer, write } = useEditing()
  const value = stringAt(buffer.index.value, pointer)
  const declared = value !== '' && !options.includes(value) ? [value] : []
  return (
    <PointerField pointer={pointer} label={label} hint={hint}>
      {(wiring) => (
        <Select
          {...wiring}
          value={value}
          placeholder="not declared"
          options={[
            ...(optional === true ? [{ value: NOT_DECLARED, label: 'not declared' }] : []),
            ...declared.map((word) => ({ value: word, label: word })),
            ...options.map((word) => ({ value: word, label: word }))
          ]}
          onValueChange={(next) =>
            write((current) => setEnum(current, pointer, next === NOT_DECLARED ? '' : next))
          }
        />
      )}
    </PointerField>
  )
}

/**
 * A reference to an id the document itself declares.
 *
 * The options are the declared ids, plus whatever the member already holds:
 * a rule whose `outcome` names an outcome nobody declared is a document the
 * runtime refuses at that pointer, and a Select that quietly dropped the
 * dangling value would repair the document out from under the diagnostic
 * pointing at it.
 */
export function IdRefField({
  pointer,
  label,
  ids,
  optional,
  hint
}: {
  pointer: string
  label: string
  ids: readonly string[]
  optional?: boolean
  hint?: ReactNode
}) {
  const { buffer, write } = useEditing()
  const value = stringAt(buffer.index.value, pointer)
  const dangling = value !== '' && !ids.includes(value) ? [value] : []
  return (
    <PointerField pointer={pointer} label={label} hint={hint}>
      {(wiring) => (
        <Select
          {...wiring}
          value={value}
          placeholder="not declared"
          options={[
            ...(optional === true ? [{ value: NOT_DECLARED, label: 'not declared' }] : []),
            ...ids.map((id) => ({ value: id, label: id })),
            ...dangling.map((id) => ({ value: id, label: `${id} — not declared here` }))
          ]}
          onValueChange={(next) =>
            write((current) => setEnum(current, pointer, next === NOT_DECLARED ? '' : next))
          }
        />
      )}
    </PointerField>
  )
}

/**
 * A list of words out of a candidate set — the reference lists, and the
 * escalation triggers.
 *
 * Checkboxes over the candidates rather than a text field, because every one
 * of these lists is drawn from something the document already declares. The
 * candidates are the declared ids **union whatever the member already holds**,
 * so a reference to an id that is not declared is on screen, checked, and can
 * be taken off — rather than disappearing from a form that would then write it
 * away on the next edit.
 *
 * An emptied list is written as `[]` and the member is kept. That is the
 * author saying the rule cites nothing, and where the schema asks for
 * `minItems: 1` the runtime names it by code at this pointer.
 */
export function StringListField({
  pointer,
  label,
  candidates,
  hint
}: {
  pointer: string
  label: string
  candidates: readonly string[]
  hint?: ReactNode
}) {
  const { buffer, write } = useEditing()
  const chosen = listAt(buffer.index.value, pointer)
  const offered = [...candidates, ...chosen.filter((entry) => !candidates.includes(entry))]
  return (
    <PointerField pointer={pointer} label={label} hint={hint}>
      {(wiring) => (
        <div className={styles.list} id={wiring.id} aria-describedby={wiring['aria-describedby']}>
          {offered.length === 0 && (
            <p className={styles.listEmpty}>The document declares nothing to reference here.</p>
          )}
          {offered.map((entry) => (
            <label className={styles.listRow} key={entry}>
              <input
                type="checkbox"
                checked={chosen.includes(entry)}
                onChange={(event) => {
                  const next = event.target.checked
                    ? [...chosen, entry]
                    : chosen.filter((held) => held !== entry)
                  write((current) => setStringList(current, pointer, next))
                }}
              />
              <span>{entry}</span>
            </label>
          ))}
        </div>
      )}
    </PointerField>
  )
}

/** A boolean member, offered as the two words the document would spell. */
export function BooleanField({
  pointer,
  label,
  hint
}: {
  pointer: string
  label: string
  hint?: ReactNode
}) {
  const { buffer, write } = useEditing()
  const value = valueAt(buffer.index.value, pointer)
  return (
    <PointerField pointer={pointer} label={label} hint={hint}>
      {(wiring) => (
        <div id={wiring.id} aria-describedby={wiring['aria-describedby']}>
          <SegmentedControl
            label={label}
            value={value === true ? 'true' : value === false ? 'false' : ''}
            onValueChange={(next) =>
              write((current) => setBoolean(current, pointer, next === 'true'))
            }
            segments={[
              { value: 'true', label: 'true' },
              { value: 'false', label: 'false' }
            ]}
          />
        </div>
      )}
    </PointerField>
  )
}

/**
 * A member that is an **object**, or the line saying the document has none.
 *
 * A field whose container is absent has nothing to splice into: `writes.place`
 * inserts a missing member but bails where the container itself has no span,
 * because inventing the object around it would write members the author never
 * asked for. Drawn unconditionally, those fields took a keystroke and moved no
 * bytes, with nothing on screen saying so — and `source.locator` and
 * `escalation.target` are both `required`, so a draft missing one is exactly
 * what an author opens the editor to fix.
 *
 * So the absence is stated and offered, which is `CardForm`'s `when` rule for
 * the one composite member that already had it. What the button writes is the
 * schema's own required members, empty, from `shape.STARTERS`.
 */
export function AbsentObject({
  pointer,
  label,
  what,
  children
}: {
  pointer: string
  label: string
  /** The words after "Write" — "a locator", "a target". */
  what: string
  children: ReactNode
}) {
  const { buffer, write } = useEditing()
  const held = valueAt(buffer.index.value, pointer)
  if (held !== undefined && (typeof held !== 'object' || held === null || Array.isArray(held))) {
    // **Present, and not an object.** `writes.place` splices into a container
    // and there is none here, so every field below took a keystroke and moved
    // no bytes with nothing on screen saying so. The bytes are named at their
    // own pointer and left alone: replacing them would be this form deciding
    // that what the author wrote was a mistake.
    return (
      <div
        id={elementIdFor(pointer)}
        data-pointer={pointer}
        tabIndex={-1}
        className={styles.absentGroup}
      >
        <p className={styles.absentLabel}>{label}</p>
        <p className={styles.absentLine}>
          <span className={styles.absentTag}>not the shape this form edits</span>
          <code>{JSON.stringify(held)}</code>
        </p>
        <p className={styles.absentLine}>
          The JSON view holds these bytes, which is where they can be changed.
        </p>
      </div>
    )
  }
  if (held !== undefined) return <>{children}</>
  const starter = starterFor(pointer)
  return (
    <div
      id={elementIdFor(pointer)}
      data-pointer={pointer}
      tabIndex={-1}
      className={styles.absentGroup}
    >
      <p className={styles.absentLabel}>{label}</p>
      <p className={styles.absentLine}>
        <span className={styles.absentTag}>not declared</span>
        {starter !== undefined && (
          <Button
            variant="quiet"
            onClick={() => write((current) => setRawJson(current, pointer, starter))}
          >
            Write {what}
          </Button>
        )}
      </p>
    </div>
  )
}
