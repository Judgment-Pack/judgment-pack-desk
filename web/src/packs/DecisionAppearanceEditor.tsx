import { useId } from 'react'
import { msg, systemMessage, useLocale } from '../i18n'
import { Select } from '../ui/Select'
import { Button } from '../ui/Button'
import { decisionAccent, DECISION_COLORS, DECISION_MEANINGS, type DecisionAppearance, type DecisionMeaning, type DecisionColor } from './decisionAppearance'
import type { useDecisionAppearance } from './decisionAppearanceStorage'
import styles from './inspector/LogicInspector.module.css'
export function meaningLabel(meaning: DecisionMeaning): string {
  return ({ categorical: msg('Category only'), proceed: msg('Proceed'), review: msg('Review'), hold: msg('Hold'), handoff: msg('Handoff'), neutral: msg('Neutral') })[meaning]
}
export function colorLabel(color: DecisionColor): string {
  return ({ blue: msg('Blue'), violet: msg('Violet'), cyan: msg('Cyan'), rose: msg('Rose'), amber: msg('Amber'), green: msg('Green'), indigo: msg('Indigo'), slate: msg('Slate') })[color]
}
export function DecisionAppearanceEditor({ appearance, state, onChange }: { appearance: DecisionAppearance; state: ReturnType<typeof useDecisionAppearance>; onChange: (appearance: DecisionAppearance) => void }) {
  useLocale()
  const id = useId()
  const disabled = !state.ready || state.pending || Boolean(state.error)
  return <section className={styles.group} aria-labelledby={id + '-heading'}>
    <h3 id={id + '-heading'}>{msg('Decision appearance')}</h3>
    <p className={styles.meta}>{msg('Colors identify outcomes. Meanings are optional.')}</p>
    <div className={styles.appearanceField}><label htmlFor={id + '-meaning'}>{msg('Meaning')}</label>
      <Select id={id + '-meaning'} value={appearance.meaning} disabled={disabled} onValueChange={meaning => onChange({ ...appearance, meaning: meaning as DecisionMeaning })} options={DECISION_MEANINGS.map(value => ({ value, label: meaningLabel(value) }))} />
    </div>
    {appearance.meaning === 'categorical' && <div className={styles.appearanceField}><label htmlFor={id + '-color'}>{msg('Color')}</label>
      <Select id={id + '-color'} value={appearance.color} disabled={disabled} onValueChange={color => onChange({ ...appearance, color: color as DecisionColor })} options={DECISION_COLORS.map(value => ({ value, label: colorLabel(value) }))} />
    </div>}
    <p className={styles.meta}><span className={styles.appearanceSwatch} style={{ background: decisionAccent(appearance) }} aria-hidden="true" />{appearance.meaning === 'categorical' ? colorLabel(appearance.color) : meaningLabel(appearance.meaning)}</p>
    <p className={styles.meta} role="status">{state.pending ? msg('Saving…') : msg('Changes save for everyone using this desk. Evaluation is unchanged.')}</p>
    {state.error && <div role="alert"><p>{systemMessage(state.error.message)}</p><Button onClick={() => void state.reload()}>{msg('Reload')}</Button></div>}
  </section>
}
