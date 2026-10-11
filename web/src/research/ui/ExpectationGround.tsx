import { msg, useLocale } from '../../i18n'
import { Button } from '../../ui/Button'
import { citedPage, caseStatements, statementTurns, type Citation, type RunState } from '../run'
import type { Selection } from './DraftPanels'
import styles from './ResearchAuthoring.module.css'

/** Opens a page a chat draft cites, in the source reader at its quote. */
export type ReadPage = (citation: Citation, opener: HTMLElement) => void

/**
 * What a case's expectation rests on, where the expectation is shown. A
 * recorded excerpt is a link into the Inspector, as before. A chat draft's
 * case rests instead on a page the draft cites, which opens in the source
 * reader, or on one of the person's own messages, which is shown in their
 * words: there is nothing to open for it.
 */
export function ExpectationGround({ source, state, onSelect, onReadPage }: { source: string; state: RunState; onSelect: (next: Selection) => void; onReadPage?: ReadPage }) {
  useLocale()
  const inherited = state.inheritedStatements !== undefined
  const said = !inherited && statementTurns(state.turns).find(item => item.id === source)
  const statement = caseStatements(state).get(source)
  if (!said && statement !== undefined) return <div><div>{msg('Statement used by this test')}</div><div className={styles.hint}>{statement.length > 280 ? `${statement.slice(0,280)}…` : statement}</div></div>
  if (said) {
    const words = said.turn.text.length > 280 ? `${said.turn.text.slice(0, 280)}…` : said.turn.text
    return <div><div>{msg('Your message {{number}}', { number: Number(source.slice('you-'.length)) })}</div><div className={styles.hint}>{words}</div></div>
  }
  const page = citedPage(source)
  if (page) {
    const citation = state.citations.find(item => item.traced && item.location === source)
    return <Button variant="inline" disabled={!citation || !onReadPage} onClick={event => { if (citation) onReadPage?.(citation, event.currentTarget) }}>
      {msg('Page {{number}}', { number: page.page })}
    </Button>
  }
  return <Button variant="inline" onClick={() => onSelect({ kind: 'excerpt', id: source })}>{source}</Button>
}

/** Whether an expectation rests on something other than a recorded excerpt. */
export function groundedInChat(source: string, state: RunState): boolean {
  return citedPage(source) !== null || caseStatements(state).has(source)
}
