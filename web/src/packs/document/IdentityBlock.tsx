/**
 * The eyebrow: what this document is, above the question it answers.
 *
 * Title, version and specVersion on one line, then the id and the description.
 * Each is its own block with its own pointer, so a diagnostic at
 * `/specVersion` — which is where an unbundled specification version is
 * reported — lands on the thing it is about.
 */
import type { PackDocument } from '../../mcp/types'
import { Block } from './Block'
import styles from './PackDocument.module.css'

export function IdentityBlock({ document: doc }: { document: PackDocument }) {
  return (
    <Block pointer="/title" className={styles.identity}>
      <p className={styles.eyebrow}>
        <span className={styles.eyebrowTitle}>{doc.title}</span>
        {doc.version !== undefined && (
          <Block pointer="/version" as="span" className={styles.eyebrowPart}>
            v{doc.version}
          </Block>
        )}
        {doc.specVersion !== undefined && (
          <Block pointer="/specVersion" as="span" className={styles.eyebrowPart}>
            specVersion {doc.specVersion}
          </Block>
        )}
      </p>
      {doc.id !== undefined && (
        <Block pointer="/id" as="p" className={styles.identityId}>
          <code className={styles.id}>{doc.id}</code>
        </Block>
      )}
      {doc.description !== undefined && (
        <Block pointer="/description" as="p" className={styles.identityDescription}>
          {doc.description}
        </Block>
      )}
    </Block>
  )
}
