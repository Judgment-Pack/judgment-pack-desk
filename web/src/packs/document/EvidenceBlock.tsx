/** What the pack requires before it decides, as the document declares it. */
import type { EvidenceRequirement } from '../../mcp/types'
import { Block } from './Block'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'

export function EvidenceBlock({
  requirements,
  at
}: {
  requirements: EvidenceRequirement[]
  at: string
}) {
  return (
    <Block pointer={at}>
      <h2 className={styles.heading}>Evidence requirements</h2>
      <ul className={styles.cards}>
        {requirements.map((requirement, index) => (
          <li key={`${requirement.id}-${index}`}>
            <Block pointer={`${at}/${index}`} as="div" className={styles.card}>
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
              <ExtensionsBlock
                extensions={requirement.extensions}
                at={`${at}/${index}/extensions`}
              />
            </Block>
          </li>
        ))}
      </ul>
    </Block>
  )
}
