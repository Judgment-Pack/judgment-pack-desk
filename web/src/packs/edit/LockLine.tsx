import { msg, useLocale } from '../../i18n'
/**
 * One line, where the project keeps a reviewed set.
 *
 * **The editor computes no lock state.** Whether this pack is in the reviewed
 * set, and whether the set is current, is the runtime's `packs verify` to say,
 * and Desk shows that answer where it asks for it: beside each pack's name on
 * Packs, and in Review and lock (ADR-0009, section 2), which is also where the
 * lock is updated. A deciding run's own payload says `reviewed` (runtime
 * 0.24.0 and later), but the editor makes none. So this page does not say
 * whether saving these bytes takes the pack out of the set: that would be a
 * verdict dressed as a fact.
 *
 * What the editor *can* see is that `jpack.lock.json` is in the file listing.
 * That is a fact about the project and is all this says: the project keeps a
 * reviewed set, and Review and lock is where it is reviewed and updated.
 * Where the file is not listed, this renders nothing at all — silence, rather
 * than "this project keeps no reviewed set", which would be a claim about a
 * file that may simply not have been read.
 */
import styles from './LockLine.module.css'

/** The conventional name, which is the only thing the desk matches on. */
export const LOCK_FILE = 'jpack.lock.json'

export function LockLine({ paths }: { paths: readonly string[] }) {
  useLocale()
  const listed = paths.some((path) => path === LOCK_FILE || path.endsWith(`/${LOCK_FILE}`))
  if (!listed) return null
  return (
    <p className={styles.lock}>{msg("This project keeps a reviewed set. Review and lock, on Packs, shows what the runtime finds and updates it.")}</p>
  )
}
