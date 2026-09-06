/**
 * The proposal, drawn as what it would do to the draft.
 *
 * One entry per top-level member, and one per element of an array member that
 * moved: the pointer it is about, what the draft holds there, and what the
 * proposal holds. The two are stacked rather than side by side because this
 * pane is 384px wide and a column of forty characters is not a diff anybody
 * reads.
 *
 * **The members that did not move collapse to one line with a count.** They are
 * the majority of any real proposal, and a list of eleven unchanged members
 * above the two that changed buries the answer. The line names them, so
 * "unchanged" is a statement about members a reader can check rather than a
 * number.
 *
 * Nothing here computes anything: `proposalDiff.ts` does, and this renders what
 * it returned.
 */
import { CodeArea } from '../ui/CodeArea'
import styles from './AssistantPane.module.css'
import type { DiffEntry, ProposalDiff } from './proposalDiff'

/** The word each status is announced by, in the desk's own plain English. */
const SAYS: Record<DiffEntry['status'], string> = {
  added: 'added',
  removed: 'removed',
  changed: 'changed',
  unchanged: 'unchanged'
}

export function ProposalDiffView({
  diff,
  onBaseline = true
}: {
  diff: ProposalDiff
  /**
   * Whether the page still holds the draft this diff is against.
   *
   * The caption is the only thing that changes, and it has to: the comparison
   * stays against the bytes the session was given — that is what Accept would
   * apply — so on a page whose draft has moved, "the draft on this page" names
   * a document this is not about.
   */
  onBaseline?: boolean
}) {
  const moved = diff.entries.filter((entry) => entry.status !== 'unchanged')
  const kept = diff.entries.filter((entry) => entry.status === 'unchanged')
  return (
    <section className={styles.diff} aria-label="The proposal as a diff">
      <p className={styles.honesty}>
        {diff.against !== 'the draft'
          ? `There was nothing to compare with — ${diff.reason} — so every member below is new.`
          : onBaseline
            ? 'Compared with the draft on this page, member by member.'
            : 'Compared with the draft this proposal was given, member by member — the draft on this page has changed since.'}
      </p>
      {moved.length === 0 && (
        <p className={styles.honesty}>
          The proposal is the draft. Nothing in it would change a member.
        </p>
      )}
      {moved.map((entry) => (
        <Entry key={entry.key} entry={entry} />
      ))}
      {kept.length > 0 && (
        <p className={styles.honesty}>
          {kept.length} member{kept.length === 1 ? '' : 's'} unchanged:{' '}
          {kept.map((entry) => entry.label).join(', ')}
        </p>
      )}
    </section>
  )
}

/** One member or one element: what it is, and both of its texts. */
function Entry({ entry }: { entry: DiffEntry }) {
  const children = entry.children ?? []
  const shown = children.filter((child) => child.status !== 'unchanged' || child.moved === true)
  const kept = children.length - shown.length
  const where = entry.pointer === '' ? 'the whole document' : entry.pointer
  return (
    <div className={styles.entry}>
      <p className={styles.entryHead}>
        <span className={styles[entry.status]}>{SAYS[entry.status]}</span>{' '}
        <code>{where}</code>
        {entry.moved === true && ' — moved, with the same value'}
      </p>
      {/*
        An array member compared element by element prints its elements rather
        than both whole arrays: the whole of `rules` twice is the diff nobody
        can read, and the elements are what actually moved.
      */}
      {children.length === 0 ? (
        <>
          {entry.before !== undefined && entry.status !== 'added' && (
            <CodeArea
              value={entry.before}
              readOnly
              rows={rowsFor(entry.before)}
              aria-label={`${where}, in the draft`}
            />
          )}
          {entry.after !== undefined && entry.status !== 'removed' && (
            <CodeArea
              value={entry.after}
              readOnly
              rows={rowsFor(entry.after)}
              aria-label={`${where}, proposed`}
            />
          )}
        </>
      ) : (
        <>
          {shown.map((child) => (
            <Entry key={child.key} entry={child} />
          ))}
          {kept > 0 && (
            <p className={styles.honesty}>
              {kept} element{kept === 1 ? '' : 's'} unchanged, in the same place.
            </p>
          )}
        </>
      )}
    </div>
  )
}

/**
 * How tall one block is drawn, bounded.
 *
 * A `metadata` object is four lines and a `rules` array is two hundred; a box
 * sized to the larger buries the pane and one sized to the smaller hides the
 * answer. The text is complete either way — the box scrolls — and this is only
 * where it starts.
 */
function rowsFor(text: string): number {
  return Math.min(14, Math.max(2, text.split('\n').length))
}
