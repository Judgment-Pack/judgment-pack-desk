/**
 * Where the pack sends a decision it will not make, and what triggers that.
 *
 * A trigger is one of five **reason words** the spec closes the list of, not an
 * id of anything: `not-applicable`, `missing-required-evidence`, `unknown`,
 * `conflict`, `no-match`. So it is drawn as a word and not as an id chip, the
 * References panel resolves nothing from it (see `packs/references.ts`), and
 * the form offers the five as checkboxes rather than as free text.
 */
import type { Escalation } from '../../mcp/types'
import { useEditing } from '../edit/editingContext'
import { AbsentObject, EnumField, StringField, StringListField, TextField } from '../edit/fields'
import { ENUMS } from '../edit/shape'
import { Block } from './Block'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'

export function EscalationBlock({ escalation, at }: { escalation: Escalation; at: string }) {
  const { editing } = useEditing()
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Escalation</h2>
      {editing ? (
        <>
          <StringListField
            pointer={`${at}/triggers`}
            label="triggers"
            candidates={ENUMS.triggers}
          />
          {/*
            The target is `required` and it is a member a draft escalation
            leaves out. Its two fields have nothing to splice into while the
            object is absent, so the absence is stated and offered rather than
            drawn as two controls that take a keystroke and write nothing.
          */}
          <AbsentObject pointer={`${at}/target`} label="target" what="a target">
            <Block pointer={`${at}/target`} as="div">
              <EnumField
                pointer={`${at}/target/kind`}
                label="target kind"
                options={ENUMS.targetKind}
              />
              <StringField pointer={`${at}/target/name`} label="target name" />
            </Block>
          </AbsentObject>
          <TextField pointer={`${at}/message`} label="message" rows={2} />
        </>
      ) : (
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
      )}
      <ExtensionsBlock extensions={escalation.extensions} at={`${at}/extensions`} />
    </Block>
  )
}
