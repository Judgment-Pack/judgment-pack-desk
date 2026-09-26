import { msg, systemMessage, useLocale } from '../i18n'
export function LogList({
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
