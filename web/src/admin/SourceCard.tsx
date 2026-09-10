/**
 * One Admin section, as one card: where the value is written, whether it was
 * read, what is in the file, the fields, and the Save where there is one — and
 * the group of cards that share a file.
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
 * **And it is stated once per file, not once per card.** Three cards writing
 * three members of one file printed that file's path three times and its read
 * status three times, which reads as three files. So the cards that share a
 * file sit inside a `SourceGroup` whose header states it, and a card under one
 * says neither — `under` is the group's own status, and a card keeps its Status
 * only where it **differs**: a member refused inside an accepted file, a save in
 * flight, a write the file moved under. A member that came from the *other*
 * file is not one this group's header speaks for, and is given no `under` at
 * all, so it states its own location as before.
 *
 * **Status is one line from a closed set**, and a refusal in it is the
 * decoder's own sentence, key path and all. The warning notes this replaced
 * were prose about a problem; a status line is the problem.
 *
 * **The Content disclosure is gone from this card**, and the two things it
 * carried went to two different places. The bytes are context rather than a
 * setting — nobody edits them here — so they are in the right pane now, through
 * `ConfigPane`, which is the same claim the pack routes make on that slot. What
 * stayed is the *rule*: `showsContent` is exported from here and the pane
 * imports it, because a refused file's bytes are the thing the refusal is about
 * and two spellings of that rule would be invisible to a harness that broke one
 * of them.
 *
 * `StatusLine` is exported for the same reason. The pane says what state the
 * file is in, and a second vocabulary for six states is a second set of
 * sentences to keep in step.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode
} from 'react'
import type { ConfigNotice, ConfigProblem, ReadFailure } from '../config/deskConfig'
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
  | { state: 'refused'; problems: readonly ConfigProblem[] }
  | { state: 'unread'; failure: ReadFailure }
  /**
   * The file was read, and this decoder did something with a member of it.
   *
   * Seven states now, and this is the one that is **not** a problem: the file
   * is in use exactly as it decoded, and what the line adds is the sentence
   * saying so. Folding it into `read` would lose the sentence; folding it into
   * `refused` would tell a reader nothing was written when everything was.
   */
  | { state: 'migrated'; notices: readonly ConfigNotice[] }
  /** A connection rather than a file. */
  | { state: 'said'; says: string }
  /** This card's own write is in the air. */
  | { state: 'writing' }
  /** The file moved underneath this card's write. Nothing was written. */
  | { state: 'stale' }
  /**
   * This card's write was refused — by this page's decoder before it was sent,
   * or by the chassis after it — and nothing was written.
   */
  | { state: 'not-written'; problems: readonly ConfigProblem[]; reason?: string }

/**
 * What a card's own write is doing, published upward by the form inside it.
 *
 * **A read status is not the whole of what a card can be.** The group header
 * suppresses a card's Status where it says what the group already said, and the
 * group's is the *file's* read state — so a card whose write was refused, or
 * whose write is in the air, or under which the file moved, showed no Status at
 * all while the group said `read`. Those are three things this card knows and
 * the group does not.
 *
 * It is published upward rather than lifted, on the pattern the Inspector slot
 * already uses: the form owns the draft and the save, and the `save` node is
 * handed to this card as a prop, so the card cannot read the form's state and
 * the form cannot reach the card's head. A form rendered outside a card — every
 * case in `projectFileCards.test.tsx` — finds no sink and publishes nothing.
 */
const WriteStatusSink = createContext<((status: SourceStatus | undefined) => void) | undefined>(
  undefined
)

/** Publish this form's write state to the card it is inside, where it is in one. */
export function usePublishedWriteStatus(status: SourceStatus | undefined): void {
  const publish = useContext(WriteStatusSink)
  // The value is read at the moment it is published; its signature is the
  // dependency, because a fresh object on every render is not a change.
  const signature = JSON.stringify(status ?? null)
  useEffect(() => {
    publish?.(status)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [publish, signature])
  // A form that leaves takes its verdict with it: a card whose Save unmounted
  // must not go on reporting a write nothing is doing.
  useEffect(() => () => publish?.(undefined), [publish])
}

export function SourceCard({
  id,
  title,
  location,
  status,
  fields,
  save,
  under,
  level
}: {
  id: string
  title: string
  /** The path, from the chassis. A card with nowhere to point says so. */
  location: ReactNode
  status: SourceStatus
  fields?: ReactNode
  /** The one write this card offers, where it offers one. */
  save?: ReactNode
  /**
   * The status of the group header above this card, where there is one.
   *
   * Its presence is what says "the location is stated already"; its value is
   * what a card's own status is compared against, so a card says its status
   * only where it has something the group has not already said.
   */
  under?: SourceStatus
  /**
   * The heading level, where the document's outline is not the nesting.
   *
   * It follows `under` by default — a card inside a group is a subsection of
   * it — and Admin's open section is the exception the prop exists for: it
   * states no location, because the list beside it and the pane already do,
   * and it is nonetheless a top-level section of the page rather than a
   * member of a group that is not rendered around it.
   */
  level?: 2 | 3
}) {
  const grouped = under !== undefined
  // What the form inside this card, if any, says its own write is doing.
  const [write, setWrite] = useState<SourceStatus | undefined>(undefined)
  const publish = useCallback((next: SourceStatus | undefined) => setWrite(next), [])
  // **The write is the more recent fact about this card**, and it is never
  // what the group said — the group's status is the file's read state — so a
  // card writing, refused, or holding a stale write always shows its own row.
  const says = write ?? status
  return (
    <section className={grouped ? styles.member : styles.card} aria-labelledby={`${id}-title`}>
      <Title
        id={id}
        title={title}
        level={level ?? (grouped ? 3 : 2)}
        className={styles.title}
      />
      <Head
        location={grouped ? undefined : location}
        status={grouped && sameStatus(says, under) ? undefined : says}
      />
      {fields !== undefined && <div className={styles.fields}>{fields}</div>}
      {save !== undefined && (
        <div className={styles.save}>
          <WriteStatusSink.Provider value={publish}>{save}</WriteStatusSink.Provider>
        </div>
      )}
    </section>
  )
}

/**
 * The cards that write one file, under one statement of where that file is.
 *
 * The header is a card in every respect but one — it has the same Location,
 * Status and fields, and the group's own write where it has one (the project's
 * default-project nomination is exactly that) — and then the members under it.
 * A group is not a heading with a border: it is the sentence "these are the
 * members of *this* file", and what sits inside it is those members: the four
 * cards this page had, and the rows of the overview that replaced them.
 */
export function SourceGroup({
  id,
  title,
  location,
  status,
  fields,
  save,
  children
}: {
  id: string
  title: string
  location: ReactNode
  status: SourceStatus
  fields?: ReactNode
  save?: ReactNode
  children: ReactNode
}) {
  return (
    <section className={styles.group} aria-labelledby={`${id}-title`}>
      <Title id={id} title={title} level={2} className={styles.groupTitle} />
      <Head location={location} status={status} />
      {fields !== undefined && <div className={styles.fields}>{fields}</div>}
      {save !== undefined && <div className={styles.save}>{save}</div>}
      <div className={styles.members}>{children}</div>
    </section>
  )
}

/**
 * Whether two statuses say the same thing.
 *
 * Compared by value rather than by state alone, because two refusals are not
 * one status: a member refused inside a file the group calls refused may name
 * a different key, and the card is the only place that key is said.
 */
function sameStatus(one: SourceStatus, other: SourceStatus): boolean {
  return JSON.stringify(one) === JSON.stringify(other)
}

/**
 * The heading, at the level its place in the document actually is.
 *
 * A card inside a group is a subsection of it, and a page whose groups and
 * members were both `h2` would offer a reader an outline that is not the one
 * on the screen.
 */
function Title({
  id,
  title,
  level,
  className
}: {
  id: string
  title: string
  level: 2 | 3
  className: string
}) {
  const Tag = level === 2 ? 'h2' : 'h3'
  return (
    <Tag className={className} id={id}>
      <span id={`${id}-title`}>{title}</span>
    </Tag>
  )
}

/** The two rows, and only the ones this card or group actually states. */
function Head({ location, status }: { location?: ReactNode; status?: SourceStatus }) {
  if (location === undefined && status === undefined) return null
  return (
    <dl className={styles.head}>
      {location !== undefined && (
        <div className={styles.row}>
          <dt className={styles.key}>Location</dt>
          <dd className={styles.value}>{location}</dd>
        </div>
      )}
      {status !== undefined && (
        <div className={styles.row}>
          <dt className={styles.key}>Status</dt>
          <dd className={styles.value}>
            <StatusLine status={status} />
          </dd>
        </div>
      )}
    </dl>
  )
}

/**
 * Whether anything on Admin may show what is in a file at all.
 *
 * **Not on a refusal, and not on a read that did not produce one.** A refused
 * file's bytes are the thing the refusal is about; an unread file's are bytes
 * this page never had.
 *
 * Exported, and there is exactly one of it. The bytes are rendered in the right
 * pane now, and a second copy of this rule living beside them is the shape the
 * `Content` disclosure already had to have taken out once: two spellings, one
 * of which a mutation can break while the other goes on saying it.
 */
export function showsContent(status: SourceStatus): boolean {
  return status.state !== 'refused' && status.state !== 'unread'
}

/**
 * One status, as one line.
 *
 * A refusal is rendered in the decoder's own words, in a `code` element,
 * because it is quoted material rather than a sentence this page wrote — and
 * the narration sweep exempts quoted material for exactly that reason.
 */
export function StatusLine({ status }: { status: SourceStatus }) {
  if (status.state === 'read') return <>read</>
  if (status.state === 'absent') return <>not present — defaults in use</>
  if (status.state === 'pending') return <>not read yet</>
  if (status.state === 'said') return <>{status.says}</>
  if (status.state === 'migrated') {
    return (
      <>
        read — <Notices notices={status.notices} />
      </>
    )
  }
  if (status.state === 'writing') return <>writing — nothing is written until the desk answers</>
  if (status.state === 'stale') return <>the file changed on disk — nothing was written</>
  if (status.state === 'not-written') {
    return (
      <>
        not written:{' '}
        {status.reason !== undefined && <code className={styles.reason}>{status.reason}</code>}
        <Problems problems={status.problems} />
      </>
    )
  }
  if (status.state === 'refused') {
    return (
      <>
        refused: <Problems problems={status.problems} />
      </>
    )
  }
  return <UnreadLine failure={status.failure} />
}

/** The decoder's own sentences about what it did, as quoted material. */
function Notices({ notices }: { notices: readonly ConfigNotice[] }) {
  return (
    <>
      {notices.map((notice) => (
        <code key={`${notice.key}:${notice.says}`} className={styles.reason}>
          {notice.says}
        </code>
      ))}
    </>
  )
}

/** The decoder's own sentences, key path and all, as quoted material. */
function Problems({ problems }: { problems: readonly ConfigProblem[] }) {
  return (
    <>
      {problems.map((problem) => (
        <code key={`${problem.key}:${problem.reason}`} className={styles.reason}>
          {problem.key === '' ? problem.reason : `${problem.key}: ${problem.reason}`}
        </code>
      ))}
    </>
  )
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
