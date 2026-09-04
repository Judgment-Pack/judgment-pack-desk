/** What the pack cites. A locator is shown as the document spells it. */
import type { Source } from '../../mcp/types'
import { Block } from './Block'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'

export function SourcesBlock({ sources, at }: { sources: Source[]; at: string }) {
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Sources</h2>
      <ul className={styles.cards}>
        {sources.map((source, index) => (
          <li key={`${source.id}-${index}`}>
            <Block pointer={`${at}/${index}`} as="div" className={styles.card}>
              <p className={styles.cardHead}>
                <span className={styles.chipLabel}>{source.title}</span>
                <code className={styles.id}>{source.id}</code>
              </p>
              <dl className={styles.fields}>
                {source.publisher !== undefined && (
                  <div className={styles.field}>
                    <dt>Publisher</dt>
                    <dd>{source.publisher}</dd>
                  </div>
                )}
                {source.publishedAt !== undefined && (
                  <div className={styles.field}>
                    <dt>Published</dt>
                    <dd>{source.publishedAt}</dd>
                  </div>
                )}
                {source.locator !== undefined && (
                  <div className={styles.field}>
                    <dt>Locator</dt>
                    <dd>
                      <Block pointer={`${at}/${index}/locator`} as="span">
                        <code className={styles.literal}>{source.locator.value}</code>{' '}
                        <span className={styles.tagQuiet}>{source.locator.kind}</span>
                      </Block>
                    </dd>
                  </div>
                )}
                {source.rights !== undefined && (
                  <div className={styles.field}>
                    <dt>Rights</dt>
                    <dd>{source.rights}</dd>
                  </div>
                )}
              </dl>
              {source.citation !== undefined && (
                <Block pointer={`${at}/${index}/citation`} as="blockquote" className={styles.citation}>
                  <p>{source.citation.excerpt}</p>
                  <cite>{source.citation.location}</cite>
                </Block>
              )}
              <ExtensionsBlock extensions={source.extensions} at={`${at}/${index}/extensions`} />
            </Block>
          </li>
        ))}
      </ul>
    </Block>
  )
}
