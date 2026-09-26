import type { ReactNode } from 'react'
import { Button } from '../ui/Button'
import styles from './ChatWorkspace.module.css'

/** A quiet reference to work reviewed in the workspace, shared by drafts and tests. */
export function ArtifactReference({
  title,
  description,
  action,
  indicator,
  onOpen,
  disabled,
  revision,
}: {
  title: string
  description: ReactNode
  action?: string
  indicator?: string
  onOpen?: () => void
  disabled?: boolean
  revision?: number
}) {
  return (
    <div className={styles.artifact} data-draft-revision={revision}>
      <div>
        <strong>{title}</strong>
        <small>{description}</small>
      </div>
      {indicator ? (
        <span className={styles.caption}>{indicator}</span>
      ) : onOpen && action ? (
        <Button disabled={disabled} onClick={onOpen}>
          {action}
        </Button>
      ) : null}
    </div>
  )
}
