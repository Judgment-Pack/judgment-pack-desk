/**
 * The metadata, including `metadata.reviews` — which the view this replaces
 * dropped entirely.
 *
 * **Render-only, and that is a decision rather than a phase.** This surface has
 * no reviewer identity: the desk knows who is at the keyboard only as "local
 * user", so a review written here would be signed by nobody. A review is a
 * statement by a person about a document, and a page that cannot say which
 * person must not offer to write one.
 */
import type { PackMetadata } from '../../mcp/types'
import { Block } from './Block'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'

export function MetadataBlock({ metadata, at }: { metadata: PackMetadata; at: string }) {
  const reviews = metadata.reviews ?? []
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Metadata</h2>
      <dl className={styles.fields}>
        {metadata.authors !== undefined && (
          <div className={styles.field}>
            <dt>Authors</dt>
            <dd>
              <Block pointer={`${at}/authors`} as="span">
                {metadata.authors.join(', ')}
              </Block>
            </dd>
          </div>
        )}
        {metadata.createdAt !== undefined && (
          <div className={styles.field}>
            <dt>Created</dt>
            <dd>
              <Block pointer={`${at}/createdAt`} as="span">
                {metadata.createdAt}
              </Block>
            </dd>
          </div>
        )}
        {metadata.license !== undefined && (
          <div className={styles.field}>
            <dt>License</dt>
            <dd>
              <Block pointer={`${at}/license`} as="span">
                {metadata.license}
              </Block>
            </dd>
          </div>
        )}
        {metadata.requiredExtensions !== undefined && (
          <div className={styles.field}>
            <dt>Required extensions</dt>
            <dd>
              <Block pointer={`${at}/requiredExtensions`} as="span" className={styles.refs}>
                {metadata.requiredExtensions.map((name) => (
                  <code key={name} className={styles.id}>
                    {name}
                  </code>
                ))}
              </Block>
            </dd>
          </div>
        )}
      </dl>
      {metadata.reviews !== undefined && (
        <Block pointer={`${at}/reviews`}>
          <h3 className={styles.subheading}>Reviews</h3>
          <p className={styles.note}>
            Recorded in the document. This page does not write one.
          </p>
          <ul className={styles.cards}>
            {reviews.map((review, index) => (
              <li key={`${review.reviewer}-${index}`}>
                <Block pointer={`${at}/reviews/${index}`} as="div" className={styles.card}>
                  <p className={styles.cardHead}>
                    <span className={styles.chipLabel}>{review.reviewer}</span>
                    <span className={styles.tag}>{review.disposition}</span>
                    <span className={styles.tagQuiet}>{review.reviewedAt}</span>
                  </p>
                  {review.note !== undefined && <p>{review.note}</p>}
                </Block>
              </li>
            ))}
          </ul>
        </Block>
      )}
      <ExtensionsBlock extensions={metadata.extensions} at={`${at}/extensions`} />
    </Block>
  )
}
