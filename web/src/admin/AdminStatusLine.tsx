/**
 * What this desk is, on one line — the strip that replaced Admin's Runtime
 * card.
 *
 * **Status is not a setting, and a settings page carries only settings.** The
 * Runtime card had a Location, a Status, a Content disclosure and two fields,
 * and not one of them was editable: it reported which binary the chassis was
 * started with, whether the socket is up and whether the tool listing
 * answered. Four card slots for four facts nobody can change reads as a
 * setting somebody has not found the control for. So the facts are a line
 * under the heading, and the card is gone — its own content has a home in
 * Help & About, which is where a reader goes to find out what they are
 * connected to.
 *
 * **Every value is handed in.** This component composes no path, no version
 * and no verdict: `AdminView` passes the chassis' own answers, exactly as the
 * cards below it get them, so there is one producer of a location on this page
 * and it is the chassis.
 *
 * **The two configuration files are not here, and the omission is the point.**
 * The line carried them for one round and the group headers carried them too —
 * one path, two statements, and the grouping exists precisely so that a file is
 * named once. The header is the statement that earns its place: it is the file
 * the cards under it write. This line is what the desk is *running*, which is
 * the thing no card is about.
 *
 * **One line, and two on a narrow shell**, which is what `flex-wrap` is for
 * rather than a breakpoint: the pairs are as wide as their values, and a long
 * path wraps the strip rather than being truncated into a path that is not the
 * one on disk.
 */
import type { ReactNode } from 'react'
import styles from './AdminStatusLine.module.css'

export function AdminStatusLine({
  runtime,
  binary
}: {
  /** The connection, read off its status and never off retained metadata. */
  runtime: ReactNode
  /** The binary the chassis was launched with, as the chassis reports it. */
  binary: ReactNode
}) {
  return (
    <dl className={styles.line}>
      <Pair label="Runtime">{runtime}</Pair>
      <Pair label="Binary">{binary}</Pair>
    </dl>
  )
}

function Pair({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className={styles.pair}>
      <dt className={styles.key}>{label}</dt>
      <dd className={styles.value}>{children}</dd>
    </div>
  )
}
