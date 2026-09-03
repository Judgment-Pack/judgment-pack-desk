/**
 * The chassis file API, as this client calls it (issue #14, phase 1).
 *
 * These are the desk's only writes, and they do not go over the relay. The
 * runtime deliberately has no write tools — ADR-0006 makes it a stateless
 * oracle — so the authoring lifecycle lives in the client, and the client is
 * this. The relay stays a verbatim pipe and the runtime stays a judge: nothing
 * here decides whether a document is valid, only what its bytes are.
 *
 * Two properties of the wire matter to every caller and are modelled here
 * rather than in each view:
 *
 * - **A write answers with a read-back**, taken off the disk after the rename
 *   rather than echoed from the request, so a caller can verify what landed.
 * - **A stale write is refused, not applied**, with both digests — the one the
 *   editor loaded and the one on disk now. That is the same digest discipline
 *   the graph binding uses, for the same reason: two answers about one file,
 *   and only equality proves they are about one revision.
 */
import { sessionToken } from '../mcp/McpProvider'

/** One file the project contains, as the listing reports it. */
export interface FileEntry {
  /** Project-relative and slash-separated, on every platform. */
  path: string
  bytes: number
  /**
   * Bare hex, the payload-member convention. Empty where the file was too
   * large to digest, which the listing reports rather than guessing.
   */
  sha256: string
}

export interface FileListing {
  root: string
  files: FileEntry[]
  note?: string
  /**
   * What the listing could not read, where anything could not be read.
   *
   * Present exactly when the answer is incomplete — an unreadable subtree, a
   * directory that contains itself, a tree past the walk's budget, a file whose
   * digest could not be taken. `files` is then not all of them, and a view that
   * ignored this would report a project as empty on the strength of a
   * permission error.
   */
  partial?: string[]
}

/** One file's bytes and what they hash to. A write answers with this too. */
export interface FileContent {
  path: string
  bytes: number
  sha256: string
  content: string
  /** Present and true exactly when the write brought the file into existence. */
  created?: boolean
}

/**
 * A write refused because the file on disk is not the file the edit started
 * from.
 *
 * This is not an error in the ordinary sense and is deliberately its own type:
 * nothing went wrong, the desk simply declines to overwrite a change it never
 * saw. `expectedSha256` is what the editor loaded and `actualSha256` is what is
 * there now — and `exists` separates "someone changed it" from "someone deleted
 * it" from "you believed this was a new file and it is not".
 */
export class StaleWrite extends Error {
  readonly path: string
  readonly expectedSha256: string
  readonly actualSha256: string
  readonly exists: boolean

  constructor(body: {
    error?: string
    path?: string
    expectedSha256?: string
    actualSha256?: string
    exists?: boolean
  }) {
    super(body.error ?? 'the file on disk is not the file this edit started from')
    this.name = 'StaleWrite'
    this.path = body.path ?? ''
    this.expectedSha256 = body.expectedSha256 ?? ''
    this.actualSha256 = body.actualSha256 ?? ''
    this.exists = body.exists ?? false
  }
}

function endpoint(path: string, params: Record<string, string> = {}): string {
  const query = new URLSearchParams({ token: sessionToken(), ...params })
  return `${path}?${query.toString()}`
}

/**
 * Read one answer, with the chassis' own message kept as the reason.
 *
 * The chassis answers every failure with `{"error": …}`, so a caller never has
 * to invent a sentence for a status code. Where the body is not that shape —
 * a proxy, a crash — the status line is the honest fallback, and it is stated
 * as a status rather than dressed up as a message from the desk.
 */
async function answer<T>(response: Response): Promise<T> {
  const text = await response.text()
  let body: unknown
  try {
    body = JSON.parse(text) as unknown
  } catch {
    body = undefined
  }
  if (response.ok) {
    if (body === undefined) {
      throw new Error(`the desk answered ${response.status} with text that is not JSON`)
    }
    return body as T
  }
  if (response.status === 409 && body !== undefined) {
    throw new StaleWrite(body as ConstructorParameters<typeof StaleWrite>[0])
  }
  const message = (body as { error?: string } | undefined)?.error
  throw new Error(message ?? `the desk answered ${response.status} ${response.statusText}`)
}

/** Every regular file in the project tree. */
export async function listFiles(signal?: AbortSignal): Promise<FileListing> {
  return answer<FileListing>(await fetch(endpoint('/api/files'), { signal }))
}

/** One file's current bytes. */
export async function readFile(path: string, signal?: AbortSignal): Promise<FileContent> {
  return answer<FileContent>(await fetch(endpoint('/api/file', { path }), { signal }))
}

/**
 * Replace one file's bytes, and answer with what is on disk afterwards.
 *
 * `baseSha256` is the digest of the bytes the editor loaded, and the empty
 * string means "I believe this file does not exist". Passing `override` writes
 * regardless — it is the user's deliberate choice and never a default, because
 * a client that always sent it would have no concurrency story, only an
 * unstated one.
 *
 * `createParents` asks the chassis to make the missing directories of `path`
 * inside the project before writing. Off unless a caller asks, on the same
 * terms and for the same reason as `override`: a client that always sent it
 * would be creating directories on every save, and the refusal a caller wants
 * for an ordinary save is the one this member turns off.
 */
export async function writeFile(input: {
  path: string
  content: string
  baseSha256: string
  override?: boolean
  createParents?: boolean
}): Promise<FileContent> {
  const response = await fetch(endpoint('/api/file'), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      path: input.path,
      content: input.content,
      baseSha256: input.baseSha256,
      override: input.override ?? false,
      createParents: input.createParents ?? false
    })
  })
  return answer<FileContent>(response)
}
