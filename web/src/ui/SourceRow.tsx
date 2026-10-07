import type { ReactNode, MouseEventHandler } from 'react'
import { IconChevronRight, IconLink } from '../shell/icons'
import styles from './SourceRow.module.css'

/** One quiet row for a retained source or an external search lead. */
export function SourceRow({title,meta,icon,href,onClick}:{title:string;meta?:string;icon:ReactNode;href?:string;onClick?:MouseEventHandler<HTMLButtonElement>}) {
 const content=<><span className={styles.icon}>{icon}</span><span className={styles.text}><span className={styles.title}>{title}</span>{meta&&<small>{meta}</small>}</span><span className={styles.action}>{href?<IconLink/>:<IconChevronRight/>}</span></>
 return href?<a className={styles.row} aria-label={[title,meta].filter(Boolean).join(' ')} href={href} target="_blank" rel="noopener noreferrer">{content}</a>:<button type="button" className={styles.row} aria-label={[title,meta].filter(Boolean).join(' ')} onClick={onClick}>{content}</button>
}
