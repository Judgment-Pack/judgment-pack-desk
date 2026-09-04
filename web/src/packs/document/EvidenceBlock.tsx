/** What the pack requires before it decides, as the document declares it. */
import type { EvidenceRequirement } from '../../mcp/types'
import { useEditing } from '../edit/editingContext'
import { BooleanField, EnumField, StringField, TextField } from '../edit/fields'
import { ENUMS } from '../edit/shape'
import { Block } from './Block'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'
import { MisshapenMember, isRecord } from './MisshapenMember'

export function EvidenceBlock({
  requirements,
  at
}: {
  requirements: EvidenceRequirement[]
  at: string
}) {
  const { editing } = useEditing()
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Evidence requirements</h2>
      <ul className={styles.cards}>
        {requirements.map((requirement, index) =>
          // Not an object: there are no fields to draw and nothing to point a
          // control at. The bytes are printed at their own pointer instead.
          !isRecord(requirement) ? (
            <li key={`misshapen-${index}`}>
              <MisshapenMember
                pointer={`${at}/${index}`}
                label={`Requirement ${index + 1}`}
                expected="an object"
                value={requirement}
              />
            </li>
          ) : (
          <li key={`${requirement.id}-${index}`}>
            <Block pointer={`${at}/${index}`} as="div" className={styles.card}>
              {editing ? (
                <RequirementForm at={`${at}/${index}`} />
              ) : (
                <>
                  <p className={styles.cardHead}>
                    <code className={styles.id}>{requirement.id}</code>
                    <span className={styles.tag}>
                      {requirement.required ? 'required' : 'optional'}
                    </span>
                    {requirement.kind !== undefined && (
                      <span className={styles.tagQuiet}>{requirement.kind}</span>
                    )}
                  </p>
                  <p>{requirement.description}</p>
                </>
              )}
              <ExtensionsBlock
                extensions={requirement.extensions}
                at={`${at}/${index}/extensions`}
              />
            </Block>
          </li>
          )
        )}
      </ul>
    </Block>
  )
}

function RequirementForm({ at }: { at: string }) {
  return (
    <>
      <StringField pointer={`${at}/id`} label="id" />
      <TextField pointer={`${at}/description`} label="description" rows={2} />
      {/*
        `required` is a boolean and is offered as the two words the document
        would spell. The reading view calls the false one "optional", which is
        English about what it means; the field writes `true` and `false`,
        which is what is on disk.
      */}
      <BooleanField pointer={`${at}/required`} label="required" />
      <EnumField pointer={`${at}/kind`} label="kind" options={ENUMS.evidenceKind} optional />
    </>
  )
}
