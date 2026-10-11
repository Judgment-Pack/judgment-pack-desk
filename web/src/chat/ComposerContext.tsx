import type { MouseEventHandler } from 'react'
import { msg } from '../i18n'
import { IconPack, IconGraph, IconChevronRight } from '../shell/icons'
import { OverflowTooltip } from '../ui/Tooltip'
import styles from './ChatWorkspace.module.css'

/** The owner of a conversation stays visible beside its composer in every view. */
export function ComposerContext({ title, kind = 'pack', detail, onOpen }: {
  title: string; kind?: 'pack' | 'graph'; detail?: string
  onOpen?: MouseEventHandler<HTMLButtonElement>
}) {
  const content = <>{kind === 'graph' ? <IconGraph/> : <IconPack/>}<span className={styles.contextText}>
    <span>{title}</span><small>{detail || (kind === 'graph' ? msg('Graph') : msg('Pack'))}</small>
  </span>{onOpen && <IconChevronRight/>}</>
  return <div className={styles.composerContext} aria-label={msg('Chat context')}>
    {onOpen ? <OverflowTooltip content={title} selector="span > span"><button type="button" onClick={onOpen} aria-label={msg('View context for {{name}}', { name: title })}>{content}</button></OverflowTooltip>
      : <div>{content}</div>}
  </div>
}
