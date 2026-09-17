import { type ReactNode } from 'react'
import { Trans } from 'react-i18next'
import { i18n, useLocale } from './index'

function Slot({ content }: { content: ReactNode }) { return <>{content}</> }
/** Translators may reorder inline values/links without touching their content. */
export function Message({ text, slots = [], values }: { text: string; slots?: ReactNode[]; values?: Record<string, unknown> }) {
  useLocale()
  return <Trans i18n={i18n} i18nKey={text} defaults={text} values={values}
    components={slots.map((content, index) => <Slot key={index} content={content} />)} />
}
