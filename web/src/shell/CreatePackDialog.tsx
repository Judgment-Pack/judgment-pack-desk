/**
 * Create a pack: a name, a description, and a template.
 *
 * Where the file goes is not a question for whoever is creating a pack. The
 * admin configured `storage.packs` once; the name gives the id, the id gives
 * the file name, and the rest is arithmetic. So this dialog asks for the three
 * things only a person can answer and decides the other two.
 *
 * # What happens on Create, and what is said when it does not
 *
 * 0. **Everything that can refuse this is asked first, before anything is
 *    written.** The listing has to have answered; the project has to have a
 *    `jpack.json` (`packs` carries `minProperties: 1`, so this can only ever
 *    amend one and never write one from nothing); that file has to read and
 *    parse; and the id has to be free *in the file as it is now* — not as it
 *    was when this dialog opened. A refusal here leaves nothing behind.
 * 1. The pack file is written, asking for its parent to be made.
 * 2. The entry is added to the configuration read in (0) and written with the
 *    digest that read returned — so a change made in between is refused rather
 *    than overwritten.
 * 3. The caches are invalidated and the new pack's page is opened.
 *
 * **Why the read moved in front of the write.** With it after, the only
 * collision check was against a cached listing, and `withPack` replaces the
 * key it is given: creating `vendor-onboarding` in a project that already had
 * one — registered under a filename this desk does not write, so no file
 * collided — silently unregistered the original document and reported success.
 * The freshly read file is the only thing entitled to answer "is this id
 * taken", and asking it before the write is also what keeps an unreadable or
 * unparseable configuration from producing an orphan.
 *
 * **If 1 succeeds and 2 fails, the pack file is on disk and nothing names it.**
 * The dialog says exactly that, and stays on screen to say it: dismissal is
 * held while the sequence runs, because a dialog that unmounts mid-flight
 * reports the residue to nobody. There is no unwind to perform — the file API
 * has no delete verb — and claiming one would be worse than the residue.
 */
import { SegmentedControl } from '../ui/SegmentedControl'
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useId, useMemo, useRef, useState, type RefObject } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { FileRequestError, readFile, writeFile, type FileContent } from '../files/client'
import { useFileContent, useFileListing } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { useValidate } from '../mcp/queries'
import { anchor, layersReached, truncationNote } from '../packs/checks'
import { DiagnosticList } from '../packs/DiagnosticList'
import { useIdleCheck } from '../packs/edit/useIdleCheck'
import { RuntimeRefusal, useExample, useExampleListing, useSchema } from '../mcp/starters'
import {
  existingPackKeys,
  existingPackPaths,
  packEntryFor,
  parseProjectConfig,
  serialiseProjectConfig,
  withPack,
  declaredMatrixPaths,
  type ProjectConfig
} from '../packs/jpackConfig'
import { codeOf, refusalDetail, refusalLead } from '../packs/createRefusal'
import { samePath } from '../packs/packPath'
import { DescribeIt, useDescribeIt } from './DescribeIt'
import {
  collisionIn,
  emptyPackFrom,
  packFromProposal,
  packPathFor,
  shapeTemplate,
  slugFor
} from '../packs/newPack'
import { Alert } from '../ui/Alert'
import { Button } from '../ui/Button'
import { Dialog, DialogActions, DialogClose } from '../ui/Dialog'
import { Field, FieldGroup } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { TextArea } from '../ui/TextArea'
import { PageHeader, PageBody } from '../ui/PageLayout'
import { ProposalUnknowns } from '../assistant/ProposalReport'
import { DraftPackEditor } from '../packs/DraftPackEditor'
import { PackOverview } from '../packs/PackWorkspace'
import { ownerOf } from '../packs/edit/editingContext'
import { buffered, bytesAt } from '../packs/edit/writes'
import { isRecord } from '../packs/document/MisshapenMember'
import { useHeldText } from '../packs/edit/heldText'
import type { PackDocument } from '../mcp/types'
import { useInspectorPortal, useInspectorSlot } from './InspectorSlot'
import type { ResearchHandover } from '../routes/ResearchAuthoringPage'
import { ButtonLink } from '../ui/Button'
import { recordActivity } from './consoleLog'
import flow from './CreatePackFlow.module.css'

const PROJECT_FILE = 'jpack.json'

/**
 * The two kinds of option, namespaced.
 *
 * A bare `"empty"` sentinel is a value a runtime can legitimately serve as an
 * example name, and one that did produced two options with the same value —
 * whichever the viewer picked was routed to `get_schema`. The prefix makes the
 * two spaces disjoint, and the example half is encoded so a name carrying the
 * separator cannot spell its way into the other one.
 */
const SCHEMA_EMPTY = 'schema:empty'
const EMPTY_LABEL = 'Empty pack'
/**
 * The fifth state of the choice: the document the assistant proposed.
 *
 * In the same namespaced space as the other two, so a runtime that serves an
 * example called `the-assistants-proposal` cannot spell its way into it.
 */
const PROPOSAL_SOURCE = 'proposal:the-assistants'
const PROPOSAL_LABEL = 'The assistant’s proposal'
const exampleValue = (name: string) => `example:${encodeURIComponent(name)}`
const exampleNameOf = (value: string): string | undefined =>
  value.startsWith('example:') ? decodeURIComponent(value.slice('example:'.length)) : undefined

const UNREADABLE_PROJECT =
  'This project’s files could not be read, so nothing was created.'
const NO_PROJECT_FILE =
  'This project has no jpack.json, so a new pack cannot be registered. Nothing was created.'
const UNREADABLE_PROJECT_FILE =
  'This project’s jpack.json could not be read, so a new pack cannot be registered. Nothing was created.'
const STALE_PROJECT_FILE = 'jpack.json changed while creating — reload and try again'
const PACK_FILE_TAKEN = 'Something is already there under that name — try another.'
const COMPANION_PATH_TAKEN =
  'The test cases and research record would be written beside the pack, and one of those two names is taken. Nothing was written; try another name.'
const ORPHANED =
  'The pack was created but could not be registered. Nothing else was changed.'
const COMPANIONS_ORPHANED =
  'The pack file was written, but its test cases or research record could not be written beside it, so nothing names the pack yet. Fix the cause and create it again under another name, or register the file by hand.'
const NO_TEMPLATE = 'There is no template to start from here.'
const TEMPLATE_UNUSABLE = 'This template could not be used.'
const NO_VALIDATE =
  'This desk cannot check a proposed document here, because this connection serves no validate. Nothing was created.'
const CHECKING = 'Checking the proposed document…'
const PROPOSAL_UNUSABLE = 'This proposal could not be used, so nothing was created.'
const RENAMED = 'Named from the field above, not from the proposal.'
const TEMPLATES_PENDING = 'Asking the runtime what it can start from…'
const PARTIAL_PROJECT =
  'This project\u2019s file listing is incomplete, so this dialog cannot tell whether that name is free. Nothing was created.'
const DIALOG_DESCRIPTION =
  'The name gives the pack\u2019s id and its file name; the template is the runtime\u2019s own.'

/**
 * What Create would write, and where it came from.
 *
 * One value rather than two nullable ones, so everything below branches on the
 * same fact: a template is bytes the runtime served and a proposal is the
 * canonical frozen snapshot the run hook ingested, and the difference matters
 * exactly twice — which shaping function is called, and which sentence a
 * refusal gets.
 */
type Source = { kind: 'template'; text: string } | { kind: 'proposal'; document: unknown } | { kind: 'draft'; text: string }

/** How many cases a handover's matrix carries, for the sentences that say so. */
function caseCount(handover: ResearchHandover): number {
  const cases = (handover.matrix as { cases?: unknown[] })?.cases
  return Array.isArray(cases) ? cases.length : 0
}

export function CreatePackDialog({
  open,
  onOpenChange,
  onCreated,
  openerRef,
  presentation = 'dialog',
  onDirtyChange,
  onWritingChange,
  reviewDraft,
  onSaved,
  canCreate
}: {
  presentation?: 'dialog' | 'page' | 'review'
  reviewDraft?: { document: unknown; name: string; description: string; unknowns: string[]; research?: ResearchHandover }
  onSaved?: (pack: { id: string; path: string; digest: string }) => string | void | Promise<string | void>
  canCreate?: () => boolean
  onDirtyChange?: (dirty: boolean) => void
  onWritingChange?: (writing: boolean) => void
  open: boolean
  onOpenChange: (open: boolean) => void
  /**
   * Called once, after a pack has been created and the route changed.
   *
   * The rail below 900px is a **modal** drawer, and closing this dialog left
   * that drawer standing over the page it had just navigated to. Closing the
   * dialog is not closing the thing the dialog was inside, and only the rail
   * knows what that is.
   */
  onCreated?: () => void
  /** The control that opened this, so focus goes back to it on every exit. */
  openerRef?: RefObject<HTMLElement | null>
}) {
  const createHelpId = useId()
  const { config } = useEffectiveConfig()
  const { dir, idBase } = config.storage.packs
  const { known, exampleSupported, schemaSupported, validateSupported } = useMcp()
  const listing = useFileListing()
  const project = useFileContent(PROJECT_FILE)
  const examples = useExampleListing()
  // Asked as soon as the dialog is open rather than when Empty is picked: the
  // option cannot be offered until a skeleton has come out of it, so waiting
  // for the pick would mean the option never appears.
  const schema0 = useSchema(schemaSupported)
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const location = useLocation()
  /**
   * The **Describe it** section's own state, held here rather than inside it.
   *
   * Because closing this dialog is not the section's event: the run has to be
   * stopped through the run hook — one terminal event, one socket close — and
   * an unmount alone would abort an engine iterator with nothing left to write
   * a terminal event onto. `close` below is the one place that happens.
   */
  const describe = useDescribeIt()

  /**
   * A draft handed over from Research and draft, in the router's location
   * state: a reviewed document with its test cases and research record. It is
   * a proposal-shaped source — the name field still wins, the runtime still
   * validates the shaped bytes, and the two writes are the same — with two
   * companion files written beside the pack before the project entry names
   * them. Read per mount, and spent when a Create lands: step (3) takes the
   * handover off this history entry, so a handover is one press of Create.
   */
  const [handover] = useState<ResearchHandover | undefined>(() => {
    if (reviewDraft?.research) return reviewDraft.research
    const state = (location.state as { research?: ResearchHandover } | null)?.research
    return state && typeof state === 'object' && state.document !== undefined ? state : undefined
  })
  const [step, setStep] = useState(0)
  const [method, setMethod] = useState<'manual' | 'ai'>(handover ? 'ai' : 'manual')
  const [draft, setDraft] = useState<string | undefined>()
  const [unknowns, setUnknowns] = useState<readonly string[]>(reviewDraft?.unknowns ?? [])
  const [reviewedUnknowns, setReviewedUnknowns] = useState(false)
  const held = useHeldText(() => {})
  const inspector = useInspectorSlot()
  const guide = useInspectorPortal(presentation === 'page' ? <aside className={flow.guide}>
    <h2>Create a pack</h2>
    <p><strong>1. Basics</strong><br />Name the decision you want to make. Start with a runtime template, or ask your configured assistant for a draft.</p>
    <p><strong>2. Build</strong><br />Write a clear decision question and possible outcomes. Add decision rules, then the evidence and sources that support them.</p>
    <p><strong>3. Review</strong><br />Read the draft and its validation report before creating the file. AI suggestions still need your review.</p>
    <p>After creating, use Test to explore inputs and read how the pack reaches an outcome. The bottom Activity tab records operation progress.</p>
  </aside> : null)

  const [name, setName] = useState(reviewDraft?.name ?? handover?.name ?? '')
  const [description, setDescription] = useState(reviewDraft?.description ?? handover?.description ?? '')
  const [choice, setChoice] = useState<string | undefined>(undefined)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<{ lead: string; reason?: string } | undefined>(undefined)

  useEffect(() => { onDirtyChange?.(name !== '' || description !== '' || describe.typed !== '' || draft !== undefined) }, [name, description, describe.typed, draft, onDirtyChange])
  useEffect(() => { onWritingChange?.(busy) }, [busy, onWritingChange])

  useEffect(() => {
    if (draft === undefined) return
    const read = buffered(draft)
    held.update((pending) => {
      const next = new Map(pending)
      for (const [pointer, value] of pending) {
        if ((bytesAt(read, pointer) ?? '') !== value.from || ownerOf(read, pointer) !== value.owner) next.delete(pointer)
      }
      return next.size === pending.size ? pending : next
    })
  }, [draft, held.update])

  const offered = useMemo(() => examples.data?.examples ?? [], [examples.data])

  /**
   * What is known about the runtime's templates, as one of four states.
   *
   * These were one state before, and collapsing them is what made a slow
   * runtime look like a broken one. `[]` meant "pending", "refused" and "this
   * runtime carries none" indistinguishably; Empty was selected the instant
   * the capability flag said `get_schema` existed, before any schema had been
   * fetched, so the first thing a viewer saw was a template that might not
   * resolve. They are named here so every consumer below branches on the fact
   * rather than on an empty array.
   */
  const examplesState: 'unsupported' | 'pending' | 'error' | 'settled' = !exampleSupported
    ? 'unsupported'
    : examples.isPending
      ? 'pending'
      : examples.isError
        ? 'error'
        : 'settled'

  // The schema half of the same question. `useSchema` is enabled below only
  // once it is needed, so "pending" here means *asked and not yet answered*.
  const emptyReady = schemaSupported && schema0.isSuccess && emptyPackFrom(schema0.data) !== undefined
  const emptyState: 'unsupported' | 'pending' | 'error' | 'unusable' | 'ready' = !schemaSupported
    ? 'unsupported'
    : schema0.isPending
      ? 'pending'
      : schema0.isError
        ? 'error'
        : emptyReady
          ? 'ready'
          : 'unusable'

  /**
   * The offered options.
   *
   * **Empty appears only once a schema has actually produced a skeleton.** It
   * used to appear on the strength of the capability flag alone, which is a
   * claim about a tool existing rather than about a template existing — and a
   * skeleton with no `specVersion` is not an incomplete pack but a file nothing
   * can read as one.
   */
  /** The Describe section has a proposal this dialog could write. */
  const proposalOffered = describe.offered

  const options = useMemo(
    () => [
      ...(examplesState === 'settled'
        ? offered.map((entry) => ({ value: exampleValue(entry.name), label: entry.name }))
        : []),
      ...(emptyState === 'ready' ? [{ value: SCHEMA_EMPTY, label: EMPTY_LABEL }] : []),
      // Offered from the moment a run has finished, whether or not it produced
      // a document: a run that ended with nothing is a state this dialog has to
      // be able to *report*, and one whose option quietly never appeared would
      // leave a person staring at a template they did not choose.
      ...(proposalOffered ? [{ value: PROPOSAL_SOURCE, label: PROPOSAL_LABEL }] : [])
    ],
    [examplesState, emptyState, offered, proposalOffered]
  )

  /**
   * Nothing is selected while anything is still being asked.
   *
   * The default used to be `options[0]`, and with Empty appearing first on a
   * runtime whose example listing had not answered, that meant Empty was
   * selected — and then silently replaced when the examples arrived. A viewer
   * watching the field change under them is a worse answer than a field that
   * says it is waiting.
   */
  const templatesPending = examplesState === 'pending' || emptyState === 'pending'
  const selected = choice ?? (templatesPending ? undefined : options[0]?.value)
  const isEmpty = selected === SCHEMA_EMPTY
  const exampleName = selected === undefined ? undefined : exampleNameOf(selected)

  const example = useExample(exampleName)
  const schema = schema0

  /**
   * The proposal becomes the source the moment there is one to be.
   *
   * Somebody who described a policy and watched it be worked out did not then
   * choose a template, and leaving the field on one would make Create write the
   * wrong document on the first press. Switching back is one click, and the
   * proposal stays on offer until this dialog closes.
   */
  useEffect(() => {
    if (proposalOffered) setChoice(PROPOSAL_SOURCE)
  }, [proposalOffered])

  /**
   * And it stops being the source the moment it stops being on offer.
   *
   * Pressing Propose again, losing the assistant, closing the section's session
   * — each takes the proposal off the list, and a choice left pointing at it
   * would be a source the author cannot see and cannot change. Falling back to
   * the template choice is what the field says it is.
   */
  useEffect(() => {
    if (!proposalOffered) {
      setChoice((current) => (current === PROPOSAL_SOURCE ? undefined : current))
    }
  }, [proposalOffered])

  useEffect(() => {
    if (open) return
    setName('')
    setDescription('')
    setChoice(undefined)
    setBusy(false)
    setFailure(undefined)
  }, [open])

  const derived = slugFor(name)
  const slug = 'slug' in derived ? derived.slug : undefined
  const path = slug === undefined ? undefined : packPathFor(dir, slug)

  const taken = useMemo(() => {
    if (slug === undefined || path === undefined) return undefined
    return collisionIn(slug, { ...projectFacts(project.data?.content), files: pathsIn(listing.data), path })
  }, [slug, path, project.data, listing.data])

  // A template the runtime is still fetching is not a refusal, and one it
  // refused is: the two are kept apart so a slow answer never reads as a
  // failure.
  const usingProposal = selected === PROPOSAL_SOURCE
  const template =
    selected === undefined || usingProposal
      ? undefined
      : isEmpty
        ? emptyPackFrom(schema.data)
        : example.data
  const templateError = isEmpty ? schema.error : example.error

  /**
   * The document Create would write, or nothing.
   *
   * The proposal half is read off the run's own event list, which is where the
   * **canonical frozen snapshot** lives: `useAssistantRun` ingests a proposal
   * once, and this is that value rather than a second reading of an engine's
   * event.
   */
  const source: Source | undefined = draft !== undefined ? { kind: 'draft', text: draft } : reviewDraft !== undefined ? { kind: 'proposal', document: reviewDraft.document } : handover !== undefined
    ? { kind: 'proposal', document: handover.document }
    : usingProposal
    ? describe.proposal === undefined
      ? undefined
      : { kind: 'proposal', document: describe.proposal.document }
    : template === undefined
      ? undefined
      : { kind: 'template', text: template }

  /** Whether the proposal calls itself something other than what was typed. */
  const renamed =
    source?.kind === 'proposal' &&
    slug !== undefined &&
    namedOtherwise(source.document, { name, slug, idBase })

  /**
   * The bytes a proposal would be written as, computed **here** so they can be
   * checked before Create is offered.
   *
   * `shapePack` fills the four members the dialog asked about and leaves the
   * rest exactly as the model wrote them — `specVersion` included — because the
   * desk does not edit a document on a model's behalf. What that leaves is a
   * document nobody has checked: an unknown top-level member, a mistyped rule,
   * a format version a model invented. The runtime's schema is
   * `additionalProperties: false` and its `specVersion` is a `const`, so the
   * answer to "is this a pack" is the runtime's and is available for the
   * asking.
   */
  const proposed = useMemo((): { text: string } | { problem: string } | undefined => {
    if (source?.kind === 'draft') return { text: source.text }
    if (source?.kind !== 'proposal' || slug === undefined) return undefined
    try {
      return { text: packFromProposal(source.document, { name, description, slug, idBase }) }
    } catch (cause) {
      return { problem: reasonOf(cause) }
    }
  }, [source, name, description, slug, idBase])
  const shapedText = proposed !== undefined && 'text' in proposed ? proposed.text : undefined
  // The editor's own instrument: a call per keystroke is a call per keystroke,
  // so what is sent is a snapshot the field settles on, and `behind` is what
  // says the bytes on screen have moved past the ones that were checked.
  const idle = useIdleCheck(shapedText)
  const checked = useValidate(idle.checkedText)

  /**
   * Why a proposal cannot be written, in the runtime's words where they exist.
   *
   * Read in order. The last branch is the load-bearing one: **a proposal is
   * written only where the runtime called the exact bytes valid**, and the
   * comparison against `checkedBytes` is what makes "the bytes that were
   * checked" and "the bytes that would be written" one string rather than two.
   */
  const proposalRefusal: string | undefined =
    source?.kind !== 'proposal' && source?.kind !== 'draft'
      ? undefined
      : proposed === undefined || 'problem' in proposed
        ? (proposed?.problem ?? CHECKING)
        : !validateSupported
          ? NO_VALIDATE
          : checked.isError
            ? `The check on the proposed document was refused — ${checked.error.message}`
            : checked.data === undefined || checked.data.checkedBytes !== shapedText
              ? CHECKING
              : checked.data.report.status === 'valid'
                ? undefined
                : `The runtime will not call this document a pack — ${
                    layersReached(checked.data.report).text
                  }`

  /**
   * What to say under the Template field, and the four facts it is made of.
   *
   * Read in order, because they are answers to different questions and only
   * one of them is a refusal:
   *
   * 1. Something is still being asked. Not a problem, and said as a wait.
   * 2. The listing itself was refused. That is the runtime's own sentence and
   *    it used to be dropped on the floor: `examples.error` was never read, so
   *    a refused listing looked like a runtime carrying no examples.
   * 3. Nothing is offered, and the capability listing has actually **answered**
   *    — `known`, not `status === 'ready'`. A connection that is up but whose
   *    tool listing failed advertises nothing, and calling that "no template
   *    here" is a claim about the runtime made from the desk's own ignorance.
   * 4. A specific template was picked and could not be read.
   */
  const templateProblem = templatesPending
    ? undefined
    : examplesState === 'error'
      ? examples.error instanceof RuntimeRefusal && !examples.error.reported
        ? 'The runtime refused to list its examples.'
        : `The runtime refused to list its examples — ${examples.error?.message ?? ''}`
      : emptyState === 'error'
        ? schema.error instanceof RuntimeRefusal && !schema.error.reported
          ? 'The runtime refused to serve its schema.'
          : `The runtime refused to serve its schema — ${schema.error?.message ?? ''}`
        : options.length === 0
          ? known
            ? NO_TEMPLATE
            : undefined
          : templateError
            ? // The runtime's own sentence where it gave one. Where it refused
              // without saying why, this dialog says less rather than putting
              // the name of a tool call in front of somebody creating a pack.
              templateError instanceof RuntimeRefusal && !templateError.reported
              ? 'This template could not be read.'
              : `This template could not be read — ${templateError.message}`
            : undefined

  const nameProblem = name.trim() === '' ? undefined : 'problem' in derived ? derived.problem : taken

  // A listing that failed is not a project with no files in it. `retry: false`
  // means one failed request is the final answer, so this is said as soon as it
  // is known rather than discovered by pressing a button — and `ready` requires
  // the listing to have *succeeded*, so "this project has no jpack.json" is
  // only ever said about a project whose files this dialog actually read.
  /**
   * An incomplete listing is not a project this dialog has read.
   *
   * `FileListing.partial` means `files` is *not all of them* — an unreadable
   * subtree, a tree past the walk's budget. Every question this dialog asks the
   * listing ("is jpack.json there", "is something already at that path") is a
   * question about absence, and absence is exactly what a partial answer cannot
   * establish. It used to be ignored, so a project whose `packs/` could not be
   * walked could be told it had no `jpack.json`.
   */
  const partial = (listing.data?.partial ?? []).length > 0

  const blocked = listing.isError
    ? { lead: UNREADABLE_PROJECT, reason: reasonOf(listing.error) }
    : partial
      ? { lead: PARTIAL_PROJECT, reason: (listing.data?.partial ?? []).join(', ') }
      : undefined

  const ready =
    (canCreate?.() ?? true) &&
    slug !== undefined &&
    taken === undefined &&
    source !== undefined &&
    proposalRefusal === undefined &&
    !busy &&
    held.drafts.size === 0 &&
    (unknowns.length === 0 || reviewedUnknowns) &&
    describe.blocking === '' &&
    listing.isSuccess &&
    !partial

  /**
   * Why Create is not offered, in the control's own `title`.
   *
   * Whatever the Describe section is holding the dialog for, in its own words:
   * a submission in flight, a refused prompt, a run that failed, a proposal an
   * error withdrew, a document that could not be read as JSON data. It holds
   * the dialog rather than the proposal source, because a Create that fell back
   * to a template one press after somebody asked for something else would write
   * a document nobody chose.
   */
  const createWhy = describe.blocking !== '' ? describe.blocking : proposalRefusal

  /**
   * The report behind a refusal, where the refusal is the runtime's.
   *
   * Only where the check answered about the bytes that would be written and
   * called them something other than valid: a pending check and a refused call
   * have a sentence and no diagnostics to print.
   */
  const refused =
    proposalRefusal !== undefined &&
    checked.data !== undefined &&
    checked.data.checkedBytes === shapedText &&
    checked.data.report.status !== 'valid'
      ? checked.data.report
      : undefined

  const invalidate = (keys: readonly (readonly unknown[])[]) => {
    for (const key of keys) void queryClient.invalidateQueries({ queryKey: key })
  }

  /**
   * **Every way this dialog closes goes through here**, and closing ends the
   * session the Describe section was running.
   *
   * The rail unmounts this component when it closes, so an effect on `open`
   * would never run: the stop has to happen on the way out rather than after
   * it. It goes through the run hook — which writes the run's one terminal
   * event and closes its one connection — rather than leaving the unmount to
   * abort an iterator nobody is reading. The proposal goes with it: nothing
   * about a session is persisted, and a dialog that reopened one would be
   * re-offering a document nobody accepted.
   */
  const close = (next: boolean) => {
    if (!next && presentation !== 'page') describe.discard()
    onOpenChange(next)
  }

  const create = async () => {
    if (!ready || slug === undefined || path === undefined || source === undefined) return
    setFailure(undefined)
    setBusy(true)
    recordActivity('Creating pack…')
    let completed = false
    try {
      // (0a) The configuration as it is now, read directly and **first**.
      //
      // The cached listing used to answer "is there a `jpack.json`" before
      // this ran, which is a question about absence asked of a source that
      // cannot establish absence: a listing is a snapshot, and a partial one is
      // explicitly not all the files. So the read itself decides, and only its
      // own 404 means the project has no configuration. Everything else — a
      // permission refusal, a socket that never answered — is "could not be
      // read", which is a different sentence and a different fix.
      const files = pathsIn(listing.data)
      let read: FileContent
      let current: ProjectConfig
      try {
        read = await readFile(PROJECT_FILE)
        current = parseProjectConfig(read.content)
      } catch (cause) {
        const absent = cause instanceof FileRequestError && cause.status === 404
        setFailure(
          absent
            ? { lead: NO_PROJECT_FILE }
            : { lead: UNREADABLE_PROJECT_FILE, reason: reasonOf(cause) }
        )
        return
      }
      const clash = collisionIn(slug, {
        keys: existingPackKeys(current),
        paths: existingPackPaths(current),
        files,
        path
      })
      if (clash !== undefined) {
        setFailure({ lead: clash })
        return
      }

      // (0b) And the file itself, asked directly.
      //
      // The listing is a snapshot and the paths in it are spellings; this is
      // the one question with an authoritative answer available, so it is
      // asked. A 404 is the only answer that means "nothing is there" —
      // anything else is a path this desk cannot write and should not try to,
      // and the write's own refusal would arrive after the point of no return
      // for the second write.
      try {
        await readFile(path)
        setFailure({ lead: PACK_FILE_TAKEN })
        return
      } catch (cause) {
        if (!(cause instanceof FileRequestError) || cause.status !== 404) {
          setFailure({
            lead: refusalLead(cause) ?? 'That location could not be used.',
            reason: refusalDetail(cause)
          })
          return
        }
      }

      // (0b') And the two files a handover writes beside the pack, on the same
      // terms. The pack write is the point of no return for them: a companion
      // path that is occupied, or that another entry already declares as its
      // matrix, is knowable now and is an orphaned pack if it is found out
      // after. Nothing derives these paths but this rule, so they are derived
      // here from the same slug the pack path came from.
      if (handover !== undefined) {
        const base = path.replace(/\.pack\.json$/, '')
        const claimed = declaredMatrixPaths(current)
        for (const companion of [`${base}.matrix.json`, `${base}.research.json`]) {
          if (claimed.some((declared) => samePath(declared, companion))) {
            setFailure({ lead: COMPANION_PATH_TAKEN, reason: `${companion} is already declared as another pack's matrix.` })
            return
          }
          try {
            await readFile(companion)
            setFailure({ lead: COMPANION_PATH_TAKEN, reason: `${companion} already exists.` })
            return
          } catch (cause) {
            if (!(cause instanceof FileRequestError) || cause.status !== 404) {
              setFailure({ lead: refusalLead(cause) ?? 'That location could not be used.', reason: refusalDetail(cause) })
              return
            }
          }
        }
      }

      // (0c) The document itself, before anything is sent: a template that is
      // not a JSON object cannot become a pack, and finding that out after the
      // write would be an orphan for a reason known in advance.
      // **A proposal is written as the bytes the runtime checked**, read back
      // off the check's own answer rather than shaped a second time: two
      // shapings are two documents to a reader even where they are one string,
      // and the claim being made is that what was validated is what was
      // written. A template is the runtime's own document and is shaped here as
      // it always was.
      let content: string
      if (source.kind === 'proposal' || source.kind === 'draft') {
        const validated = checked.data
        if (validated === undefined || validated.checkedBytes !== shapedText) {
          setFailure({ lead: PROPOSAL_UNUSABLE, reason: CHECKING })
          return
        }
        content = validated.checkedBytes
      } else {
        try {
          content = shapeTemplate(source.text, { name, description, slug, idBase })
        } catch (cause) {
          setFailure({ lead: TEMPLATE_UNUSABLE, reason: reasonOf(cause) })
          return
        }
      }

      // (1) The pack itself, and **what the chassis says it wrote**.
      //
      // The answer is a read-back from the disk after the rename, and its
      // `path` is the canonical spelling the chassis resolved the request to.
      // Discarding it and registering the requested spelling is how an entry
      // ends up naming a path the runtime cleans to something else — the same
      // aliasing defect the collision check had, arriving from the other side.
      if (canCreate && !canCreate()) { setFailure({ lead: 'The draft changed during review. Return to the conversation and review it again.' }); return }
      let landed: FileContent
      try {
        landed = await writeFile({ path, content, baseSha256: '', createParents: true })
      } catch (cause) {
        setFailure({
          // The chassis' stable code chooses a sentence about creating a pack;
          // its own words go underneath. "The directory packs does not exist in
          // the project; create it first" is a good sentence for an editor and
          // the wrong one to put in front of somebody who typed a name.
          lead: refusalLead(cause) ?? 'The pack could not be created.',
          reason: refusalDetail(cause)
        })
        invalidate([['desk-files']])
        return
      }

      // (1b) A handed-over draft's companions, beside the pack and before the
      // entry names them: the rows document every established case became,
      // and the research record — every source, receipt, excerpt and
      // verification state — with the digest the chassis reported for the
      // pack it describes. A companion that could not be written leaves the
      // pack on disk and nothing naming it, and the dialog says exactly that.
      let matrixPath: string | undefined
      if (handover !== undefined) {
        const base = landed.path.replace(/\.pack\.json$/, '')
        matrixPath = `${base}.matrix.json`
        const researchPath = `${base}.research.json`
        const research = { ...(handover.research as Record<string, unknown>), packSha256: landed.sha256 }
        // Every file that landed is named if the next one does not: "the files
        // left behind" is the whole of what a person can act on here, and the
        // matrix is one of them once it is written.
        const written = [landed.path]
        try {
          await writeFile({ path: matrixPath, content: JSON.stringify(handover.matrix, null, 2) + '\n', baseSha256: '' })
          written.push(matrixPath)
          await writeFile({ path: researchPath, content: JSON.stringify(research, null, 2) + '\n', baseSha256: '' })
        } catch (cause) {
          setFailure({ lead: COMPANIONS_ORPHANED, reason: `${written.join(' and ')} ${written.length === 1 ? 'is' : 'are'} on disk and unregistered. ${refusalDetail(cause) ?? ''}`.trim() })
          invalidate([['desk-files']])
          return
        }
      }

      // (2) The entry, naming the file that was actually written, against the
      // digest the read in (0a) returned.
      try {
        await writeFile({
          path: PROJECT_FILE,
          content: serialiseProjectConfig(
            read.content,
            withPack(current, slug, matrixPath === undefined
              ? packEntryFor(landed.path, description)
              : { ...packEntryFor(landed.path, description), matrix: matrixPath })
          ),
          baseSha256: read.sha256
        })
      } catch (cause) {
        setFailure({
          lead: ORPHANED,
          // A conflict here is the one refusal with a fix worth naming —
          // reload and try again. Everything else keeps the chassis' own
          // words, because the lead already says what happened and the detail
          // is the only place the *why* survives.
          reason: codeOf(cause) === 'stale' ? STALE_PROJECT_FILE : refusalDetail(cause)
        })
        invalidate([['desk-files'], ['desk-file', PROJECT_FILE]])
        return
      }

      const destination = await onSaved?.({ id: slug, path, digest: landed.sha256 })

      // (3) Everything that answered before this pack existed.
      invalidate([['desk-files'], ['desk-file', PROJECT_FILE], ['list_packs'], ['desk-config']])
      completed = true
      recordActivity('Pack created and registered.')
      if (presentation === 'page' || presentation === 'review') {
        describe.discard()
        onCreated?.()
      } else close(false)
      // The handover is spent, and the history entry has to say so. It rode in
      // on this entry's router state and stays there through the push below, so
      // a Back re-presents the same reviewed document: renamed, it writes a
      // second pack carrying the first one's matrix rows and research record —
      // one run's receipts and verified excerpts standing behind two packs as
      // though each had earned its own. Replaced rather than dropped on the way
      // out, because only the entry being left can still be rewritten.
      //
      // The entry rewritten is the one this mount rendered on — `location` as
      // it was when Create was pressed, four awaited writes back. That it is
      // still the entry on top is a premise, not a check: nothing on this page
      // pushes, and the desk's other writers of a search or a fragment replace
      // (`PackView`). Move history under a create in flight and this stamps
      // the create page's URL over the entry that moved there instead.
      // Chat-owned review data is consumed by the saved pack binding. Only
      // a router-state handover owns an entry to rewrite here.
      if (handover !== undefined && reviewDraft?.research === undefined) {
        const rest: Record<string, unknown> = { ...(location.state as Record<string, unknown> | null) }
        delete rest.research
        navigate(`${location.pathname}${location.search}${location.hash}`, {
          replace: true,
          state: Object.keys(rest).length === 0 ? null : rest
        })
      }
      navigate(destination ?? `/packs/${slug}`)
      // Closing this dialog is not closing the thing it was inside. Below
      // 900px the rail is a modal drawer, and it stayed over the page this
      // just navigated to.
      if (presentation === 'dialog') onCreated?.()
    } finally {
      if (!completed) recordActivity('Pack creation stopped. See the creation page for details.')
      setBusy(false)
    }
  }

  /**
   * **A route change closes this dialog**, and closing it ends the session.
   *
   * This dialog is mounted by the rail, which sits above the route's own
   * `<Routes>`: a browser Back, a Forward, or any programmatic navigation
   * changes the page underneath without unmounting the dialog or telling it
   * anything. A run would go on running over a page it has nothing to do with,
   * with no terminal event and no connection close, because only a Radix
   * dismissal and a successful Create ever reached `close`.
   *
   * A navigation is a dismissal like Escape, so it takes the same path. The one
   * exception is the one the dismissal handler already makes: while the create
   * sequence is running this dialog is the only place its outcome is reported,
   * and it stays to report it.
   *
   * **The page is the pathname and the search, and a fragment is not either.**
   * The obvious key is `location.key`, and it moves for a hash-only history
   * entry too — which this desk defines as *not* a navigation, in as many words
   * (`MemberOutline`: selecting a member writes a hash and stays on the page).
   * Keyed on that, choosing a member in the document behind an open dialog
   * would have closed it, stopped the run and discarded the proposal, over a
   * page that had not changed.
   */
  const closeNow = useRef(close)
  closeNow.current = close
  const page = `${location.pathname}${location.search}`
  const shownAt = useRef(page)
  useEffect(() => {
    if (presentation !== 'dialog') return
    if (!open) {
      shownAt.current = page
      return
    }
    if (page === shownAt.current) return
    shownAt.current = page
    if (busy) return
    closeNow.current(false)
  }, [page, open, busy, presentation])

  if (presentation === 'review') return <form className={flow.flow} noValidate onSubmit={event => { event.preventDefault(); void create() }}>
    <div className={flow.intro}><h2>Create this pack</h2><p className={flow.hint}>Choose its name and review the remaining questions. Your conversation stays with the pack.</p></div>
    <FieldGroup>
      <Field label="Pack name" error={nameProblem} hint={path ? `Save to ${path}` : undefined}>{wiring => <Input {...wiring} required value={name} disabled={busy} onChange={event => setName(event.target.value)} />}</Field>
      <Field label="Description">{wiring => <TextArea {...wiring} rows={2} value={description} disabled={busy} onChange={event => setDescription(event.target.value)} />}</Field>
    </FieldGroup>
    {unknowns.length > 0 && <section className={flow.summary}>
      <ProposalUnknowns unknowns={unknowns} />
      <label><input type="checkbox" checked={reviewedUnknowns} disabled={busy} onChange={event => setReviewedUnknowns(event.target.checked)} /> I reviewed these open questions and assumptions.</label>
    </section>}
    <p className={flow.hint}>{handover ? `${caseCount(handover)} checked cases and the research record will be saved with this pack.` : 'The structure is validated. Source research and behavioral testing have not been performed.'}</p>
    {createWhy && <p role="status">{createWhy}</p>}
    {refused && <DiagnosticList diagnostics={anchor(refused, new Set())} label="Validation details" />}
    {(failure ?? blocked) && <Alert reason={(failure ?? blocked)!.reason}>{(failure ?? blocked)!.lead}</Alert>}
    <div className={flow.actions}><Button disabled={busy} onClick={() => close(false)}>Back to draft</Button><Button variant="primary" type="submit" disabled={!ready}>{busy ? 'Creating…' : 'Create pack'}</Button></div>
  </form>

  if (presentation === 'page') {
    const next = () => {
      if (step === 0) {
        if (slug === undefined || taken !== undefined || source === undefined || describe.blocking !== '') return
        try {
          if (draft === undefined) setDraft(source.kind === 'proposal'
            ? packFromProposal(source.document, { name, description, slug, idBase })
            : shapeTemplate(source.text, { name, description, slug, idBase }))
          if (source.kind === 'proposal') {
            setUnknowns(handover?.unknowns ?? describe.proposal?.unknowns ?? [])
            describe.discard()
          }
          setStep(1)
        } catch (cause) { setFailure({ lead: TEMPLATE_UNUSABLE, reason: reasonOf(cause) }) }
      } else if (held.drafts.size === 0) setStep(2)
    }
    const preview = draft === undefined ? undefined : buffered(draft).index.value
    return <>
      {guide}
      <PageHeader title="Packs" context="Create pack" actions={<Button onClick={() => inspector.reveal()}>Guide</Button>} />
      <PageBody width="form">
        <form noValidate className={flow.flow} onSubmit={(event) => { event.preventDefault(); if (step === 2) void create(); else next() }}>
          <ol className={flow.steps} aria-label="Creation steps">
            {['Basics', 'Build', 'Review'].map((label, index) => <li key={label} aria-current={step === index ? 'step' : undefined}>{index + 1}. {label}</li>)}
          </ol>
          <div className={flow.intro}><h2>{['Create a pack', 'Build your decision', 'Review your pack'][step]}</h2>
            <p className={flow.hint}>{['Start manually or draft with your assistant.', 'Define the rules, outcomes, and supporting evidence.', 'Check the content and runtime validation before creating.'][step]}</p>
          </div>
          {step === 0 && <>
            {handover === undefined && <><div><SegmentedControl label="Creation method" value={method} onValueChange={(next) => {
              if (next === 'manual') { setMethod('manual'); describe.discard(); setChoice(undefined) }
              else setMethod('ai')
            }} segments={[
              { value: 'manual', label: 'Manual', disabled: draft !== undefined || describe.running },
              { value: 'ai', label: 'Draft with Assistant', disabled: draft !== undefined || !describe.usable || !describe.advertised || describe.picked.model === '' }
            ]} /></div>
            {!describe.usable && <p className={flow.hint}>AI drafting is unavailable. Configure the assistant in Admin to enable it. {describe.unusableBecause}</p>}
            {describe.usable && !describe.advertised && <p className={flow.hint}>This runtime does not offer the authoring prompt required for AI drafting.</p>}
            {describe.usable && describe.advertised && describe.picked.model === '' && <p className={flow.hint}>Choose an enabled model in Admin → Assistant to use AI drafting.</p>}
            </>}
            <FieldGroup>
            <Field label="Name (required)" hint={slug === undefined ? undefined : `id: ${slug}`} error={nameProblem}>
              {(wiring) => <Input {...wiring} autoFocus required value={name} disabled={draft !== undefined} onChange={(event) => setName(event.target.value)} />}
            </Field>
            <Field label="Description" hint="What decision does this pack help someone make?">
              {(wiring) => <TextArea {...wiring} rows={3} value={description} disabled={draft !== undefined} onChange={(event) => setDescription(event.target.value)} />}
            </Field>
            {method === 'manual' ? <Field label="Starting template" error={templateProblem}>
              {(wiring) => <Select {...wiring} value={selected} onValueChange={setChoice} disabled={draft !== undefined} options={options} placeholder={templatesPending ? TEMPLATES_PENDING : 'Choose a template'} />}
            </Field> : draft === undefined ? handover !== undefined ? <p className={flow.hint} role="status">This draft came from Research and draft: {caseCount(handover)} test case(s) and its research record will be written beside the pack. The draft itself is not edited here.</p> : <>
              <DescribeIt state={describe} blockingElsewhere={Boolean(createWhy)} expanded />
              <p className={flow.hint}>Or research first: <ButtonLink to="/create-pack/research" variant="inline">Research and draft with sources</ButtonLink> lets the assistant search and read official pages through the gateway, cite them, and test the draft before you create it.</p>
            </> : null}
            </FieldGroup>
            {draft !== undefined && <p className={flow.hint}>Your draft is retained. Edit its name, description, and other fields in Build → Full document.</p>}
          </>}
          {/*
            * A handed-over draft is read at Build, not edited. Its matrix and
            * research record assert that these exact bytes were checked against
            * source-grounded cases, and an edit here would leave those
            * companions describing a document that no longer exists while the
            * record's digest named the edited one. Editing a pack is what the
            * pack editor is for, once the pack exists and carries its own
            * history; changing what the draft says is what Research is for.
            */}
          {step === 1 && draft !== undefined && (handover !== undefined
            ? <section className={flow.summary} aria-label="Reviewed draft">
                <p className={flow.hint} role="status">This is the reviewed draft, as its {caseCount(handover)} test case(s) checked it. It is not edited here: go back to Research and draft to change what it says, or create the pack and edit it afterwards.</p>
                {isRecord(preview) && <PackOverview document={preview as unknown as PackDocument} />}
              </section>
            : <DraftPackEditor text={draft} onChange={(next) => { setDraft(next); setReviewedUnknowns(false) }} pending={held.drafts} hold={held.hold} />)}
          {step === 2 && unknowns.length > 0 && <section className={flow.summary} aria-label="Assistant review">
            <ProposalUnknowns unknowns={unknowns} />
            <label><input type="checkbox" checked={reviewedUnknowns} onChange={(event) => setReviewedUnknowns(event.target.checked)} /> I reviewed these unknowns and updated the draft where needed.</label>
          </section>}
          {step === 2 && isRecord(preview) && <PackOverview document={preview as unknown as PackDocument} />}
          {step > 0 && <section className={flow.summary} aria-label="Draft validation">
            <h3>Structure check</h3>
            <p id={createWhy === proposalRefusal ? createHelpId : undefined} role="status">{proposalRefusal ?? 'The runtime validated this draft. This does not mean its rules have passed tests.'}</p>
            {refused !== undefined && <DiagnosticList diagnostics={anchor(refused, new Set())} label="What the runtime said about this document" />}
            {refused !== undefined && truncationNote(refused) !== undefined && <p>{truncationNote(refused)}</p>}
            {held.drafts.size > 0 && <p>Finish or clear the incomplete field values before continuing.</p>}
          </section>}
          {(failure ?? blocked) && <Alert reason={(failure ?? blocked)!.reason}>{(failure ?? blocked)!.lead}</Alert>}
          {busy && <p role="status">Creating and registering the pack. Stay on this page until it finishes.</p>}
          {createWhy && (step === 0 || createWhy !== proposalRefusal) && <p id={createHelpId} className={flow.hint}>{createWhy}</p>}
          <div className={flow.actions}>
            <Button variant="quiet" disabled={busy} onClick={() => close(false)}>Cancel</Button>
            <div>
              {step > 0 && <Button disabled={busy} onClick={() => setStep(step - 1)}>Back</Button>}
              <Button variant="primary" type="submit" disabled={step === 2 ? !ready : step === 0 ? slug === undefined || taken !== undefined || source === undefined || describe.blocking !== '' || (method === 'ai' && source?.kind !== 'proposal' && draft === undefined) : held.drafts.size > 0} aria-describedby={step === 2 && createWhy ? createHelpId : undefined}>
                {busy ? 'Creating…' : step === 2 ? 'Create pack' : step === 1 ? 'Review pack' : 'Continue'}
              </Button>
            </div>
          </div>
        </form>
      </PageBody>
    </>
  }

  return (
    <Dialog
      open={open}
      // Escape and the overlay are dismissals like any other, and the rail
      // unmounts this component when it closes. A dismissal mid-sequence would
      // take the only place the outcome is reported off the screen — including
      // the one outcome that leaves something behind.
      onOpenChange={(next) => {
        if (!next && busy) return
        close(next)
      }}
      title="Create a pack"
      description={DIALOG_DESCRIPTION}
      openerRef={openerRef}
    >
      <form
        onSubmit={(event) => {
          event.preventDefault()
          void create()
        }}
      >
        <Field
          label="Name (required)"
          hint={slug === undefined ? undefined : `id: ${slug}`}
          error={nameProblem}
        >
          {(wiring) => (
            <Input
              {...wiring}
              autoFocus
              required
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          )}
        </Field>

        <Field label="Description">
          {(wiring) => (
            <TextArea
              {...wiring}
              rows={3}
              value={description}
              onChange={(event) => setDescription(event.target.value)}
            />
          )}
        </Field>

        {/* No hint under this field. The one that was here said checks would
            report an empty pack incomplete — a disclaimer, and a verdict the
            shell derived without asking the runtime. The runtime reports the
            document's status on the page this opens, which is the thing
            entitled to. */}
        <Field label="Template" error={templateProblem}>
          {(wiring) => (
            <Select
              {...wiring}
              value={selected}
              onValueChange={setChoice}
              options={options}
              placeholder={templatesPending ? TEMPLATES_PENDING : '—'}
            />
          )}
        </Field>

        {renamed && <p className="quiet">{RENAMED}</p>}
        {proposalRefusal !== undefined && <p id={createWhy === proposalRefusal ? createHelpId : undefined} className="quiet">{proposalRefusal}</p>}
        {/*
          **Every diagnostic the runtime returned, as it wrote them.** Not the
          first, and not reworded: a runtime reporting independent errors at two
          members is describing two problems, and this is the one place they can
          be read — the page that would have shown the rest is the page this
          refusal prevents from existing. The rendering is the Checks panel's
          own, so the words are the same words wherever a diagnostic is printed.
          `anchor` is given an empty set of rendered pointers because nothing of
          the document is on screen here, which makes every `named` the
          diagnostic's own `instancePath`.
        */}
        {refused !== undefined && (
          <>
            <DiagnosticList
              diagnostics={anchor(refused, new Set())}
              label="What the runtime said about this document"
            />
            {truncationNote(refused) !== undefined && (
              <p className="quiet">{truncationNote(refused)}</p>
            )}
          </>
        )}

        <DescribeIt state={describe} blockingElsewhere={Boolean(createWhy)} />

        {(failure ?? blocked) && (
          <Alert reason={(failure ?? blocked)!.reason}>{(failure ?? blocked)!.lead}</Alert>
        )}

        {createWhy && createWhy !== proposalRefusal && <p id={createHelpId} className="quiet">{createWhy}</p>}
        <DialogActions>
          <DialogClose asChild>
            <Button variant="secondary" disabled={busy}>
              Cancel
            </Button>
          </DialogClose>
          <Button variant="primary" type="submit" disabled={!ready} aria-describedby={createWhy ? createHelpId : undefined}>
            Create pack
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  )
}

/** The paths a listing reports, or none where it has not answered. */
function pathsIn(listing: { files: { path: string }[] } | undefined): string[] {
  return (listing?.files ?? []).map((file) => file.path)
}

/**
 * The keys and paths the cached configuration names, for the live refusal
 * under the field.
 *
 * Best effort, and only that: an unreadable or unparseable file yields nothing
 * here, because a name is not wrong because a file elsewhere is — and because
 * the create sequence reads that file itself and refuses on what it finds.
 * This is the hint; step (0b) is the answer.
 */
function projectFacts(text: string | undefined): { keys: string[]; paths: string[] } {
  if (text === undefined) return { keys: [], paths: [] }
  try {
    const config = parseProjectConfig(text)
    return { keys: existingPackKeys(config), paths: existingPackPaths(config) }
  } catch {
    return { keys: [], paths: [] }
  }
}

/**
 * Whether a proposal calls itself something other than what the field says.
 *
 * Read off the document rather than off anything the model said about its own
 * work, and compared with what the shaping will actually write. Where the two
 * differ the dialog says so in one line: the desk's shaping wins either way,
 * and a document quietly arriving under a name nobody chose is worse than a
 * sentence saying which name won.
 */
function namedOtherwise(
  document: unknown,
  fields: { name: string; slug: string; idBase: string }
): boolean {
  if (typeof document !== 'object' || document === null) return false
  const held = document as { id?: unknown; title?: unknown }
  return held.title !== fields.name.trim() || held.id !== `${fields.idBase}${fields.slug}`
}

/** The message the failure carries, never a sentence invented over it. */
function reasonOf(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}
