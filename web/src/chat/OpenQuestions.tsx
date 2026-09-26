import type { ReactNode } from 'react'
import type { ChatAttachment } from './store'
import { msg, useLocale } from '../i18n'
import { Disclosure } from '../ui/Disclosure'
import { MessageRenderer } from './MessageRenderer'
import type { ReadMessageDetail } from './MessageSources'
import styles from './ChatWorkspace.module.css'

/** The saved unknowns field is a list of uncertainties, not an extra answer. */
export function openQuestions(text: string): string[] {
  return text.split(/(?:^|\r?\n)•[ \t]*/).map(item => item.trim()).filter(Boolean)
}
export function OpenQuestions({text, documents, onRead, children}: {text: string; documents?: ChatAttachment[]; onRead?: ReadMessageDetail; children?: ReactNode}) {
  useLocale()
  const questions = openQuestions(text)
  return <Disclosure className={styles.openQuestions} title={<>{msg('Assumptions and open questions')} · {questions.length}</>}>
    <p>{msg('Points the assistant could not confirm from the supplied information.')}</p>
    <ul>{questions.map((question, index) => <li key={index}><MessageRenderer text={question} documents={documents} onRead={onRead}/></li>)}</ul>
    {children}
  </Disclosure>
}
