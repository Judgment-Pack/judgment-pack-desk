/**
 * A write the chassis refused because the file on disk is not the file this
 * edit started from.
 *
 * Nothing went wrong and nothing was written: the buffer is intact, and the
 * two ways forward are stated as what they are rather than one of them being
 * taken. **Reload says that it discards**, because it does, and
 * **Overwrite anyway is never the default** — a client whose primary button
 * overwrote would have no concurrency story, only an unstated one.
 *
 * Both digests are behind a disclosure. Each is a fact the reader may need and
 * neither is the first thing to say: sixty-four hex characters ahead of "this
 * file changed and nothing was written" buries the sentence that matters.
 *
 * `exists` is the chassis' own, not inferred: a write that stated no base
 * digest believed nothing was there, and finding something is a different
 * event from a file that moved underneath an edit.
 */
import type { StaleWrite } from '../../files/client'
import { AlertPanel } from '../../ui/AlertPanel'
import { Button } from '../../ui/Button'
import styles from './StaleWriteAlert.module.css'

export function StaleWriteAlert({
  stale,
  pending,
  onReload,
  onOverwrite
}: {
  stale: StaleWrite
  pending: boolean
  onReload: () => void
  onOverwrite: () => void
}) {
  return (
    <AlertPanel
      heading="This file changed since you opened it. Nothing was written."
      detailLabel="digests"
      detail={
        <>
          <span>
            this edit started from <code>sha256 {digest(stale.expectedSha256)}</code>
          </span>
          <span>
            on disk now <code>sha256 {digest(stale.actualSha256)}</code>
          </span>
        </>
      }
      actions={
        <>
          <Button variant="primary" disabled={pending} onClick={onReload}>
            Reload
          </Button>
          <Button variant="quiet" disabled={pending} onClick={onOverwrite}>
            Overwrite anyway
          </Button>
        </>
      }
    >
      <span className={styles.body}>
        {stale.exists
          ? 'Something else wrote to it while this edit was open.'
          : 'The file is no longer on disk — something else deleted or moved it.'}{' '}
        Your draft is intact. Reload takes the file on disk and discards these edits;
        overwriting replaces it.
      </span>
    </AlertPanel>
  )
}

function digest(value: string): string {
  return value ? `${value.slice(0, 12)}…` : '(no file)'
}
