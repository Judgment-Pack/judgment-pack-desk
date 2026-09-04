/** What the pack cites. A locator is shown as the document spells it. */
import type { Source } from '../../mcp/types'
import { useEditing } from '../edit/editingContext'
import { AbsentObject, EnumField, StringField, TextField } from '../edit/fields'
import { ENUMS } from '../edit/shape'
import { Block } from './Block'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'
import { MisshapenMember, isRecord } from './MisshapenMember'

export function SourcesBlock({ sources, at }: { sources: Source[]; at: string }) {
  const { editing } = useEditing()
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Sources</h2>
      <ul className={styles.cards}>
        {sources.map((source, index) =>
          // Not an object: there are no fields to draw and nothing to point a
          // control at. The bytes are printed at their own pointer instead.
          !isRecord(source) ? (
            <li key={`misshapen-${index}`}>
              <MisshapenMember
                pointer={`${at}/${index}`}
                label={`Source ${index + 1}`}
                expected="an object"
                value={source}
              />
            </li>
          ) : (
          <li key={`${source.id}-${index}`}>
            <Block pointer={`${at}/${index}`} as="div" className={styles.card}>
              {editing ? (
                <SourceForm at={`${at}/${index}`} />
              ) : (
                <SourceReading at={`${at}/${index}`} source={source} />
              )}
              <ExtensionsBlock extensions={source.extensions} at={`${at}/${index}/extensions`} />
            </Block>
          </li>
          )
        )}
      </ul>
    </Block>
  )
}

function SourceReading({ at, source }: { at: string; source: Source }) {
  return (
    <>
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
              <Block pointer={`${at}/locator`} as="span">
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
        <Block pointer={`${at}/citation`} as="blockquote" className={styles.citation}>
          <p>{source.citation.excerpt}</p>
          <cite>{source.citation.location}</cite>
        </Block>
      )}
    </>
  )
}

/**
 * The source's members, including the two objects under it.
 *
 * `locator` and `citation` are edited as their own members rather than as a
 * pair of composite controls: each has its own pointer, and a diagnostic about
 * `locator.value` anchors on the field that writes it.
 *
 * **Neither object's fields are drawn where the object is not there.** A field
 * whose container has no bytes takes a keystroke and writes nothing, and
 * `locator` is `required` — so the absence is stated with the offer to write
 * the schema's own members, empty, and the fields appear once it is.
 */
function SourceForm({ at }: { at: string }) {
  return (
    <>
      <StringField pointer={`${at}/id`} label="id" />
      <StringField pointer={`${at}/title`} label="title" />
      <StringField pointer={`${at}/publisher`} label="publisher" />
      <StringField pointer={`${at}/publishedAt`} label="published" hint="a date." />
      <AbsentObject pointer={`${at}/locator`} label="locator" what="a locator">
        <Block pointer={`${at}/locator`} as="div">
          <EnumField
            pointer={`${at}/locator/kind`}
            label="locator kind"
            options={ENUMS.locatorKind}
          />
          <StringField pointer={`${at}/locator/value`} label="locator" />
        </Block>
      </AbsentObject>
      <StringField pointer={`${at}/rights`} label="rights" />
      <AbsentObject pointer={`${at}/citation`} label="citation" what="a citation">
        <Block pointer={`${at}/citation`} as="div">
          <StringField pointer={`${at}/citation/location`} label="citation location" />
          <TextField pointer={`${at}/citation/excerpt`} label="citation excerpt" rows={2} />
        </Block>
      </AbsentObject>
    </>
  )
}
