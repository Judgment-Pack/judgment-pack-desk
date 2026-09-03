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
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { StaleWrite, readFile, writeFile, type FileContent } from '../files/client'
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
import { collisionIn, emptyPackFrom, packPathFor, shapeTemplate, slugFor } from '../packs/newPack'
import { Alert } from '../ui/Alert'
import { Button } from '../ui/Button'
import { Dialog, DialogActions, DialogClose } from '../ui/Dialog'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { TextArea } from '../ui/TextArea'

const PROJECT_FILE = 'jpack.json'
const EMPTY = 'empty'
const EMPTY_LABEL = 'Empty pack'

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
const EMPTY_IS_A_START =
  'An empty pack is a start, not a finished one: checks report it incomplete until you fill it in.'

export function CreatePackDialog({
  open,
  onOpenChange
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { config } = useEffectiveConfig()
  const { dir, idBase } = config.storage.packs
  const { exampleSupported, schemaSupported } = useMcp()
  const listing = useFileListing()
  const project = useFileContent(PROJECT_FILE)
  const examples = useExampleListing()
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [choice, setChoice] = useState<string | undefined>(undefined)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<{ lead: string; reason?: string } | undefined>(undefined)

  const offered = useMemo(() => examples.data?.examples ?? [], [examples.data])

  // The runtime's own examples in the runtime's own order, then the empty
  // pack — which is offered only where there is a schema to derive it from,
  // because a skeleton with no `specVersion` is not an incomplete pack but a
  // file that cannot be read as one.
  const options = useMemo(
    () => [
      ...(exampleSupported ? offered.map((entry) => ({ value: entry.name, label: entry.name })) : []),
      ...(schemaSupported ? [{ value: EMPTY, label: EMPTY_LABEL }] : [])
    ],
    [exampleSupported, schemaSupported, offered]
  )

  const selected = choice ?? options[0]?.value
  const isEmpty = selected === EMPTY

  const example = useExample(isEmpty || selected === undefined ? undefined : selected)
  const schema = useSchema(isEmpty)

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
  const templateProblem =
    options.length === 0
      ? NO_TEMPLATE
      : templateError
        ? // The runtime's own sentence where it gave one. Where it refused
          // without saying why, this dialog says less rather than putting the
          // name of a tool call in front of somebody creating a pack.
          templateError instanceof RuntimeRefusal && !templateError.reported
          ? 'This template could not be read.'
          : `This template could not be read — ${templateError.message}`
        : isEmpty && schema.data !== undefined && template === undefined
          ? NO_TEMPLATE
          : undefined

  const nameProblem = name.trim() === '' ? undefined : 'problem' in derived ? derived.problem : taken

  // A listing that failed is not a project with no files in it. `retry: false`
  // means one failed request is the final answer, so this is said as soon as it
  // is known rather than discovered by pressing a button — and `ready` requires
  // the listing to have *succeeded*, so "this project has no jpack.json" is
  // only ever said about a project whose files this dialog actually read.
  const blocked = listing.isError
    ? { lead: UNREADABLE_PROJECT, reason: reasonOf(listing.error) }
    : undefined

  const ready =
    slug !== undefined &&
    taken === undefined &&
    template !== undefined &&
    !busy &&
    listing.isSuccess

  const invalidate = (keys: readonly (readonly unknown[])[]) => {
    for (const key of keys) void queryClient.invalidateQueries({ queryKey: key })
  }

  const create = async () => {
    if (!ready || slug === undefined || path === undefined || template === undefined) return
    setFailure(undefined)
    setBusy(true)
    try {
      // (0a) The project's own files. `ready` has already required the listing
      // to have succeeded, which is what makes the next sentence true rather
      // than a guess: a listing that failed says so on its own, above.
      const files = pathsIn(listing.data)
      if (!files.includes(PROJECT_FILE)) {
        setFailure({ lead: NO_PROJECT_FILE })
        return
      }

      // (0b) The configuration as it is now, read once: this answers the
      // collision, and its digest is what the registration commits against.
      let read: FileContent
      let current: ProjectConfig
      try {
        read = await readFile(PROJECT_FILE)
        current = parseProjectConfig(read.content)
      } catch (cause) {
        setFailure({ lead: UNREADABLE_PROJECT_FILE, reason: reasonOf(cause) })
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

      // (1) The pack itself.
      try {
        await writeFile({ path, content, baseSha256: '', createParents: true })
      } catch (cause) {
        setFailure({
          lead: 'The pack could not be created.',
          // A 409 here means a file appeared under that name since the listing
          // was taken. "Reload it, or write again with override" is what the
          // chassis tells an editor; this dialog has no override to offer and
          // the person in front of it has a name to change.
          reason: cause instanceof StaleWrite ? PACK_FILE_TAKEN : reasonOf(cause)
        })
        invalidate([['desk-files']])
        return
      }

      // (2) The entry, against the digest the read in (0b) returned.
      try {
        await writeFile({
          path: PROJECT_FILE,
          content: serialiseProjectConfig(
            read.content,
            withPack(current, slug, packEntryFor(path, description))
          ),
          baseSha256: read.sha256
        })
      } catch (cause) {
        setFailure({
          lead: ORPHANED,
          reason: cause instanceof StaleWrite ? STALE_PROJECT_FILE : reasonOf(cause)
        })
        invalidate([['desk-files'], ['desk-file', PROJECT_FILE]])
        return
      }

      // (3) Everything that answered before this pack existed.
      invalidate([['desk-files'], ['desk-file', PROJECT_FILE], ['list_packs'], ['desk-config']])
      onOpenChange(false)
      navigate(`/packs/${slug}`)
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
    >
      <form
        onSubmit={(event) => {
          event.preventDefault()
          void create()
        }}
      >
        <Field
          label="Name"
          hint={slug === undefined ? undefined : `id: ${slug}`}
          error={nameProblem}
        >
          {(wiring) => (
            <Input
              {...wiring}
              autoFocus
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

        <Field
          label="Template"
          hint={isEmpty && template !== undefined ? EMPTY_IS_A_START : undefined}
          error={templateProblem}
        >
          {(wiring) => (
            <Select
              {...wiring}
              value={selected}
              onValueChange={setChoice}
              options={options}
              placeholder="—"
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
