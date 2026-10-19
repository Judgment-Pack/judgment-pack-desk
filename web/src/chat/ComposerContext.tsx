import type { MouseEventHandler } from 'react'
import { msg } from '../i18n'
import { IconPack, IconGraph, IconMatrix, IconChevronRight } from '../shell/icons'
import { OverflowTooltip } from '../ui/Tooltip'
import styles from './ChatWorkspace.module.css'

/** The owner of a conversation stays visible beside its composer in every view. */
export function ComposerContext({ title, kind = 'pack', detail, onOpen, openLabel }: {
  title: string; kind?: 'pack' | 'graph' | 'input' | 'policy'; detail?: string
  onOpen?: MouseEventHandler<HTMLButtonElement>; openLabel?: string
}) {
  const content = <>{kind==='input'?<IconMatrix/>:kind === 'graph' ? <IconGraph/> : <IconPack/>}<span className={styles.contextText}>
    <span>{title}</span><small>{detail || (kind==='policy'?msg('Policy'):kind==='input'?msg('Dialects'):kind === 'graph' ? msg('Graph') : msg('Pack'))}</small>
  </span>{openLabel && <small style={{flexShrink:0}}>{openLabel}</small>}{onOpen && <IconChevronRight/>}</>
  return <div className={styles.composerContext} aria-label={msg('Chat context')}>
    {onOpen ? <OverflowTooltip content={title} selector="span > span"><button type="button" onClick={onOpen} aria-label={openLabel ?? msg('View context for {{name}}', { name: title })}>{content}</button></OverflowTooltip>
      : <div>{content}</div>}
  </div>
}
