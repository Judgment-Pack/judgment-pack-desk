import { systemMessage } from '../i18n'
import { sourceMessage } from '../i18n/source'
import { msg, useLocale } from '../i18n'
/** Selected details and real activity, beneath the main pane. */
import { Tabs } from 'radix-ui'
import { useEffect, useSyncExternalStore } from 'react'
import { useMcp } from '../mcp/McpProvider'
import { consoleSnapshot, recordConnection, subscribeConsole } from './consoleLog'
import { Button } from '../ui/Button'
import type { ConsoleTab } from './paneState'

const LATER = 'This channel arrives later.'

export function BottomPane({
  open,
  tab,
  onTabChange,
  details = false,
  showDetails = false,
  onDetails,
  publishTarget,
  onClose,
  onMaximize,
  maximized = false
}: {
  open: boolean
  tab: ConsoleTab
  onTabChange: (tab: ConsoleTab) => void
  details?: boolean
  showDetails?: boolean
  onDetails?: () => void
  publishTarget?: (target: HTMLDivElement | null) => void
  onClose?: () => void
  onMaximize?: () => void
  maximized?: boolean
}) {
  useLocale()
  const { status, connectionEpoch, attempt } = useMcp()
  const entries = useSyncExternalStore(subscribeConsole, consoleSnapshot, consoleSnapshot)

  // One line per transition. The store drops an identical consecutive line,
  // which is what makes StrictMode's mount → cleanup → mount free here.
  useEffect(() => {
    recordConnection(attempt > 0
      ? sourceMessage('{{status}} · connection {{epoch}} (attempt {{attempt}})', { status, epoch: connectionEpoch, attempt })
      : sourceMessage('{{status}} · connection {{epoch}}', { status, epoch: connectionEpoch }))
  }, [status, connectionEpoch, attempt])

  const connection = entries.filter((entry) => entry.channel === 'connection')
  const files = entries.filter((entry) => entry.channel === 'files')

  return (
    <section className="desk-console" aria-label={msg("Console")} id="desk-console" hidden={!open}>
      {/* A flex column of its own, and the class is load-bearing. `.desk-console`
          is a fixed-height flex column with `overflow: hidden`; this element sat
          between it and `.desk-console-body` as an ordinary block, so the body's
          `flex: 1; overflow: auto` had neither a flex parent nor a constrained
          height and a long log was clipped rather than scrolled. */}
      <Tabs.Root
        className="desk-console-tabs"
        value={details && showDetails ? 'details' : tab}
        onValueChange={(next) => next === 'details' ? onDetails?.() : onTabChange(next as ConsoleTab)}
      >
        <div className="desk-console-heading"><Tabs.List className="desk-tablist" aria-label={msg("Console channels")}>
          {details && <Tabs.Trigger className="desk-tab" value="details">{msg("Details")}</Tabs.Trigger>}
          <Tabs.Trigger className="desk-tab" value="connection">{msg("Connection")}</Tabs.Trigger>
          <Tabs.Trigger className="desk-tab" value="calls">{msg("Activity")}</Tabs.Trigger>
          <Tabs.Trigger className="desk-tab" value="files">{msg("Files")}</Tabs.Trigger>
          <Tabs.Trigger className="desk-tab" value="notices">{msg("Notices")}</Tabs.Trigger>
        </Tabs.List>
        <div className="desk-console-actions">
          {onMaximize && <Button variant="quiet" onClick={onMaximize} aria-label={maximized ? msg("Restore panel height") : msg("Expand panel")}>{maximized ? msg("Restore") : msg("Expand")}</Button>}
          {onClose && <Button variant="quiet" onClick={onClose}>{msg("Close")}</Button>}
        </div></div>
        <Tabs.Content forceMount className="desk-console-body" value="details" hidden={!details || !showDetails}>
          <div className="desk-details-slot" ref={publishTarget} />
        </Tabs.Content>
        <Tabs.Content className="desk-console-body" value="connection">
          <LogList entries={connection} empty={msg("Nothing recorded on this connection yet.")} />
        </Tabs.Content>
        <Tabs.Content className="desk-console-body" value="calls">
          <LogList entries={entries.filter((entry) => entry.channel === 'calls')} empty={msg("No operations recorded yet.")} />
        </Tabs.Content>
        <Tabs.Content className="desk-console-body" value="files">
          <LogList entries={files} empty={msg("No file change has been reported yet.")} />
        </Tabs.Content>
        <Tabs.Content className="desk-console-body" value="notices">
          <p className="desk-pane-empty">{msg(LATER)}</p>
        </Tabs.Content>
      </Tabs.Root>
    </section>
  )
}

function LogList({
  entries,
  empty
}: {
  entries: { seq: number; at: number; text: string; authored?: boolean; context?: 'research' | 'assistant' }[]
  empty: string
}) {
  useLocale()
  if (entries.length === 0) return <p className="desk-pane-empty">{empty}</p>
  return (
    <ul className="desk-log" aria-live="off">
      {entries.map((entry) => (
        <li key={entry.seq}>
          <span className="quiet">{new Date(entry.at).toISOString().slice(11, 19)}</span>
          <span>{entry.context && <>{entry.context === 'research' ? msg('Research') : msg('Assistant')}: </>}{entry.authored ? systemMessage(entry.text) : entry.text}</span>
        </li>
      ))}
    </ul>
  )
}
