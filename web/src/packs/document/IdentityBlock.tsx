/**
 * The identity members: what this document is, above the question it answers.
 *
 * **One component, one member.** These were drawn as a single block in a fixed
 * title/version/specVersion/id/description order, and that order was the page's
 * rather than the document's: a pack written with `decision` before
 * `specVersion`, `id` and `version` had those three moved in front of
 * `decision`, so a reordered document was silently reordered back by the view
 * whose whole claim is that it shows the document's own order. Each member now
 * finds its own place, and `members.ts` keeps them under one outline entry —
 * which is a nav question and is answered where nav is decided.
 */
import type { PackDocument } from '../../mcp/types'
import { Block } from './Block'
import styles from './PackDocument.module.css'

export function TitleBlock({ document: doc }: { document: PackDocument }) {
  return (
    <Block pointer="/title" className={styles.identity}>
      <p className={styles.eyebrow}>
        <span className={styles.eyebrowTitle}>{doc.title}</span>
      </p>
    </Block>
  )
}

export function VersionBlock({ document: doc }: { document: PackDocument }) {
  return (
    <Block pointer="/version" className={styles.identity}>
      <p className={styles.eyebrow}>
        <span className={styles.eyebrowPart}>v{doc.version}</span>
      </p>
    </Block>
  )
}

export function SpecVersionBlock({ document: doc }: { document: PackDocument }) {
  return (
    <Block pointer="/specVersion" className={styles.identity}>
      <p className={styles.eyebrow}>
        <span className={styles.eyebrowPart}>specVersion {doc.specVersion}</span>
      </p>
    </Block>
  )
}

export function IdBlock({ document: doc }: { document: PackDocument }) {
  return (
    <Block pointer="/id" className={styles.identity}>
      <p className={styles.identityId}>
        <code className={styles.id}>{doc.id}</code>
      </p>
    </Block>
  )
}

export function DescriptionBlock({ document: doc }: { document: PackDocument }) {
  return (
    <Block pointer="/description" className={styles.identity}>
      <p className={styles.identityDescription}>{doc.description}</p>
    </Block>
  )
}
