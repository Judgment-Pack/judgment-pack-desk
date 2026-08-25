/**
 * The file API as react-query hooks, on the same terms the MCP queries use.
 *
 * These are kept apart from `src/mcp/queries.ts` on purpose: those ask the
 * runtime, these ask the chassis, and the two must not blur into one surface in
 * a reader's head. The runtime is a judge and answers questions; the chassis is
 * the user's hand and moves bytes.
 */
import { useMutation, useQuery, type UseMutationResult, type UseQueryResult } from '@tanstack/react-query'
import { listFiles, readFile, writeFile, type FileContent, type FileListing } from './client'

/** Every regular file the project contains. */
export function useFileListing(): UseQueryResult<FileListing, Error> {
  return useQuery({
    queryKey: ['desk-files'],
    queryFn: ({ signal }) => listFiles(signal)
  })
}

/**
 * One file's bytes.
 *
 * `staleTime: 0` and no background refetch: the editor's whole concurrency
 * story is that it knows which bytes it loaded, and a query that silently
 * replaced them underneath an unsaved buffer would undo that. A reload is
 * something the user asks for.
 */
export function useFileContent(path: string | undefined): UseQueryResult<FileContent, Error> {
  return useQuery({
    queryKey: ['desk-file', path ?? null],
    enabled: Boolean(path),
    queryFn: ({ signal }) => readFile(path!, signal)
  })
}

export interface WriteInput {
  path: string
  content: string
  baseSha256: string
  override?: boolean
}

/**
 * One save.
 *
 * A mutation and not a query because it is not a read: it replaces a file on
 * the user's disk. Nothing here retries — a save that failed is the user's to
 * repeat, and a silent retry of a write is how one edit becomes two.
 */
export function useWriteFile(): UseMutationResult<FileContent, Error, WriteInput> {
  return useMutation({ mutationFn: writeFile })
}
