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
 * 0. The project must already have a `jpack.json` — `packs` carries
 *    `minProperties: 1`, so this can only ever amend one and never write one
 *    from nothing. The check runs **before any write**, so a project without
 *    one is told, and nothing lands.
 * 1. The pack file is written, asking for its parent to be made.
 * 2. `jpack.json` is read, one entry is added to what came back, and it is
 *    written with the digest that read just returned — so a change made while
 *    this dialog was open is refused rather than overwritten.
 * 3. The caches are invalidated and the new pack's page is opened.
 *
 * **If 1 succeeds and 2 fails, the pack file is on disk and nothing names it.**
 * The dialog says exactly that. There is no unwind to perform — the file API
 * has no delete verb — and claiming one would be worse than the residue.
 */
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { StaleWrite, readFile, writeFile } from '../files/client'
import { useFileContent, useFileListing } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { useExample, useExampleListing, useSchema } from '../mcp/starters'
import {
  existingPackKeys,
  packEntryFor,
  parseProjectConfig,
  serialiseProjectConfig,
  withPack
} from '../packs/jpackConfig'
import { collisionIn, emptyPackFrom, packPathFor, shapeTemplate, slugFor } from '../packs/newPack'
import { Button } from '../ui/Button'
import { Dialog, DialogActions, DialogClose } from '../ui/Dialog'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { TextArea } from '../ui/TextArea'

const PROJECT_FILE = 'jpack.json'
const EMPTY = 'empty'
const EMPTY_LABEL = 'Empty pack'

const NO_PROJECT_FILE =
  'This project has no jpack.json, so a new pack cannot be registered. Nothing was created.'
const STALE_PROJECT_FILE = 'jpack.json changed while creating — reload and try again'
const ORPHANED =
  'The pack was created but could not be registered. Nothing else was changed.'

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

  // The runtime's own first example, else the empty pack. The runtime's order
  // is never re-sorted here.
  const selected = choice ?? (exampleSupported && offered.length > 0 ? offered[0]!.name : EMPTY)
  const isEmpty = selected === EMPTY

  const example = useExample(isEmpty ? undefined : selected)
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
    let keys: string[] = []
    if (project.data) {
      try {
        keys = existingPackKeys(parseProjectConfig(project.data.content))
      } catch {
        // An unreadable project file is not this field's problem to report:
        // the create sequence surfaces it in full, and a name is not wrong
        // because a file elsewhere is.
        keys = []
      }
    }
    return collisionIn(slug, {
      keys,
      files: (listing.data?.files ?? []).map((file) => file.path),
      path
    })
  }, [slug, path, project.data, listing.data])

  // A template the runtime is still fetching is not a refusal, and one it
  // refused is: the two are kept apart so a slow answer never reads as a
  // failure.
  const template = isEmpty
    ? schemaSupported
      ? schema.data === undefined
        ? undefined
        : emptyPackFrom(schema.data)
      : emptyPackFrom(undefined)
    : example.data
  const templateError = isEmpty ? (schemaSupported ? schema.error : null) : example.error

  const nameProblem = name.trim() === '' ? undefined : 'problem' in derived ? derived.problem : taken
  const ready =
    slug !== undefined &&
    taken === undefined &&
    template !== undefined &&
    !busy &&
    !listing.isPending

  const create = async () => {
    if (!ready || slug === undefined || path === undefined || template === undefined) return
    setFailure(undefined)
    setBusy(true)
    try {
      // (0) Nothing is written until the project is known to have a file to
      // register the pack in.
      const files = (listing.data?.files ?? []).map((file) => file.path)
      if (!files.includes(PROJECT_FILE)) {
        setFailure({ lead: NO_PROJECT_FILE })
        return
      }

      // (1) The pack itself.
      const content = shapeTemplate(template, { name, description, slug, idBase })
      try {
        await writeFile({ path, content, baseSha256: '', createParents: true })
      } catch (cause) {
        setFailure({ lead: 'The pack could not be created.', reason: reasonOf(cause) })
        return
      }

      // (2) The entry, against the digest this read just returned.
      try {
        const read = await readFile(PROJECT_FILE)
        const amended = withPack(
          parseProjectConfig(read.content),
          slug,
          packEntryFor(path, description)
        )
        await writeFile({
          path: PROJECT_FILE,
          content: serialiseProjectConfig(read.content, amended),
          baseSha256: read.sha256
        })
      } catch (cause) {
        setFailure({
          lead: ORPHANED,
          reason: cause instanceof StaleWrite ? STALE_PROJECT_FILE : reasonOf(cause)
        })
        return
      }

      // (3) Everything that answered before this pack existed.
      for (const key of [['desk-files'], ['list_packs'], ['desk-config']]) {
        void queryClient.invalidateQueries({ queryKey: key })
      }
      onOpenChange(false)
      navigate(`/packs/${slug}`)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Create a pack">
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
          error={templateError ? `This template could not be read — ${templateError.message}` : undefined}
        >
          {(wiring) => (
            <Select
              {...wiring}
              value={selected}
              onValueChange={setChoice}
              options={[
                ...(exampleSupported
                  ? offered.map((entry) => ({ value: entry.name, label: entry.name }))
                  : []),
                { value: EMPTY, label: EMPTY_LABEL }
              ]}
            />
          )}
        </Field>

        {failure && (
          <p role="alert">
            {failure.lead}
            {/* The reason is its own element so that what the chassis or the
                conflict actually said can be read — and asserted — on its own,
                rather than only as a tail of this dialog's sentence. */}
            {failure.reason && <> <span>{failure.reason}</span></>}
          </p>
        )}

        <DialogActions>
          <DialogClose asChild>
            <Button variant="secondary">Cancel</Button>
          </DialogClose>
          <Button variant="primary" type="submit" disabled={!ready}>
            Create pack
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  )
}

/** The message the failure carries, never a sentence invented over it. */
function reasonOf(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}
