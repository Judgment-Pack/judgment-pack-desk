/** Mutable, read-only server snapshots outside the project file watcher.
 * Never use for commands, previews, validation, generation, or an editor's
 * frozen base revision. Cached data survives a failed refresh. TanStack pauses
 * polling in hidden windows; the next mount/focus refreshes immediately.
 * Writes still require their original revision and are never retried here.
 */
export const privateQuery = {
 staleTime: 0,
 refetchOnMount: 'always' as const,
 refetchOnWindowFocus: true,
 refetchOnReconnect: true,
 refetchInterval: 10_000,
 refetchIntervalInBackground: false,
 retry: false as const,
}
