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
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState, type RefObject } from 'react'
import { useNavigate } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { FileRequestError, readFile, writeFile, type FileContent } from '../files/client'
import { useFileContent, useFileListing } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { RuntimeRefusal, useExample, useExampleListing, useSchema } from '../mcp/starters'
import {
  existingPackKeys,
  existingPackPaths,
  packEntryFor,
  parseProjectConfig,
  serialiseProjectConfig,
  withPack,
  type ProjectConfig
} from '../packs/jpackConfig'
import { codeOf, refusalDetail, refusalLead } from '../packs/createRefusal'
import { collisionIn, emptyPackFrom, packPathFor, shapeTemplate, slugFor } from '../packs/newPack'
import { Alert } from '../ui/Alert'
import { Button } from '../ui/Button'
import { Dialog, DialogActions, DialogClose } from '../ui/Dialog'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { TextArea } from '../ui/TextArea'

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
const ORPHANED =
  'The pack was created but could not be registered. Nothing else was changed.'
const NO_TEMPLATE = 'There is no template to start from here.'
const TEMPLATES_PENDING = 'Asking the runtime what it can start from…'
const PARTIAL_PROJECT =
  'This project\u2019s file listing is incomplete, so this dialog cannot tell whether that name is free. Nothing was created.'
const DIALOG_DESCRIPTION =
  'The name gives the pack\u2019s id and its file name; the template is the runtime\u2019s own.'

export function CreatePackDialog({
  open,
  onOpenChange,
  onCreated,
  openerRef
}: {
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
  const { config } = useEffectiveConfig()
  const { dir, idBase } = config.storage.packs
  const { known, exampleSupported, schemaSupported } = useMcp()
  const listing = useFileListing()
  const project = useFileContent(PROJECT_FILE)
  const examples = useExampleListing()
  // Asked as soon as the dialog is open rather than when Empty is picked: the
  // option cannot be offered until a skeleton has come out of it, so waiting
  // for the pick would mean the option never appears.
  const schema0 = useSchema(schemaSupported)
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [choice, setChoice] = useState<string | undefined>(undefined)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<{ lead: string; reason?: string } | undefined>(undefined)

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
  const options = useMemo(
    () => [
      ...(examplesState === 'settled'
        ? offered.map((entry) => ({ value: exampleValue(entry.name), label: entry.name }))
        : []),
      ...(emptyState === 'ready' ? [{ value: SCHEMA_EMPTY, label: EMPTY_LABEL }] : [])
    ],
    [examplesState, emptyState, offered]
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
  const template = selected === undefined ? undefined : isEmpty ? emptyPackFrom(schema.data) : example.data
  const templateError = isEmpty ? schema.error : example.error

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
    slug !== undefined &&
    taken === undefined &&
    template !== undefined &&
    !busy &&
    listing.isSuccess &&
    !partial

  const invalidate = (keys: readonly (readonly unknown[])[]) => {
    for (const key of keys) void queryClient.invalidateQueries({ queryKey: key })
  }

  const create = async () => {
    if (!ready || slug === undefined || path === undefined || template === undefined) return
    setFailure(undefined)
    setBusy(true)
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

      // (0c) The document itself, before anything is sent: a template that is
      // not a JSON object cannot become a pack, and finding that out after the
      // write would be an orphan for a reason known in advance.
      let content: string
      try {
        content = shapeTemplate(template, { name, description, slug, idBase })
      } catch (cause) {
        setFailure({ lead: 'This template could not be used.', reason: reasonOf(cause) })
        return
      }

      // (1) The pack itself, and **what the chassis says it wrote**.
      //
      // The answer is a read-back from the disk after the rename, and its
      // `path` is the canonical spelling the chassis resolved the request to.
      // Discarding it and registering the requested spelling is how an entry
      // ends up naming a path the runtime cleans to something else — the same
      // aliasing defect the collision check had, arriving from the other side.
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

      // (2) The entry, naming the file that was actually written, against the
      // digest the read in (0a) returned.
      try {
        await writeFile({
          path: PROJECT_FILE,
          content: serialiseProjectConfig(
            read.content,
            withPack(current, slug, packEntryFor(landed.path, description))
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

      // (3) Everything that answered before this pack existed.
      invalidate([['desk-files'], ['desk-file', PROJECT_FILE], ['list_packs'], ['desk-config']])
      onOpenChange(false)
      navigate(`/packs/${slug}`)
      // Closing this dialog is not closing the thing it was inside. Below
      // 900px the rail is a modal drawer, and it stayed over the page this
      // just navigated to.
      onCreated?.()
    } finally {
      setBusy(false)
    }
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
        onOpenChange(next)
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

        {(failure ?? blocked) && (
          <Alert reason={(failure ?? blocked)!.reason}>{(failure ?? blocked)!.lead}</Alert>
        )}

        <DialogActions>
          <DialogClose asChild>
            <Button variant="secondary" disabled={busy}>
              Cancel
            </Button>
          </DialogClose>
          <Button variant="primary" type="submit" disabled={!ready}>
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

/** The message the failure carries, never a sentence invented over it. */
function reasonOf(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}
