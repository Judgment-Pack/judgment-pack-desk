/**
 * One Admin section, as one card: where the value is written, whether it was
 * read, what is in the file, the fields, and the Save where there is one.
 *
 * **Every section on Admin is this card**, and the shape is the argument. The
 * page used to answer a different question per section — a paragraph here, a
 * warning note there, a paste block at the foot — and a reader who wanted the
 * same four facts about two settings had to find them in two shapes. Four
 * slots, in one order, is what makes the page readable without a paragraph
 * telling anyone how to read it.
 *
 * **Location comes from the chassis and is never composed here.** The desk-level
 * file's path, the project file's path and the runtime binary are facts about
 * the machine this desk runs on; a page that joined a directory to a file name
 * would be asserting a location on a filesystem it cannot see, and would be
 * wrong the first time somebody's `XDG_CONFIG_HOME` was not where this page
 * guessed.
 *
 * **Status is one line from a closed set**, and a refusal in it is the
 * decoder's own sentence, key path and all. The warning notes this replaced
 * were prose about a problem; a status line is the problem.
 *
 * **Content is the member's own bytes** where this page read the file, and the
 * decoded value — labelled as decoded — where it did not. See `memberBytes`.
 */
import type { ReactNode } from 'react'
import type { ConfigProblem, ReadFailure } from '../config/deskConfig'
import { memberBytes } from './memberBytes'
import styles from './SourceCard.module.css'

/**
 * What a card may say about the file behind it, and the whole of it.
 *
 * Six states rather than three, because collapsing any two of them is a
 * sentence this desk has already had to withdraw once: an absent file is not a
 * refused one, a refused one is not an unread one, and an unread one
 * establishes only that absence was **not** established.
 */
export type SourceStatus =
  | { state: 'read' }
  | { state: 'absent' }
  | { state: 'pending' }
  | { state: 'refused'; problems: ConfigProblem[] }
  | { state: 'unread'; failure: ReadFailure }
  /** The runtime card: a connection rather than a file. */
  | { state: 'said'; says: string }

/** What the Content disclosure shows, and where it comes from. */
export interface CardContent {
  /** The file's text, where this page read it. */
  text?: string
  /** The top-level member to quote, or the whole file where absent. */
  member?: string
  /** What is shown where the bytes cannot be established. */
  value: unknown
}

export function SourceCard({
  id,
  title,
  location,
  status,
  content,
  fields,
  save
}: {
  id: string
  title: string
  /** The path, from the chassis. A card with nowhere to point says so. */
  location: ReactNode
  status: SourceStatus
  content?: CardContent
  fields?: ReactNode
  /**
   * The one write this card offers, where it offers one.
   *
   * Panes' reset is here too: it is that card's only control and it changes
   * this browser's record of the layout, which is the same kind of thing a
   * Save is — something the reader does, reported by what happened.
   */
  save?: ReactNode
}) {
  return (
    <section className={styles.card} aria-labelledby={`${id}-title`}>
      <h2 className={styles.title} id={id}>
        <span id={`${id}-title`}>{title}</span>
      </h2>
      <dl className={styles.head}>
        <div className={styles.row}>
          <dt className={styles.key}>Location</dt>
          <dd className={styles.value}>{location}</dd>
        </div>
        <div className={styles.row}>
          <dt className={styles.key}>Status</dt>
          <dd className={styles.value}>
            <StatusLine status={status} />
          </dd>
        </div>
      </dl>
      {content !== undefined && <Content content={content} />}
      {fields !== undefined && <div className={styles.fields}>{fields}</div>}
      {save !== undefined && <div className={styles.save}>{save}</div>}
    </section>
  )
}

/**
 * One status, as one line.
 *
 * A refusal is rendered in the decoder's own words, in a `code` element,
 * because it is quoted material rather than a sentence this page wrote — and
 * the narration sweep exempts quoted material for exactly that reason.
 */
function StatusLine({ status }: { status: SourceStatus }) {
  if (status.state === 'read') return <>read</>
  if (status.state === 'absent') return <>not present — defaults in use</>
  if (status.state === 'pending') return <>not read yet</>
  if (status.state === 'said') return <>{status.says}</>
  if (status.state === 'refused') {
    return (
      <>
        refused:{' '}
        {status.problems.map((problem) => (
          <code key={`${problem.key}:${problem.reason}`} className={styles.reason}>
            {problem.key === '' ? problem.reason : `${problem.key}: ${problem.reason}`}
          </code>
        ))}
      </>
    )
  }
  return <UnreadLine failure={status.failure} />
}

/**
 * A read that did not produce a file, with **who said so** carried rather than
 * inferred.
 *
 * The three provenances are three different facts and were once read off "is
 * there a status?", which put a 200 whose body this desk cannot use into the
 * transport-failure bucket and had the page say the request never got an
 * answer. The reason itself is always quoted, whoever wrote it.
 */
function UnreadLine({ failure }: { failure: ReadFailure }) {
  return (
    <>
      {!failure.responseReceived ? (
        <>not read — the browser’s own reason: </>
      ) : failure.source === 'chassis' ? (
        <>not read — the desk answered {failure.status}, and its own reason: </>
      ) : (
        <>not read — the desk answered {failure.status}, and this page’s reason: </>
      )}
      <code className={styles.reason}>{failure.reason}</code>
    </>
  )
}

/** The file, or the one member of it this card is about. */
function Content({ content }: { content: CardContent }) {
  const bytes =
    content.text === undefined
      ? undefined
      : content.member === undefined
        ? content.text
        : memberBytes(content.text, content.member)
  // The bytes where this page has them; the decoded value, said to be decoded,
  // where it does not. Showing a re-serialisation and calling it the file is
  // the one thing this disclosure must not do.
  const shown = bytes ?? JSON.stringify(content.value, null, 2)
  return (
    <details className={styles.content}>
      <summary className={styles.summary}>
        Content {bytes === undefined && <span className={styles.decoded}>(decoded)</span>}
      </summary>
      <pre className={styles.json}>
        <code>{shown}</code>
      </pre>
    </details>
  )
}

/**
 * One label, one control or value, and at most one line of rule under it.
 *
 * `rule` is for a value that has one — a range, a shape the decoder enforces —
 * in the decoder's own words. It is not a place for help: a card that needed a
 * paragraph to be understood is a card whose fields are wrong.
 */
export function CardField({
  label,
  rule,
  children
}: {
  label: string
  rule?: ReactNode
  children: ReactNode
}) {
  return (
    <div className={styles.field}>
      <span className={styles.key}>{label}</span>
      <div className={styles.value}>{children}</div>
      {rule !== undefined && <p className={styles.rule}>{rule}</p>}
    </div>
  )
}
