import { valueLabel } from '../terminology'
import { msg, useLocale } from '../../i18n'
import { PACK_TERMS } from '../terminology'
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
  useLocale()
  const { editing } = useEditing()
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>{PACK_TERMS.evidenceRequirements.label}</h2>
      <ul className={styles.cards}>
        {requirements.map((requirement, index) =>
          // Not an object: there are no fields to draw and nothing to point a
          // control at. The bytes are printed at their own pointer instead.
          !isRecord(requirement) ? (
            <li key={`misshapen-${index}`}>
              <MisshapenMember
                pointer={`${at}/${index}`}
                label={msg("Requirement {{value0}}", { value0: index + 1 })}
                expected={msg("an object")}
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
                      {requirement.required ? msg("required") : msg("optional")}
                    </span>
                    {requirement.kind !== undefined && (
                      <span className={styles.tagQuiet}>{valueLabel('kind', requirement.kind)}</span>
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
  useLocale()
  return (
    <>
      <StringField pointer={`${at}/id`} label={msg("id")} />
      <TextField pointer={`${at}/description`} label={msg("description")} rows={2} />
      {/*
        `required` is a boolean and is offered as the two words the document
        would spell. The reading view calls the false one "optional", which is
        English about what it means; the field writes `true` and `false`,
        which is what is on disk.
      */}
      <BooleanField pointer={`${at}/required`} label={msg("required")} />
      <EnumField pointer={`${at}/kind`} label={msg("kind")} options={ENUMS.evidenceKind} optional />
    </>
  )
}
