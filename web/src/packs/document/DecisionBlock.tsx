/**
 * The decision: the question as the page's h1, the intent as the standfirst.
 *
 * The question is the heading because it is what the document is for — a pack
 * is an answer to one question — and the intent is the sentence under it that
 * says what answering it is meant to achieve. Both are the document's own
 * words.
 */
import type { Decision } from '../../mcp/types'
import { useEditing } from '../edit/editingContext'
import { TextField } from '../edit/fields'
import { Block } from './Block'
import { ExtensionsBlock } from './ExtensionsBlock'
import styles from './PackDocument.module.css'

export function DecisionBlock({ decision, at }: { decision: Decision; at: string }) {
  const { editing } = useEditing()
  if (editing) {
    return (
      <Block pointer={at} className={styles.decision}>
        <TextField pointer={`${at}/question`} label="question" rows={2} />
        <TextField pointer={`${at}/intent`} label="intent" rows={2} />
        <ExtensionsBlock extensions={decision.extensions} at={`${at}/extensions`} />
      </Block>
    )
  }
  return (
    <Block pointer={at} className={styles.decision}>
      <Block pointer={`${at}/question`} as="h1" className={styles.question}>
        {decision.question}
      </Block>
      {decision.intent !== undefined && (
        <Block pointer={`${at}/intent`} as="p" className={styles.standfirst}>
          {decision.intent}
        </Block>
      )}
      <ExtensionsBlock extensions={decision.extensions} at={`${at}/extensions`} />
    </Block>
  )
}
