import { createContext, useContext, useEffect, useState, useSyncExternalStore } from 'react'
import { msg, useLocale } from '../i18n'
import { sourceMessage } from '../i18n/source'
import { useMcp } from '../mcp/McpProvider'
import { consoleSnapshot, recordConnection, subscribeConsole } from './consoleLog'
import { Tabs } from '../ui/Tabs'
import { LogList } from './LogList'

export const DiagnosticsContext = createContext<() => void>(() => {})
export const useDiagnostics = () => useContext(DiagnosticsContext)

/** Recording belongs to the connection lifecycle, independently of visible panels. */
export function useConnectionLog() {
  const { status, connectionEpoch, attempt } = useMcp()
  useEffect(() => {
    recordConnection(attempt > 0
      ? sourceMessage('{{status}} · connection {{epoch}} (attempt {{attempt}})', { status, epoch: connectionEpoch, attempt })
      : sourceMessage('{{status}} · connection {{epoch}}', { status, epoch: connectionEpoch }))
  }, [status, connectionEpoch, attempt])
}
export function Diagnostics() {
  useLocale()
  const [tab, setTab] = useState('connection')
  const entries = useSyncExternalStore(subscribeConsole, consoleSnapshot, consoleSnapshot)
  return <Tabs label={msg('Diagnostics')} scrollable keepMounted value={tab} onValueChange={setTab} tabs={[
    { value: 'connection', label: msg('Connection'), panel: <LogList entries={entries.filter(e=>e.channel==='connection')} empty={msg('Nothing recorded on this connection yet.')} /> },
    { value: 'calls', label: msg('Activity'), panel: <LogList entries={entries.filter(e=>e.channel==='calls')} empty={msg('No operations recorded yet.')} /> },
    { value: 'files', label: msg('File changes'), panel: <LogList entries={entries.filter(e=>e.channel==='files')} empty={msg('No file change has been reported yet.')} /> }
  ]} />
}
