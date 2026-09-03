/**
 * Create pack: it writes bytes and forms no opinion.
 *
 * **The desk ships no template.** Starting bytes are the runtime's own
 * example, the runtime's own schema, or an empty file — a desk-authored
 * skeleton would be the desk asserting what a pack is. Where the runtime
 * advertises neither tool the dialog says so in one line and offers the empty
 * file.
 *
 * **The dialog runs no model.** Where the runtime advertises `author_pack`,
 * Help & About renders that prompt's text for a person to carry to whatever
 * agent they run; this dialog links there and does nothing else about it.
 *
 * The path field is free text. It is seeded with `packs/` — a prefix, not a
 * name, so the desk invents nothing — **only where the project already has a
 * file under `packs/`**, and with nothing otherwise. That rule exists because
 * the chassis creates no directories: `files.go` stats the parent and answers
 * 404 with "the directory packs does not exist in the project; create it
 * first", and a dialog whose default path 404s on first use in a flat project
 * is a dead end offered as a convenience. The helper lines say both things —
 * that the location and the suffix are this dialog's idea, and that the parent
 * directory has to be there already.
 *
 * The write is the existing `PUT /api/file` with `baseSha256: ''`, the
 * documented "I believe this file does not exist" case, and **no `override`**.
 * A 409 is reported as what it is; overriding a file the user did not know was
 * there is not a convenience, it is a lost document.
 */
import { useQueryClient } from '@tanstack/react-query'
import { Dialog } from 'radix-ui'
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { StaleWrite } from '../files/client'
import { useFileListing, useWriteFile } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { useExample, useExampleListing, useSchema } from '../mcp/starters'
import { requestOpen } from './authorBridge'

const PATH_PREFIX = 'packs/'
const PATH_HELP =
  'A convenience of this dialog: nothing in JPS requires this location or this suffix.'
const PARENT_DIR_NOTE =
  'The parent directory has to exist already — the chassis writes files and creates no ' +
  'directories, and it refuses a path whose directory is not there.'
const NO_STARTER =
  'This runtime advertises no example and no schema, so the only starting point the desk can ' +
  'offer without inventing one is an empty file.'
const SCHEMA_LABEL = "The runtime's JPS schema — a reference to author against, not a pack"

type Choice = { kind: 'example'; name: string } | { kind: 'schema' } | { kind: 'empty' }

function encodeChoice(choice: Choice): string {
  return choice.kind === 'example' ? `example:${choice.name}` : choice.kind
}

function decodeChoice(value: string): Choice {
  if (value.startsWith('example:')) return { kind: 'example', name: value.slice('example:'.length) }
  return value === 'schema' ? { kind: 'schema' } : { kind: 'empty' }
}

export function CreatePackDialog({
  open,
  onOpenChange
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { exampleSupported, schemaSupported } = useMcp()
  const listing = useExampleListing()
  const files = useFileListing()
  const write = useWriteFile()
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const [path, setPath] = useState('')
  const [pathEdited, setPathEdited] = useState(false)
  const [selection, setSelection] = useState<string | undefined>(undefined)

  const examples = useMemo(() => listing.data?.examples ?? [], [listing.data])

  /**
   * `packs/` only where the project already keeps one there.
   *
   * The listing is the project's own answer, not a guess: a project with a
   * flat layout gets an empty field and writes where it actually writes, and a
   * project with a `packs/` directory gets the prefix it uses. A failed or
   * pending listing is the flat case, because an unanswered question is not
   * evidence that a directory exists.
   */
  const seed = useMemo(
    () => ((files.data?.files ?? []).some((file) => file.path.startsWith(PATH_PREFIX)) ? PATH_PREFIX : ''),
    [files.data]
  )
  useEffect(() => {
    if (pathEdited) return
    setPath(seed)
  }, [seed, pathEdited])

  // The runtime's own first example, else the schema, else empty — the
  // precedence in one place, and the runtime's ordering never re-sorted here.
  const defaultChoice = useMemo<Choice>(() => {
    if (exampleSupported && examples.length > 0) return { kind: 'example', name: examples[0]!.name }
    if (schemaSupported) return { kind: 'schema' }
    return { kind: 'empty' }
  }, [exampleSupported, examples, schemaSupported])

  const choice = selection === undefined ? defaultChoice : decodeChoice(selection)
  const example = useExample(choice.kind === 'example' ? choice.name : undefined)
  const schema = useSchema(choice.kind === 'schema')

  useEffect(() => {
    if (!open) {
      setPath('')
      setPathEdited(false)
      setSelection(undefined)
      write.reset()
    }
    // `write` is a stable mutation object; resetting it is the point of the
    // effect and listing it would loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const starting =
    choice.kind === 'example' ? example.data : choice.kind === 'schema' ? schema.data : ''
  const startingError = choice.kind === 'example' ? example.error : schema.error
  const startingPending =
    (choice.kind === 'example' && example.isPending) || (choice.kind === 'schema' && schema.isPending)

  const trimmed = path.trim()
  const ready = trimmed !== '' && !trimmed.endsWith('/') && starting !== undefined

  const stale = write.error instanceof StaleWrite ? write.error : undefined
  const failure = write.error && !stale ? write.error : undefined

  const create = () => {
    if (!ready || starting === undefined) return
    write.mutate(
      { path: trimmed, content: starting, baseSha256: '' },
      {
        onSuccess: () => {
          void queryClient.invalidateQueries({ queryKey: ['desk-files'] })
          requestOpen(trimmed)
          onOpenChange(false)
          navigate('/author')
        }
      }
    )
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="desk-overlay" />
        <Dialog.Content className="desk-dialog">
          <Dialog.Title>Create a pack</Dialog.Title>
          <Dialog.Description className="quiet">
            The desk writes the bytes; the runtime judges them. Nothing here validates anything.
          </Dialog.Description>

          <p>
            <label htmlFor="desk-create-path">Path</label>
            <br />
            <input
              id="desk-create-path"
              className="desk-input"
              value={path}
              onChange={(event) => {
                setPathEdited(true)
                setPath(event.target.value)
              }}
            />
          </p>
          <p className="quiet">{PATH_HELP}</p>
          <p className="quiet">{PARENT_DIR_NOTE}</p>

          <p>
            <label htmlFor="desk-create-start">Starting bytes</label>
            <br />
            <select
              id="desk-create-start"
              value={encodeChoice(choice)}
              onChange={(event) => setSelection(event.target.value)}
            >
              {exampleSupported &&
                examples.map((entry) => (
                  <option key={entry.name} value={`example:${entry.name}`}>
                    {entry.focus ? `${entry.name} — ${entry.focus}` : entry.name}
                  </option>
                ))}
              {schemaSupported && <option value="schema">{SCHEMA_LABEL}</option>}
              <option value="empty">Empty file</option>
            </select>
          </p>
          {!exampleSupported && !schemaSupported && <p className="quiet">{NO_STARTER}</p>}
          {!exampleSupported && schemaSupported && (
            <p className="quiet">
              This runtime does not advertise the <code>list_examples</code> /{' '}
              <code>get_example</code> pair, so no example source is available — the schema and an
              empty file are what it offers.
            </p>
          )}
          {startingError && (
            <p className="note note-warn" role="status">
              The starting bytes could not be read — {startingError.message}
            </p>
          )}

          {stale && (
            <p className="note note-warn" role="status">
              A file already exists at that path.
            </p>
          )}
          {failure && (
            <p className="note note-warn" role="status">
              The file could not be created — {failure.message}
            </p>
          )}

          <p>
            <button
              type="button"
              onClick={create}
              disabled={!ready || write.isPending || startingPending}
            >
              Create
            </button>{' '}
            <Dialog.Close>Cancel</Dialog.Close>
          </p>

          <p className="quiet">
            Authoring method is the runtime's, not the desk's:{' '}
            <Link to="/help#authoring-method" onClick={() => onOpenChange(false)}>
              Help &amp; About › Authoring method
            </Link>
            . The desk holds no model key and runs no prompt.
          </p>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
