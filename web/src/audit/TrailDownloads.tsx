/**
 * The trail, its signature sidecar and its stamps, downloaded as exact bytes
 * (ADR-0010, section 2): one button for each file the audit directory holds,
 * saved under the runtime's own name. The answer is kept as a Blob from the
 * response to the saved file: nothing here reads it as text.
 */
import { useState } from 'react'
import { msg, systemMessage, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import { downloadTrailFile, TRAIL_FILES, type TrailFile } from './client'
import styles from './DecisionRecord.module.css'

export function TrailDownloads({ files }: { files: readonly TrailFile[] }) {
  useLocale()
  const [busy, setBusy] = useState<TrailFile>()
  const [saved, setSaved] = useState<string>()
  const [error, setError] = useState<string>()
  if (files.length === 0) return null
  async function save(which: TrailFile) {
    setBusy(which); setSaved(undefined); setError(undefined)
    try {
      const blob = await downloadTrailFile(which)
      const url = URL.createObjectURL(blob), link = document.createElement('a')
      link.href = url; link.download = TRAIL_FILES[which]
      document.body.append(link); link.click(); link.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      setSaved(TRAIL_FILES[which])
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(undefined)
    }
  }
  return <section aria-label={msg('Download the trail')}>
    <h4 className={styles.heading}>{msg('Download the trail')}</h4>
    <p className={styles.quiet}>{msg('Each file is saved under the runtime’s own name, as its bytes stood on disk between two writes.')}</p>
    <div className={styles.actions}>{files.map(which => <Button key={which} disabled={busy !== undefined} onClick={() => void save(which)}>{msg('Download {{file}}', { file: TRAIL_FILES[which] })}</Button>)}</div>
    {saved && <p role="status">{msg('Saved {{file}}.', { file: saved })}</p>}
    {error && <p role="alert">{systemMessage(error)}</p>}
  </section>
}
