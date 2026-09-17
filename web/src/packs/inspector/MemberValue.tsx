import { msg, useLocale } from '../../i18n'
import { CodeBlock } from '../../ui/CodeBlock'
import styles from './PackInspector.module.css'

/** Human prose stays prose. Structured values retain their complete JSON shape. */
export function MemberValue({ value }: { value: unknown }) {
  useLocale()
  if (value === undefined) return <p className={styles.empty}>{msg("Not declared.")}</p>
  if (typeof value === 'string') return <p className={styles.prose}>{value === '' ? msg("(empty string)") : value}</p>
  if (value === null || typeof value !== 'object') return <p className={styles.prose}><code>{JSON.stringify(value)}</code></p>
  return <CodeBlock text={JSON.stringify(value, null, 2)} />
}
