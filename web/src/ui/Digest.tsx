import { msg, useLocale } from '../i18n'
import { useEffect, useState } from 'react'
import { Button } from './Button'
import { Popover } from './Popover'
import styles from './Digest.module.css'

/** Full, selectable digest values remain available to pointer, keyboard and touch. */
export function Digest({ value, prefix = 'sha256 ' }: { value: string; prefix?: string }) {
  useLocale()
  const [copied, setCopied] = useState(false)
  const [failed, setFailed] = useState(false)
  useEffect(() => { setCopied(false); setFailed(false) }, [value])
  if (!value) return <code>{prefix}(no file)</code>
  return <Popover title={msg("Full digest")} trigger={<Button className={styles.trigger} variant="quiet" aria-label={msg("Show full digest {{value0}}", { value0: value })}>
    <code>{prefix}{value.slice(0, 12)}{value.length > 12 ? '…' : ''}</code>
  </Button>}>
    <code className={styles.value}>{value}</code>
    <Button onClick={async () => {
      try { await navigator.clipboard.writeText(value); setCopied(true); setFailed(false) }
      catch { setCopied(false); setFailed(true) }
    }}>{msg("Copy digest")}</Button>
    <span role="status" className={styles.feedback}>{copied ? msg("Copied") : failed ? msg("Select the full value above to copy it.") : ''}</span>
  </Popover>
}
