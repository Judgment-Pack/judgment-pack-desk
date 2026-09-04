/**
 * A rule and an exception, as the forms they become in place — and the
 * ordering that is part of what they say.
 *
 * **Order is §7-significant.** The first matching rule is the one that fires,
 * so moving a rule is an edit to what the pack decides and not a tidy. It
 * moves by keyboard: two buttons on each card and `Alt+ArrowUp` /
 * `Alt+ArrowDown` inside it. Not drag — a drag surface has no keyboard story
 * that is not a second implementation, and this one is the whole story.
 *
 * **Every `/rules/N` pointer past the move is now a different rule.** `?at`,
 * the Inspector's subtree, and every anchored diagnostic are addressed by
 * those pointers, so the caller marks the check stale after a move: a real
 * diagnostic re-anchored across a reorder lands on a rule it is not about,
 * and that reads as an answer.
 *
 * **`onUnknown` is rendered whether or not the document carries it.** It is
 * required on both a rule and an exception, and it is the member most drafts
 * of an exception omit; the runtime reports the absence at that pointer
 * *including the absent name*, so the field is there for the diagnostic to
 * land on and for the author to fill.
 */
import { useEffect, useState, type KeyboardEvent, type ReactNode } from 'react'
import { Button } from '../../ui/Button'
import { valueAt } from '../pointers'
import { ConditionBuilder } from './ConditionBuilder'
import { NEW_NODE } from './conditionOps'
import { useEditing } from './editingContext'
import { EnumField, IdRefField, StringField, StringListField, TextField } from './fields'
import { ENUMS, LOCAL_ID } from './shape'
import { moveRule, setRawJson } from './writes'
import styles from './CardForm.module.css'

const ID_HINT = `a local id — ${LOCAL_ID.source}`

/** One rule's card, as its form. */
export function RuleForm({ at }: { at: string }) {
  const { ids } = useEditing()
  return (
    <div className={styles.form}>
      <StringField pointer={`${at}/id`} label="id" hint={ID_HINT} />
      <TextField pointer={`${at}/description`} label="description" />
      <WhenField at={at} />
      <div className={styles.row}>
        <IdRefField pointer={`${at}/outcome`} label="outcome" ids={ids.outcomes} />
        <EnumField pointer={`${at}/onUnknown`} label="on unknown" options={ENUMS.onUnknown} />
      </div>
      <StringListField
        pointer={`${at}/evidenceRequirementRefs`}
        label="evidence"
        candidates={ids.evidence}
      />
      <StringListField pointer={`${at}/sourceRefs`} label="sources" candidates={ids.sources} />
      <TextField pointer={`${at}/rationale`} label="rationale" />
    </div>
  )
}

/** One exception's card, as its form. */
export function ExceptionForm({ at }: { at: string }) {
  const { ids } = useEditing()
  return (
    <div className={styles.form}>
      <StringField pointer={`${at}/id`} label="id" hint={ID_HINT} />
      <TextField pointer={`${at}/description`} label="description" />
      <WhenField at={at} />
      <div className={styles.row}>
        <EnumField pointer={`${at}/effect`} label="effect" options={ENUMS.effect} />
        <IdRefField pointer={`${at}/targetRule`} label="target rule" ids={ids.rules} optional />
        <IdRefField pointer={`${at}/outcome`} label="outcome" ids={ids.outcomes} optional />
      </div>
      <EnumField pointer={`${at}/onUnknown`} label="on unknown" options={ENUMS.onUnknown} />
      <StringListField pointer={`${at}/sourceRefs`} label="sources" candidates={ids.sources} />
    </div>
  )
}

/**
 * The condition, or the line saying there is not one.
 *
 * A `when` that is absent is a required member the runtime refuses at this
 * pointer. The line states the absence and offers to write one, rather than
 * drawing an empty builder that would look like a condition that matches
 * nothing.
 */
function WhenField({ at }: { at: string }) {
  const { buffer, write } = useEditing()
  const present = valueAt(buffer.index.value, `${at}/when`) !== undefined
  if (present) {
    return (
      <div className={styles.when}>
        <p className={styles.whenLabel}>when</p>
        <ConditionBuilder at={`${at}/when`} />
      </div>
    )
  }
  return (
    <div className={styles.when} id={`${at}/when`} data-pointer={`${at}/when`}>
      <p className={styles.whenLabel}>when</p>
      <p className={styles.absent}>
        <span className={styles.absentTag}>not declared</span>
        <Button
          variant="quiet"
          onClick={() =>
            write((current) => setRawJson(current, `${at}/when`, JSON.stringify(NEW_NODE)))
          }
        >
          Write a condition
        </Button>
      </p>
    </div>
  )
}

/**
 * Where a card sits in its array, and how it moves.
 *
 * The state lives above the cards because the cards do not survive a move:
 * a list keyed by id remounts both of the two that exchanged places, so a
 * "just moved" flag held inside one would be thrown away by the move it is
 * about. Focus follows the card to its new address and the announcement names
 * the position it landed in — a reorder with neither is a document that
 * changed with nothing to say it did.
 */
export function useCardOrder(
  arrayPointer: string,
  count: number
): {
  move: (from: number, to: number) => void
  announcement: string
  onCardKey: (index: number) => (event: KeyboardEvent<HTMLElement>) => void
} {
  const { write } = useEditing()
  const [landed, setLanded] = useState<{ at: number; of: number; nonce: number } | null>(null)

  useEffect(() => {
    if (landed === null) return
    // The pointer is the element id, as it is everywhere else in this desk.
    document.getElementById(`${arrayPointer}/${landed.at}`)?.focus()
  }, [landed, arrayPointer])

  const move = (from: number, to: number) => {
    if (to < 0 || to >= count || from === to) return
    write((current) => moveRule(current, arrayPointer, from, to))
    setLanded((previous) => ({ at: to, of: count, nonce: (previous?.nonce ?? 0) + 1 }))
  }

  const onCardKey = (index: number) => (event: KeyboardEvent<HTMLElement>) => {
    if (!event.altKey) return
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      event.stopPropagation()
      move(index, index - 1)
      return
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      event.stopPropagation()
      move(index, index + 1)
    }
  }

  return {
    move,
    announcement: landed === null ? '' : `Moved to position ${landed.at + 1} of ${landed.of}.`,
    onCardKey
  }
}

/** The two buttons, and the position they are about. */
export function MoveControls({
  index,
  count,
  move,
  what
}: {
  index: number
  count: number
  move: (from: number, to: number) => void
  /** The word for one of these — "rule", "exception". */
  what: string
}) {
  return (
    <span className={styles.moves}>
      <span className={styles.position}>
        {index + 1} of {count}
      </span>
      <Button
        variant="quiet"
        disabled={index === 0}
        aria-label={`Move this ${what} up`}
        onClick={() => move(index, index - 1)}
      >
        Move up
      </Button>
      <Button
        variant="quiet"
        disabled={index === count - 1}
        aria-label={`Move this ${what} down`}
        onClick={() => move(index, index + 1)}
      >
        Move down
      </Button>
    </span>
  )
}

/** The live region the move speaks through, rendered once per list. */
export function OrderAnnouncement({ text }: { text: string }) {
  return (
    <p role="status" aria-live="polite" className={styles.announcement}>
      {text}
    </p>
  )
}

export type { ReactNode }
