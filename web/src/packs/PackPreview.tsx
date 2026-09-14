import type { PackSummary } from '../mcp/types'
import { ButtonLink } from '../ui/Button'
import styles from './PackPreview.module.css'

/** Read-only inventory metadata; previewing never evaluates or opens an editor. */
export function PackPreview({ pack, onOpen }: { pack: PackSummary; onOpen?: () => void }) {
  const present = (value: string | undefined) => typeof value === 'string' && value.trim() !== ''
  return <section className={styles.preview} aria-label="Pack preview">
    <h2>{pack.id}</h2>
    {present(pack.description) && <p>{pack.description}</p>}
    {present(pack.detail) && <p className={styles.notice}>{pack.detail}</p>}
    <dl className={styles.metadata}>
      <div><dt>Version</dt><dd>{present(pack.packVersion) ? `v${pack.packVersion}` : 'Unavailable'}</dd></div>
      <div><dt>Saved cases</dt><dd>{pack.matrix === true ? 'Configured' : pack.matrix === false ? 'Not configured' : 'Unavailable'}</dd></div>
    </dl>
    <ButtonLink to={`/packs/${encodeURIComponent(pack.id)}`} onClick={event => {
      if (event.button === 0 && !event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey) onOpen?.()
    }}>Open pack</ButtonLink>
    {(present(pack.packId) || present(pack.path) || present(pack.matrixPath)) && <details className={styles.technical}>
      <summary>Technical details</summary>
      <dl className={styles.metadata}>
      {present(pack.packId) && <div><dt>Document ID</dt><dd><code>{pack.packId}</code></dd></div>}
      {present(pack.path) && <div><dt>File</dt><dd><code>{pack.path}</code></dd></div>}
      {present(pack.matrixPath) && <div><dt>Saved cases file</dt><dd><code>{pack.matrixPath}</code></dd></div>}
    </dl>
    </details>}
  </section>
}

export function PackPreviewHint() {
  return <section className={styles.preview} aria-label="Pack preview">
    <p className={styles.hint}>Choose a pack’s preview control, or focus a row and press Space.</p>
  </section>
}
