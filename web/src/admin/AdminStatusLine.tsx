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
 * **One line, and two on a narrow shell**, which is what `flex-wrap` is for
 * rather than a breakpoint: the pairs are as wide as their values, and a long
 * path wraps the strip rather than being truncated into a path that is not the
 * one on disk.
 */
import type { ReactNode } from 'react'
import styles from './AdminStatusLine.module.css'

export function AdminStatusLine({
  runtime,
  binary,
  projectFile,
  deskFile
}: {
  /** The connection, in the connection's own words. */
  runtime: ReactNode
  /** The binary the chassis was launched with, as the chassis reports it. */
  binary: ReactNode
  /** This project's own configuration file. */
  projectFile: ReactNode
  /** This desk's file, on the machine rather than in the project. */
  deskFile: ReactNode
}) {
  return (
    <dl className={styles.line}>
      <Pair label="Runtime">{runtime}</Pair>
      <Pair label="Binary">{binary}</Pair>
      <Pair label="This project">{projectFile}</Pair>
      <Pair label="This desk">{deskFile}</Pair>
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
