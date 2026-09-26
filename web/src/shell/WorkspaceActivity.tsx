import { useState, useSyncExternalStore } from 'react'
import { msg, useLocale } from '../i18n'
import { Tabs } from '../ui/Tabs'
import { consoleSnapshot, subscribeConsole } from './consoleLog'
import { LogList } from './LogList'

/** A bounded session log. File changes are watcher events, not source files. */
export function WorkspaceActivity({ runtime = false }: { runtime?: boolean }) {
  useLocale()
  const entries = useSyncExternalStore(subscribeConsole, consoleSnapshot, consoleSnapshot)
  const [tab, setTab] = useState('calls')
  if (runtime) return <div className="desk-tool-log"><LogList entries={entries.filter(e => e.channel === 'connection')} empty={msg('Nothing recorded on this connection yet.')} /></div>
  return <Tabs label={msg('Activity')} scrollable keepMounted value={tab} onValueChange={setTab} tabs={[
    { value: 'calls', label: msg('Activity'), panel: <LogList entries={entries.filter(e => e.channel === 'calls')} empty={msg('No operations recorded yet.')} /> },
    { value: 'files', label: msg('File changes'), panel: <LogList entries={entries.filter(e => e.channel === 'files')} empty={msg('No file change has been reported yet.')} /> }
  ]} />
}
