/**
 * The runtime's diagnostics, printed as the runtime wrote them.
 *
 * Factored out of the Checks panel because the Create dialog refuses a proposal
 * on the same answer and must not describe it in words of its own. A second
 * rendering of one refusal is a second refusal: the panel would say `code`,
 * `layer`, `severity` and the pointer, and the dialog would say a sentence
 * somebody wrote about the first of them.
 *
 * **Nothing here is translated and nothing is coloured by a rule of this
 * desk's own.** The severity is the runtime's word for how bad it is, and a
 * second opinion in a colour would be a verdict. The pointer is printed
 * verbatim — `named`, which is the diagnostic's own `instancePath` — because a
 * reformatted path is a path this desk invented.
 *
 * **Every diagnostic, not the first.** A runtime that reports independent
 * errors at `/rules/0` and `/outcomes/1` is describing two problems, and a
 * caller shown one of them cannot see what is wrong with the document.
 */
import type { AnchoredDiagnostic } from './checks'
import styles from './inspector/PackInspector.module.css'

export function DiagnosticList({
  diagnostics,
  label
}: {
  diagnostics: readonly AnchoredDiagnostic[]
  /** An accessible name, where two lists could be on one page. */
  label?: string
}) {
  if (diagnostics.length === 0) return null
  return (
    <ul className={styles.diagnostics} aria-label={label}>
      {diagnostics.map((entry, index) => (
        <li key={`${entry.diagnostic.code}-${index}`} className={styles.diagnostic}>
          <p className={styles.diagnosticHead}>
            <code className={styles.code}>{entry.diagnostic.code}</code>
            {entry.diagnostic.layer !== undefined && (
              <span className={styles.word}>{entry.diagnostic.layer}</span>
            )}
            {entry.diagnostic.severity !== undefined && (
              <span className={styles.word}>{entry.diagnostic.severity}</span>
            )}
            {entry.diagnostic.codeStability !== undefined && (
              <span className={styles.word}>{entry.diagnostic.codeStability}</span>
            )}
          </p>
          <p className={styles.message}>{entry.diagnostic.message}</p>
          <p className={styles.pointer}>
            <code>{entry.named}</code>
          </p>
        </li>
      ))}
    </ul>
  )
}
