import type { ReactNode } from 'react'
import { ChatAction, ChatActionLink } from './ChatActions'
import { OverflowTooltip } from '../ui/Tooltip'
import styles from './ChatWorkspace.module.css'

/** A quiet reference to work reviewed in the workspace, shared by drafts and tests. */
export function ArtifactReference({
  title,
  description,
  action,
  indicator,
  onOpen,
  href,
  disabled,
  revision,
  compact = false,
}: {
  title: string
  description: ReactNode
  action?: string
  indicator?: string
  onOpen?: () => void
  href?: string
  disabled?: boolean
  revision?: number
  compact?: boolean
}) {
  const content = <div><strong>{title}</strong><small>{description}</small></div>
  return (
    <div className={styles.artifact} data-draft-revision={revision} data-artifact-compact={compact||undefined}>
      {compact ? <OverflowTooltip selector="strong,small">{content}</OverflowTooltip> : content}
      {indicator ? (
        <span className={styles.caption}>{indicator}</span>
      ) : href && action ? (
        <ChatActionLink to={href}>{action}</ChatActionLink>
      ) : onOpen && action ? (
        <ChatAction disabled={disabled} onClick={onOpen}>
          {action}
        </ChatAction>
      ) : null}
    </div>
  )
}
