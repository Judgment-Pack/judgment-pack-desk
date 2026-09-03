/**
 * An `extensions` object, at the root or inside one of the eight members that
 * may carry one.
 *
 * Dropped by the view this replaces. The spec does not interpret an extension
 * and neither does this: it is printed as the JSON the document carries, at
 * its own pointer, because a namespaced member someone put in a pack is part
 * of what the pack says.
 */
import { Block } from './Block'
import styles from './PackDocument.module.css'

export function ExtensionsBlock({
  extensions,
  at,
  heading
}: {
  extensions: Record<string, unknown> | undefined
  at: string
  /** A heading where this is a member's own block; omitted inside a card. */
  heading?: string
}) {
  if (extensions === undefined) return null
  return (
    <Block pointer={at} className={styles.extensions}>
      {heading !== undefined && <h2 className={styles.heading}>{heading}</h2>}
      {heading === undefined && <p className={styles.fieldLabel}>extensions</p>}
      <ul className={styles.extensionList}>
        {Object.entries(extensions).map(([name, value]) => (
          <li key={name}>
            <Block pointer={`${at}/${name.replaceAll('~', '~0').replaceAll('/', '~1')}`} as="div">
              <code className={styles.id}>{name}</code>{' '}
              <code className={styles.literal}>{JSON.stringify(value)}</code>
            </Block>
          </li>
        ))}
      </ul>
    </Block>
  )
}
