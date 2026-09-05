/**
 * One line, where the project keeps a reviewed set.
 *
 * **No tool reports lock state.** None of the runtime's thirteen answers
 * carries a lock member, the Evaluation payload does not either, and `packs
 * lock` is a CLI verb (ADR-0019). So the desk cannot know whether this pack is
 * in the reviewed set, whether the set is current, or whether saving these
 * bytes takes it out — and it must not compute any of the three, because each
 * would be a verdict dressed as a fact.
 *
 * What the desk *can* see is that `jpack.lock.json` is in the file listing.
 * That is a fact about the project and is all this says: the project keeps a
 * reviewed set, and updating it is the project's own step. Where the file is
 * not listed, this renders nothing at all — silence, rather than "this project
 * keeps no reviewed set", which would be a claim about a file that may simply
 * not have been read.
 */
import styles from './LockLine.module.css'

/** The conventional name, which is the only thing the desk matches on. */
export const LOCK_FILE = 'jpack.lock.json'

export function LockLine({ paths }: { paths: readonly string[] }) {
  const listed = paths.some((path) => path === LOCK_FILE || path.endsWith(`/${LOCK_FILE}`))
  if (!listed) return null
  return (
    <p className={styles.lock}>
      This project keeps a reviewed set. Updating it is the project&rsquo;s own step.
    </p>
  )
}
