/**
 * A member whose bytes are not the shape this page draws.
 *
 * **Not a verdict, and not JPS validation.** `validate` is what says whether a
 * document is a pack; this is the narrower question of whether *these controls*
 * can be pointed at these bytes. `"rules": {}` is valid JSON and a document
 * somebody can have on disk — the desk can write it, and after finding 4 the
 * page draws the buffer whatever it says — and the reading view reached
 * `rules.map` and took the whole route down with it. A form field over
 * `"locator": null` was worse than a crash: it drew, took a keystroke, and
 * wrote nothing, because the write has no container to splice into.
 *
 * So the member is named, its pointer is printed, its bytes are shown, and the
 * JSON view is named as the place they can be fixed. The block carries the
 * member's own pointer like every other block, so a diagnostic about it still
 * anchors here and the outline entry still reaches it.
 */
import type { ReactNode } from 'react'
import { Block } from './Block'
import styles from './PackDocument.module.css'

export function MisshapenMember({
  pointer,
  label,
  expected,
  value,
  compact
}: {
  pointer: string
  /** What the document calls this member. */
  label: string
  /** The shape the controls need — "a list", "an object", "a string". */
  expected: string
  /** The bytes as they are, printed rather than described. */
  value: unknown
  /** Inside a card or a field, where a heading would be the wrong element. */
  compact?: boolean
}) {
  return (
    <Block pointer={pointer}>
      {compact === true ? (
        <p className={styles.fieldLabel}>{label}</p>
      ) : (
        <h2 className={styles.heading}>{label}</h2>
      )}
      <p className={styles.note}>
        This member is not the shape this page draws: <code>{pointer}</code> holds{' '}
        {describe(value)} and this page draws {expected}. Its bytes are below and in the JSON
        view, which is where they can be changed.
      </p>
      <pre className={styles.raw}>
        <code>{JSON.stringify(value, null, 2) ?? String(value)}</code>
      </pre>
    </Block>
  )
}

/** What is actually there, in a word. */
export function describe(value: unknown): string {
  if (value === null) return 'null'
  if (Array.isArray(value)) return 'a list'
  switch (typeof value) {
    case 'object':
      return 'an object'
    case 'string':
      return 'a string'
    case 'number':
      return 'a number'
    case 'boolean':
      return 'a true/false value'
    default:
      return 'nothing this page can read'
  }
}

/** Whether these bytes are an object the form can point controls at. */
export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/**
 * One nested member, drawn where it is the shape its controls need and stated
 * where it is not.
 *
 * The root members have `MEMBER_SHAPE`; this is the same question one level in,
 * at the places a component reaches through a member to read what is inside it.
 * `"locator": null` is valid JSON, and `source.locator.value` is a crash.
 */
export function Shaped({
  pointer,
  label,
  expects,
  value,
  children
}: {
  pointer: string
  label: string
  expects: 'object' | 'list'
  value: unknown
  children: ReactNode
}) {
  const right = expects === 'list' ? Array.isArray(value) : isRecord(value)
  if (right) return <>{children}</>
  return (
    <MisshapenMember
      pointer={pointer}
      label={label}
      expected={expects === 'list' ? 'a list' : 'an object'}
      value={value}
      compact
    />
  )
}
