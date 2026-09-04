/**
 * A field that is still a block: one pointer, one control, and the diagnostics
 * about that pointer printed under it.
 *
 * **The address does not change when the document becomes a form.** The
 * reading view addresses `/rules/0/description` with `data-pointer` and an
 * element id, a diagnostic anchors on that string, a deep link reaches it and
 * the Inspector selects it. Edit mode replaces what is *inside* that address
 * and keeps the address, so all four keep working and the diagnostic the
 * runtime issued about a member lands on the control that writes it — rather
 * than on the card above it, which is where anchoring lands anything it cannot
 * place exactly.
 *
 * The label, the `aria-describedby` and the `aria-invalid` are `ui/Field`'s,
 * so this file wires none of them itself. What it adds is the diagnostics:
 * `Field` prints whatever `error` it is handed and names it in
 * `aria-describedby`, so a screen reader reaching the input is told the
 * runtime's own code and message rather than that the field is invalid.
 *
 * **The diagnostics are the runtime's words.** Nothing here summarises,
 * re-classifies or colours by severity beyond the class the sheet holds: the
 * code, the severity and the message are printed as they arrived, and where
 * the report is about bytes that have moved the caller passes none at all.
 */
import type { ReactNode } from 'react'
import type { AnchoredDiagnostic } from '../checks'
import { elementIdFor } from '../pointers'
import { Field, type FieldWiring } from '../../ui/Field'
import { useEditing } from './editingContext'
import styles from './PointerField.module.css'

export function PointerField({
  pointer,
  label,
  hint,
  children
}: {
  pointer: string
  label: string
  hint?: ReactNode
  children: (wiring: FieldWiring) => ReactNode
}) {
  const { diagnosticsAt } = useEditing()
  const found = diagnosticsAt(pointer)
  return (
    <div id={elementIdFor(pointer)} data-pointer={pointer} className={styles.field}>
      <Field
        label={label}
        hint={hint}
        error={found.length === 0 ? undefined : <Diagnostics found={found} />}
      >
        {children}
      </Field>
    </div>
  )
}

function Diagnostics({ found }: { found: readonly AnchoredDiagnostic[] }) {
  return (
    <span className={styles.diagnostics}>
      {found.map((entry, index) => (
        <span className={styles.diagnostic} key={`${entry.diagnostic.code ?? 'd'}-${index}`}>
          {entry.diagnostic.code !== undefined && (
            <code className={styles.code}>{entry.diagnostic.code}</code>
          )}
          {entry.diagnostic.severity !== undefined && (
            <span className={styles.severity}>{entry.diagnostic.severity}</span>
          )}
          <span>{entry.diagnostic.message}</span>
          {/*
            The pointer, where it is not this field's own. Anchoring sends a
            diagnostic to the nearest rendered ancestor when its own member is
            not on the page, so a field that printed only the message would be
            saying a sentence about a descendant as though it were about itself.
          */}
          {entry.named !== '' && entry.named !== entry.anchor && (
            <code className={styles.named}>{entry.named}</code>
          )}
        </span>
      ))}
    </span>
  )
}
