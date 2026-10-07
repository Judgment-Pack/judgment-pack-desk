import { OverflowTooltip } from '../ui/Tooltip'
import { parents } from './browserState'
import styles from './FileLocation.module.css'

/** Quiet location context beside the filename. The file tree owns navigation. */
export function FileLocation({path}:{path:string}) {
 const folder=parents(path).at(-1)
 if(!folder)return null
 return <span className={styles.location} data-file-location>
  <OverflowTooltip><span className={styles.folder}>{folder}</span></OverflowTooltip>
  <span className={styles.separator} aria-hidden="true">/</span>
 </span>
}
