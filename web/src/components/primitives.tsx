import type { ReactNode } from 'react'

/** A titled block. A section with nothing in it renders nothing at all. */
export function Section({
  title,
  count,
  children
}: {
  title: string
  count?: number
  children: ReactNode
}) {
  if (children === null || children === undefined || children === false) return null
  return (
    <section className="section">
      <h2 className="section-title">
        {title}
        {count !== undefined && <span className="count">{count}</span>}
      </h2>
      {children}
    </section>
  )
}

/** Pretty-printed JSON. Conditions are shown this way rather than paraphrased:
 *  a paraphrase of a policy condition is a claim about what it means. */
export function Json({ value, label }: { value: unknown; label?: string }) {
  return (
    <figure className="json">
      {label && <figcaption>{label}</figcaption>}
      <pre>
        <code>{JSON.stringify(value, null, 2)}</code>
      </pre>
    </figure>
  )
}

export function Pill({
  children,
  tone = 'neutral'
}: {
  children: ReactNode
  tone?: 'neutral' | 'strong' | 'quiet' | 'success' | 'skipped' | 'danger'
}) {
  return <span className={`pill pill-${tone}`}>{children}</span>
}

/**
 * The tone one of the runtime's own suite statuses reads as. Success is
 * reserved for `passed`; `skipped` is a non-answer and must never wear the
 * success accent; everything else is the runtime saying no.
 */
export function statusTone(status: string): 'success' | 'skipped' | 'danger' {
  if (status === 'passed') return 'success'
  if (status === 'skipped') return 'skipped'
  return 'danger'
}

export function Fields({ items }: { items: [string, ReactNode][] }) {
  const present = items.filter(([, value]) => value !== undefined && value !== null && value !== '')
  if (present.length === 0) return null
  return (
    <dl className="fields">
      {present.map(([label, value]) => (
        <div key={label} className="field">
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="empty">{children}</p>
}

export function ErrorBox({ title, error }: { title: string; error: Error }) {
  return (
    <div className="error-box" role="alert">
      <strong>{title}</strong>
      <p>{error.message}</p>
    </div>
  )
}

export function Loading({ what }: { what: string }) {
  return <p className="loading">Loading {what}…</p>
}
