import { useState, type ReactNode } from 'react'
import { Button } from '../ui/Button'
import styles from './AdminStatusLine.module.css'

export function AdminStatusLine({
  runtime,
  binary,
  copyText
}: {
  /** The connection, read off its status and never off retained metadata. */
  runtime: ReactNode
  /** The binary the chassis was launched with, as the chassis reports it. */
  binary: ReactNode
  copyText?: string
}) {
  const [copied, setCopied] = useState<string>()
  return (
    <>
    <dl className={styles.line}>
      <Pair label="Runtime">{runtime}</Pair>
      <Pair label="Binary">{binary}</Pair>
    </dl>
    {copyText !== undefined && <div className={styles.actions}>
      <span role="status">{copied}</span>
      <Button onClick={async () => {
        try {
          await navigator.clipboard.writeText(copyText)
          setCopied('Copied')
        } catch { setCopied('Could not copy. Select the details to copy them.') }
      }}>Copy details</Button>
    </div>}
    </>
  )
}

function Pair({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className={styles.pair}>
      <dt className={styles.key}>{label}</dt>
      <dd className={styles.value}>{children}</dd>
    </div>
  )
}
