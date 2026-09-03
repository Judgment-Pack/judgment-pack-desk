/**
 * `jpack-desk.json`, read through the file API the desk already has.
 *
 * **Zero Go.** The project's configuration is an ordinary project file, so it
 * is read with `GET /api/file` exactly as every other file is — no new
 * endpoint, no new proxy entry, and `viteConfig.test.ts`'s exact `['/api','/ws']`
 * assertion still holds untouched.
 *
 * Two consequences of that, both deliberate rather than tolerated:
 *
 * - The path is under no `skipDirs` entry, so the chassis' watcher already
 *   reports changes to it, and `McpProvider`'s blanket `invalidateQueries()`
 *   makes the header update live when the file is edited in any editor.
 * - It appears in `GET /api/files` and is editable in `/author`. That is
 *   honest: it *is* an ordinary project file, and the file API forms no
 *   opinion about what any file means.
 *
 * **The query never rejects.** A 404, a body that is not JSON, a file over the
 * 4 MiB read cap, a non-UTF-8 file, or no `fetch` stub at all each resolve to
 * the built-in defaults with the reason recorded for Admin — and with the
 * chassis' status beside it where the chassis answered, so a refusal it issued
 * is not confused with a request that never reached it. The last case is
 * load-bearing: `testing/harness.tsx` stubs no fetch, and a shell query that
 * rejected would poison every future integration test with an unhandled
 * rejection that has nothing to do with the case under test.
 */
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { FileRequestError, readFile } from '../files/client'
import {
  PROJECT_CONFIG_PATH,
  decodeDeskConfig,
  effectiveConfig,
  type EffectiveConfig
} from './deskConfig'

export const DESK_CONFIG_QUERY_KEY = ['desk-config'] as const

export async function loadDeskConfig(signal?: AbortSignal): Promise<EffectiveConfig> {
  let text: string
  try {
    text = (await readFile(PROJECT_CONFIG_PATH, signal)).content
  } catch (cause) {
    // **Absence and every other failure are different answers.** A 404 is a
    // project that has not written the file: defaults, no banner, no error,
    // and the state every desk is in until someone writes one. Everything
    // else is a read that did not produce one, and reporting that as "no file"
    // is the desk describing itself as unconfigured when it is merely unread.
    // Not all of "everything else" says the same thing about the file, either:
    // a 413 or a permission refusal is the chassis speaking about something it
    // found, while a socket that never answered establishes only that absence
    // was **not** established — which is weaker, and is why the provenance
    // below is carried rather than summarised into one sentence.
    //
    // And the provenance is **carried, not inferred**. A refusal the chassis
    // answered is a statement about the file; a `200` whose body is not the
    // envelope this API promises is still an answer, but the sentence about it
    // is the desk's; and a socket that never answered is neither — it
    // establishes only that absence was *not* established. Reading all three
    // off "is there a status?" put the middle one in the last bucket and had
    // Admin say the request never got an answer.
    if (cause instanceof FileRequestError) {
      if (cause.status === 404) return effectiveConfig(undefined, reasonFor(cause))
      return effectiveConfig(undefined, undefined, {
        reason: cause.message,
        responseReceived: true,
        status: cause.status,
        source: cause.source
      })
    }
    return effectiveConfig(undefined, undefined, {
      reason: messageOf(cause),
      responseReceived: false,
      source: 'browser'
    })
  }
  return effectiveConfig(decodeDeskConfig(text, 'project'))
}

function messageOf(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function reasonFor(cause: unknown): string {
  return `no configuration was read: ${messageOf(cause)}`
}

export function useDeskConfig(): UseQueryResult<EffectiveConfig, Error> {
  return useQuery({
    queryKey: DESK_CONFIG_QUERY_KEY,
    staleTime: Infinity,
    retry: false,
    queryFn: ({ signal }) => loadDeskConfig(signal)
  })
}
