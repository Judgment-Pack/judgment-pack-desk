import { useEffect, useState } from 'react'
import { Button } from './Button'
import { Popover } from './Popover'
import styles from './Digest.module.css'

/** Full, selectable digest values remain available to pointer, keyboard and touch. */
export function Digest({ value, prefix = 'sha256 ' }: { value: string; prefix?: string }) {
  const [copied, setCopied] = useState(false)
  const [failed, setFailed] = useState(false)
  useEffect(() => { setCopied(false); setFailed(false) }, [value])
  if (!value) return <code>{prefix}(no file)</code>
  return <Popover title="Full digest" trigger={<Button className={styles.trigger} variant="quiet" aria-label={`Show full digest ${value}`}>
    <code>{prefix}{value.slice(0, 12)}{value.length > 12 ? '…' : ''}</code>
  </Button>}>
    <code className={styles.value}>{value}</code>
    <Button onClick={async () => {
      try { await navigator.clipboard.writeText(value); setCopied(true); setFailed(false) }
      catch { setCopied(false); setFailed(true) }
    }}>Copy digest</Button>
    <span role="status" className={styles.feedback}>{copied ? 'Copied' : failed ? 'Select the full value above to copy it.' : ''}</span>
  </Popover>
}
