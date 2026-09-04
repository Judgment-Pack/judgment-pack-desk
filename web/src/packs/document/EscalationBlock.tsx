/**
 * Where the pack sends a decision it will not make, and what triggers that.
 *
 * A trigger is one of five **reason words** the spec closes the list of, not an
 * id of anything: `not-applicable`, `missing-required-evidence`, `unknown`,
 * `conflict`, `no-match`. So it is drawn as a word and not as an id chip, and
 * the References panel resolves nothing from it — see `packs/references.ts`.
 */
import type { Escalation } from '../../mcp/types'
import { Block } from './Block'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'

export function EscalationBlock({ escalation, at }: { escalation: Escalation; at: string }) {
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Escalation</h2>
      <dl className={styles.fields}>
        <div className={styles.field}>
          <dt>Target</dt>
          <dd>
            <Block pointer={`${at}/target`} as="span">
              {escalation.target?.name}{' '}
              <span className={styles.tagQuiet}>{escalation.target?.kind}</span>
            </Block>
          </dd>
        </div>
        <div className={styles.field}>
          <dt>Triggers</dt>
          <dd>
            <Block pointer={`${at}/triggers`} as="span" className={styles.refs}>
              {(escalation.triggers ?? []).map((trigger) => (
                <span key={trigger} className={styles.tagQuiet}>
                  {trigger}
                </span>
              ))}
            </Block>
          </dd>
        </div>
        {escalation.message !== undefined && (
          <div className={styles.field}>
            <dt>Message</dt>
            <dd>
              <Block pointer={`${at}/message`} as="span">
                {escalation.message}
              </Block>
            </dd>
          </div>
        )}
      </dl>
      <ExtensionsBlock extensions={escalation.extensions} at={`${at}/extensions`} />
    </Block>
  )
}
