/**
 * The Console: what this session's own wire has done.
 *
 * Two channels carry real entries in phase A, because two channels have a real
 * feed. **Connection** is driven off `useMcp()` — status, epoch, reconnect
 * attempt — and **Files** off the chassis' own `desk/fileChanged`
 * notification. **Calls** and **Notices** exist as tabs and say they arrive
 * later; they render no rows at all, because a channel that invented plausible
 * traffic would be worse than an absent one.
 *
 * The pane spans the content area right of the rail — grid columns 2–3, never
 * under the rail — and the log list is `aria-live="off"`: a screen reader
 * should not be read a running commentary of a socket.
 */
import { Tabs } from 'radix-ui'
import { useEffect, useSyncExternalStore } from 'react'
import { useMcp } from '../mcp/McpProvider'
import { consoleSnapshot, recordConnection, subscribeConsole } from './consoleLog'
import type { ConsoleTab } from './paneState'

const LATER = 'This channel arrives later.'

export function BottomPane({
  open,
  tab,
  onTabChange
}: {
  open: boolean
  tab: ConsoleTab
  onTabChange: (tab: ConsoleTab) => void
}) {
  const { status, connectionEpoch, attempt } = useMcp()
  const entries = useSyncExternalStore(subscribeConsole, consoleSnapshot, consoleSnapshot)

  // One line per transition. The store drops an identical consecutive line,
  // which is what makes StrictMode's mount → cleanup → mount free here.
  useEffect(() => {
    const suffix = attempt > 0 ? ` (attempt ${attempt})` : ''
    recordConnection(`${status} · connection ${connectionEpoch}${suffix}`)
  }, [status, connectionEpoch, attempt])

  const connection = entries.filter((entry) => entry.channel === 'connection')
  const files = entries.filter((entry) => entry.channel === 'files')

  return (
    <section className="desk-console" aria-label="Console" id="desk-console" hidden={!open}>
      <Tabs.Root value={tab} onValueChange={(next) => onTabChange(next as ConsoleTab)}>
        <Tabs.List className="desk-tablist" aria-label="Console channels">
          <Tabs.Trigger className="desk-tab" value="connection">
            Connection
          </Tabs.Trigger>
          <Tabs.Trigger className="desk-tab" value="calls">
            Calls
          </Tabs.Trigger>
          <Tabs.Trigger className="desk-tab" value="files">
            Files
          </Tabs.Trigger>
          <Tabs.Trigger className="desk-tab" value="notices">
            Notices
          </Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content className="desk-console-body" value="connection">
          <LogList entries={connection} empty="Nothing recorded on this connection yet." />
        </Tabs.Content>
        <Tabs.Content className="desk-console-body" value="calls">
          <p className="desk-pane-empty">{LATER}</p>
        </Tabs.Content>
        <Tabs.Content className="desk-console-body" value="files">
          <LogList entries={files} empty="No file change has been reported yet." />
        </Tabs.Content>
        <Tabs.Content className="desk-console-body" value="notices">
          <p className="desk-pane-empty">{LATER}</p>
        </Tabs.Content>
      </Tabs.Root>
    </section>
  )
}

function LogList({
  entries,
  empty
}: {
  entries: { seq: number; at: number; text: string }[]
  empty: string
}) {
  if (entries.length === 0) return <p className="desk-pane-empty">{empty}</p>
  return (
    <ul className="desk-log" aria-live="off">
      {entries.map((entry) => (
        <li key={entry.seq}>
          <span className="quiet">{new Date(entry.at).toISOString().slice(11, 19)}</span>
          <span>{entry.text}</span>
        </li>
      ))}
    </ul>
  )
}
