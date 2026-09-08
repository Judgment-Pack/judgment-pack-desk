/**
 * The one save path the project-file cards on Admin write through.
 *
 * Two cards — Storage and Organization — write two top-level members of one
 * file, and each of them writes **its own member and nothing else**. That is
 * the whole of this module, and every rule in it exists because the obvious
 * implementation gets one of them wrong. (There were four: Panes and Appearance
 * have since left Admin, and each write path left with its form.)
 *
 * **One member changes; every other byte stays.** A form over a parsed object
 * has to re-serialise the file to save it, so a one-word change to an
 * organization name arrives as a diff of every line — indentation, member
 * order, the author's own alignment, the trailing newline. So the edit is a
 * splice: `setRawJson` at the member's own pointer, through the span-preserving
 * writer the pack editor already uses, and every byte outside that span
 * survives. `bytesAt` on each other pointer, before and after, is what holds
 * it.
 *
 * **The bytes are decoded before any of them are sent.** The same decoder that
 * reads the file, run over the file this save would write, so a value this desk
 * would then refuse to read never reaches the disk — and what the reader is
 * shown is the decoder's own sentence against the field its key path names,
 * rather than a second opinion written on the page. This is the browser's half
 * of the rule the chassis holds for the desk-level file, and it is here because
 * the file API forms no opinion about what a file means: it would write an
 * `appearance.theme` of `"dark "` without a word.
 *
 * **The write states the bytes it replaces.** `baseSha256` is the digest the
 * read carried, so a file somebody edited between that read and this write is
 * refused rather than overwritten. A page that sent no digest — or one it
 * invented — would be asserting the state of a file it never saw. There is no
 * override: a card offering "write anyway" would have no concurrency story,
 * only an unstated one.
 *
 * **And the revision it states is held, not read live.** The chassis watches
 * the project and invalidates every query when it sees this file change, so a
 * digest taken from the configuration query would silently move onto bytes
 * nobody saw — and the Save that followed would overwrite somebody else's edit
 * with no 409 at all, which is precisely what the conditional commit exists to
 * prevent. So the bytes and their digest are taken together, once, and move
 * only where the reader acts: an arrival while nothing is unsaved, an explicit
 * Reload, or a save that landed. It is the rule `useFileEditing` holds for the
 * pack editor, and it is here for the same reason rather than by analogy.
 *
 * **The composed value is the file's own, with the edited fields laid over
 * it.** Not the effective value: `panes` is the case that proves it. A file
 * that declares only the rail's width means the other two to stay as they are —
 * `DeclaredPanes` is read off exactly that, and the Inspector's drawer has its
 * own baseline when the width is undeclared — so composing the whole member
 * from the effective configuration would silently declare two dimensions nobody
 * wrote and change a pane on every desk that configures none.
 *
 * **The path is the project's own file, and the card's Location is the same
 * file.** The chassis reports it as an absolute path in the desk-config answer
 * and the card prints that; the file API addresses everything project-relative
 * inside the pinned root, and `jpack-desk.json` is that same file by the name
 * the API takes. Nothing here composes a path.
 */
import { useMutation, useQueryClient, type UseMutationResult } from '@tanstack/react-query'
import { useCallback, useRef, useState } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import {
  PROJECT_CONFIG_PATH,
  decodeDeskConfig,
  effectiveConfig,
  type ConfigProblem,
  type DeskLevelRead,
  type EffectiveConfig
} from '../config/deskConfig'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import { StaleWrite, readFile, writeFile, type FileContent } from '../files/client'
import { agreesWithParse } from '../packs/documentText'
import { valueAt } from '../packs/pointers'
import { buffered, setRawJson, type Buffered } from '../packs/edit/writes'

/**
 * The members this composer will splice, by pointer.
 *
 * A closed list rather than a string, because the pointer is what decides which
 * bytes are spliced: a caller free to name any pointer could splice `identity`
 * out of a desk-level file it never read, and this route's whole claim is that
 * one card writes one member of one file.
 */
export const PROJECT_FILE_POINTERS = [
  '/organization',
  '/appearance',
  '/panes',
  '/storage'
] as const
export type ProjectFilePointer = (typeof PROJECT_FILE_POINTERS)[number]

/**
 * The members a **card** writes, which is not the same list.
 *
 * `/panes` and `/appearance` are missing on purpose, and this is where that is
 * written down. Both cards are gone from Admin — the pane dimensions are the
 * shell's, and the theme and the density are a *person's* preference, held in
 * that person's browser rather than in a file everyone who clones the project
 * shares — and a Save with no control behind it is a write path nothing offers.
 * The composer above keeps both, because it is a general splicer with its own
 * tests; the settings page has no member to hand it.
 *
 * **It is a declaration and not a type constraint, deliberately.** Narrowing the
 * hook's parameter to this list would make the guarantee unbreakable and
 * therefore untestable: a mutation routing an Admin Save through `/appearance`
 * would not compile, and the only row left would mutate the *expected list* —
 * which fails a comparison against itself and proves nothing about the write
 * path. The hook takes the composer's wider pointer, and the guarantee is
 * behavioural: every Save on Admin is driven and what came off the wire is
 * compared to this list.
 */
export const CARD_POINTERS = ['/organization', '/storage'] as const

/**
 * One field a card actually edited, addressed inside its member.
 *
 * A list of edits rather than a whole value, because "the reader changed the
 * theme" and "the reader means every member of `appearance` to be written" are
 * different statements and only the first is true. See the module note on
 * `panes`.
 */
export interface MemberEdit {
  /** The path inside the member — `['packs', 'dir']` under `/storage`. */
  path: readonly string[]
  /** What to write there. `null` is a value; `undefined` is never written. */
  value: unknown
}

/** What a compose produced: the file to write, or why there is none. */
export interface ComposedFile {
  /** The whole file, where it composed one. */
  text?: string
  /** Why it did not, in the decoder's own words, key by key. */
  problems: ConfigProblem[]
}

/**
 * Compose the file one card's save would write, or the problems that stop it.
 *
 * Pure, and separate from the hook, because everything worth proving about a
 * save is provable here: that one member's bytes moved and no others did, that
 * a value the decoder refuses is never composed into a request, and that a
 * member the file omits is added rather than silently dropped.
 */
export function composeProjectFile(
  text: string,
  pointer: ProjectFilePointer,
  edits: readonly MemberEdit[]
): ComposedFile {
  const current = buffered(text)
  // **Two readings of one file, and they have to agree before anything is
  // spliced.** A duplicated top-level member is the case that exists: this
  // scanner keeps the first and `JSON.parse` keeps the last, so a save would
  // decode one value and overwrite another. The chassis refuses to compose over
  // such a file for the same reason, in its own words.
  const disagreements = agreesWithParse(text, current.index)
  if (disagreements.length > 0) {
    return {
      problems: disagreements.map((each) => ({
        key: keyPathOf(each.pointer),
        reason: each.reason
      }))
    }
  }
  const member = compose(valueAt(current.index.value, pointer), edits)
  const next = setRawJson(current, pointer, memberJson(member, shapeOf(current, pointer)))
  // The same decoder the file's reader runs, over the file this would write.
  const decoded = decodeDeskConfig(next.text, 'project')
  if (decoded.problems.length > 0) return { problems: decoded.problems }
  return { text: next.text, problems: [] }
}

/**
 * The member's value with the edited fields laid over it.
 *
 * Immutable, and it keeps the file's own member order: spreading an existing
 * object preserves the order its keys were written in and appends a key that is
 * new, so a save never reorders a member somebody wrote.
 *
 * A container that is not an object is replaced rather than merged into. That
 * case only arises in a file the decoder has already refused — `organization`
 * must be an object — and the cards do not offer a save on one.
 */
function compose(base: unknown, edits: readonly MemberEdit[]): unknown {
  let value: unknown = base === undefined ? {} : base
  for (const edit of edits) value = setIn(value, edit.path, edit.value)
  return value
}

function setIn(value: unknown, path: readonly string[], next: unknown): unknown {
  if (path.length === 0) return next
  const [head, ...rest] = path
  const record: Record<string, unknown> = isObject(value) ? { ...value } : {}
  record[head!] = setIn(record[head!], rest, next)
  return record
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/** How one member is laid out in the file it is written back into. */
interface MemberShape {
  /** The indentation its own line sits at. */
  indent: string
  /** Whether the file writes it on one line. */
  inline: boolean
}

/**
 * The bytes one member is written with.
 *
 * **The shape the file already gives that member**, and only that member: a
 * one-line member is written back on one line, and a member laid out over
 * several keeps two spaces per level, with every line after the first carrying
 * its own indentation. Neither is a house style imposed on the file — the rest
 * of it is spliced around these bytes untouched — and the reason for following
 * the member rather than picking one is that either choice, applied always, is
 * a reformatting nobody asked for: expanding a one-line member turns a one-word
 * edit into four lines of diff, and collapsing a member somebody aligned by
 * hand throws that alignment away.
 */
function memberJson(value: unknown, shape: MemberShape): string {
  if (shape.inline) return JSON.stringify(value)
  return JSON.stringify(value, null, 2).split('\n').join(`\n${shape.indent}`)
}

/**
 * How the file lays this member out, read off it rather than assumed.
 *
 * Its own layout where the file already carries the member; otherwise the first
 * top-level member's indentation — an added member takes the layout its
 * neighbours use — on one line, which is the smaller addition to a file this
 * desk did not write and the shape the chassis composes a member in.
 */
function shapeOf(current: Buffered, pointer: ProjectFilePointer): MemberShape {
  const own = current.index.spans.get(pointer)
  if (own !== undefined) {
    const measured = runBefore(current.text, own.memberStart)
    if (measured !== undefined) {
      return {
        indent: measured,
        inline: !current.text.slice(own.valueStart, own.valueEnd).includes('\n')
      }
    }
  }
  for (const span of current.index.spans.values()) {
    if (span.pointer === '' || span.pointer.indexOf('/', 1) >= 0) continue
    const neighbour = runBefore(current.text, span.memberStart)
    if (neighbour !== undefined) return { indent: neighbour, inline: true }
  }
  return { indent: '  ', inline: true }
}

/** The run of spaces or tabs immediately before an offset, on its own line. */
function runBefore(text: string, at: number): string | undefined {
  let start = at
  while (start > 0 && (text[start - 1] === ' ' || text[start - 1] === '\t')) start -= 1
  if (start === 0 || text[start - 1] !== '\n') return undefined
  return text.slice(start, at)
}

/** A pointer as the decoder spells a key: `/storage/packs` is `storage.packs`. */
function keyPathOf(pointer: string): string {
  return pointer.startsWith('/') ? pointer.slice(1).split('/').join('.') : ''
}

/** What one card's save says when it lands. One sentence, from a closed set. */
export const SAVED = 'Saved. Every other member of the file is exactly as it was.'

/**
 * The sentence a card refuses to write on, where this page never read the file.
 *
 * A read that produced no bytes and no digest is a page that has not seen the
 * file this save would replace. The desk-level form says the same thing in the
 * same words for the same reason, and neither guesses.
 */
export const NOT_READ =
  'This desk has not read this project’s configuration file, and a write states the bytes it replaces.'

/**
 * The sentence a card says where the value it shows comes from the other file.
 *
 * The desk-level file supplies a section wherever the project's own file does
 * not, and this page has no write route for those members of it. Saving here
 * would write the project file instead — a different file from the one the
 * card's Location names — so it is not offered, and the reason is said rather
 * than left as a control that does nothing.
 */
export const FROM_THE_DESK_FILE =
  'This value comes from the desk-level file, which this page does not write.'

/**
 * The sentence a card says over a file this desk would not read back.
 *
 * A refused file is the built-in defaults plus a list of problems, so the values
 * on the form are nobody's configuration; composing a write over one would be
 * this page saving a file it never honoured.
 */
export const REFUSED = 'This file was refused, so these values are the built-in ones.'

export interface ProjectFileSave {
  /** True where this page has the bytes and the digest a write states. */
  ready: boolean
  /** Why it has not, where it has not. One sentence, from the four above. */
  blocked: string | undefined
  pending: boolean
  /**
   * Every problem this save was refused with, whoever refused it: this page's
   * own decoder before the write, or the chassis after it.
   */
  problems: readonly ConfigProblem[]
  /** The file moved underneath this card. Nothing was written. */
  stale: StaleWrite | undefined
  /** Any other refusal, as its own sentence. */
  refusal: string | undefined
  /** What the last save that landed said. */
  said: string | undefined
  /**
   * Write the member, with only the fields the reader actually changed.
   *
   * `onSaved` runs when the write lands, and only then. It is how a form
   * withdraws the values it was holding: what the file says afterwards is the
   * **decoded** value, which is not always what was typed — an `idBase` gains
   * the separator it was missing, a `dir` loses its trailing one — and a form
   * that went on holding the raw input would stay dirty for ever over a save
   * that succeeded, offering to write again what the file already says.
   */
  save: (edits: readonly MemberEdit[], onSaved?: () => void) => void
  /**
   * Read the file again and hold what it says, so the next save states a
   * digest that is true.
   *
   * A direct read rather than a refetch, for the reason `useFileEditing` gives:
   * the watcher's broad `cancelQueries` makes `refetch` report success from
   * cache when it cancels the request in flight, so its success is not proof
   * that anything was fetched — and installing cached bytes as the new base is
   * a reload that replaces nothing with what it was already showing.
   */
  reload: () => void
  /** True while that read is in the air. */
  reloading: boolean
  /** Forget the last attempt's verdict, which an edit does. */
  forget: () => void
}

/** One revision of the file: the bytes, and what they hash to. */
interface Revision {
  text: string
  sha256: string
}

/**
 * One card's save, wired to the file API and to the configuration query.
 *
 * **The answer moves the cache, and the re-read only confirms it.** The write
 * answers with the file read back off the disk after the rename, so the shell —
 * the organization name in the header, the theme, the pane bounds — reflects a
 * save without waiting for a second request; a page that invalidated and waited
 * would leave the header showing what was just replaced under a form that said
 * "Saved". The invalidation follows, because the answer is authoritative about
 * this write and not about anything that happened after it.
 *
 * **A refused write invalidates nothing.** Re-reading after a 409 or a 422 would
 * be this page telling itself that something happened.
 */
export function useProjectFileSave(
  pointer: ProjectFilePointer,
  /**
   * Whether the card is holding a value nobody has written yet.
   *
   * **This is what decides when the revision may move.** A live query moves on
   * a watcher notification; a base that followed it would rebase this card onto
   * bytes nobody saw, and the next Save would overwrite a change with no
   * refusal at all.
   */
  unsaved = false
): ProjectFileSave {
  const effective = useEffectiveConfig()
  const client = useQueryClient()
  const write = useWriteProjectFile()
  const [problems, setProblems] = useState<readonly ConfigProblem[]>([])
  const [said, setSaid] = useState<string | undefined>(undefined)
  const [base, setBase] = useState<Revision | undefined>(undefined)
  const [reloading, setReloading] = useState(false)
  const [readFailure, setReadFailure] = useState<string | undefined>(undefined)
  // Only the last reload asked for counts. An earlier one resolving afterwards
  // is answering a question that has been replaced.
  const reloads = useRef(0)

  const live: Revision | undefined =
    effective.text === undefined || effective.sha256 === undefined
      ? undefined
      : { text: effective.text, sha256: effective.sha256 }
  // **Taken on arrival, and afterwards only while there is nothing to lose.**
  // A card nobody is typing into follows the file, which is what makes an edit
  // in another editor show up here; a card that is holding a value does not,
  // which is what makes the next Save state the revision it was composed
  // against.
  if (live !== undefined && live.sha256 !== base?.sha256 && (base === undefined || !unsaved)) {
    setBase(live)
  }

  const source = effective.sources[sectionOf(pointer)]
  const blocked =
    source === 'desk file'
      ? FROM_THE_DESK_FILE
      : effective.problems.length > 0
        ? REFUSED
        : base === undefined
          ? NOT_READ
          : undefined

  const forget = useCallback(() => {
    setProblems([])
    setSaid(undefined)
    write.reset()
    // The mutation object is stable; depending on it would rebuild this on
    // every state change of the write.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const save = (edits: readonly MemberEdit[], onSaved?: () => void) => {
    if (blocked !== undefined || base === undefined) return
    setProblems([])
    setSaid(undefined)
    setReadFailure(undefined)
    write.reset()
    // The bytes this digest is the digest **of**. Composing against anything
    // else would send a file built on one revision under a claim about another.
    const composed = composeProjectFile(base.text, pointer, edits)
    // **Refused here, and nothing is sent.** The file API would write these
    // bytes without an opinion; the decoder that has one is this page's, and it
    // is asked before the request rather than after it.
    if (composed.text === undefined) {
      setProblems(composed.problems)
      return
    }
    write.mutate(
      { path: PROJECT_CONFIG_PATH, content: composed.text, baseSha256: base.sha256 },
      {
        onSuccess: (landed) => {
          // The revision moves because the reader acted, and it moves onto what
          // the chassis read back off the disk rather than onto what was sent.
          setBase({ text: landed.content, sha256: landed.sha256 })
          client.setQueryData<EffectiveConfig>(DESK_CONFIG_QUERY_KEY, (previous) =>
            configAfterProjectFileWrite(previous, landed)
          )
          void client.invalidateQueries({ queryKey: DESK_CONFIG_QUERY_KEY })
          setSaid(SAVED)
          // Last, and only on a write that landed: the fields go back to
          // following the file, which now says what the decoder made of them.
          onSaved?.()
        }
      }
    )
  }

  const reload = () => {
    const ticket = (reloads.current += 1)
    setProblems([])
    setSaid(undefined)
    setReadFailure(undefined)
    setReloading(true)
    void readFile(PROJECT_CONFIG_PATH)
      .then((fresh) => {
        if (ticket !== reloads.current) return
        setReloading(false)
        // **The refusal is cleared here and nowhere earlier.** Round 1 found it
        // cleared on the button press instead: a read that then failed left the
        // card with only the read's own error — no digests, no Reload — while
        // the revision behind it had not moved, so the next Save was refused
        // again for a reason nothing on screen still said.
        write.reset()
        setBase({ text: fresh.content, sha256: fresh.sha256 })
        client.setQueryData<EffectiveConfig>(DESK_CONFIG_QUERY_KEY, (previous) =>
          configAfterProjectFileWrite(previous, fresh)
        )
      })
      .catch((cause: unknown) => {
        if (ticket !== reloads.current) return
        setReloading(false)
        // **The conflict stands until the read lands.** A failed reload that
        // cleared it would leave the card with no notice at all and a Save that
        // would be refused again.
        setReadFailure(cause instanceof Error ? cause.message : String(cause))
      })
  }

  const stale = write.error instanceof StaleWrite ? write.error : undefined
  const chassisProblems = problemsOf(write.error)
  return {
    ready: blocked === undefined,
    blocked,
    pending: write.isPending,
    problems: problems.length > 0 ? problems : chassisProblems,
    stale,
    refusal:
      readFailure ??
      (stale === undefined && chassisProblems.length === 0
        ? (write.error?.message ?? undefined)
        : undefined),
    said,
    save,
    reload,
    reloading,
    forget
  }
}

/** Which top-level section a pointer names, for the source badge. */
function sectionOf(pointer: ProjectFilePointer): 'organization' | 'appearance' | 'panes' | 'storage' {
  return pointer.slice(1) as 'organization' | 'appearance' | 'panes' | 'storage'
}

/** The `{key, reason}` list a refusal carried, and an empty list for one that did not. */
function problemsOf(error: Error | null): readonly ConfigProblem[] {
  const problems = (error as { problems?: readonly ConfigProblem[] } | null)?.problems
  return problems ?? []
}

/**
 * One save.
 *
 * A mutation of its own rather than `useWriteFile`, so a save here cannot
 * acquire the editor's `override` or `createParents` by a caller passing one:
 * the request this route makes is a conditional replacement of a file that is
 * already there, and neither of those is a choice a configuration card offers.
 * Nothing retries — a retried conditional commit is one write becoming two, and
 * the second would carry a digest the first has already made stale.
 */
function useWriteProjectFile(): UseMutationResult<
  FileContent,
  Error,
  { path: string; content: string; baseSha256: string }
> {
  return useMutation({
    mutationFn: (input: { path: string; content: string; baseSha256: string }) =>
      writeFile(input),
    retry: false
  })
}

/**
 * What the cached configuration becomes once a project-file write lands.
 *
 * **Re-layered from the answer, through the one function that states the
 * precedence.** The write answers with the whole project file as the chassis
 * read it back off the disk, so it is decoded and merged over the desk-level
 * read this page already has — rather than the new member being patched into
 * the effective values, which would be this page deciding a layering question
 * that `effectiveConfig` exists to answer once.
 */
export function configAfterProjectFileWrite(
  previous: EffectiveConfig | undefined,
  landed: FileContent
): EffectiveConfig | undefined {
  if (previous === undefined) return undefined
  return effectiveConfig(
    decodeDeskConfig(landed.content, 'project'),
    undefined,
    undefined,
    deskReadOf(previous),
    landed.content,
    landed.sha256
  )
}

/** The desk-level read this page already made, as `effectiveConfig` takes it. */
function deskReadOf(previous: EffectiveConfig): DeskLevelRead | undefined {
  const desk = previous.desk
  if (desk === undefined) return undefined
  return {
    path: desk.path,
    present: desk.present,
    text: desk.text,
    sha256: desk.sha256,
    decoded: desk.decoded,
    note: desk.note,
    readFailure: desk.readFailure,
    chassis: desk.chassis
  }
}
