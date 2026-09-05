/**
 * A monospace editor with line numbers, and no editor dependency.
 *
 * Raw mode is the same buffer the forms write, shown as the bytes it is. What
 * that needs is a textarea that does not reflow, a gutter that says which line
 * a parse error names, and nothing else — no syntax tree, no folding, no
 * second model of the document. A code-editor package would bring all three
 * and its own idea of what the text is.
 *
 * **The gutter is `aria-hidden` and is not focusable.** Line numbers are a
 * rendering of the text a screen reader already has, and announcing "1 2 3 4"
 * before the content is noise. It scrolls with the textarea rather than with
 * the page: the two are separate scroll containers, and a gutter that did not
 * follow would number the wrong lines the moment the buffer is longer than the
 * box.
 */
import { useRef, type TextareaHTMLAttributes } from 'react'
import styles from './CodeArea.module.css'

export function CodeArea({
  value,
  className,
  ...rest
}: TextareaHTMLAttributes<HTMLTextAreaElement> & { value: string }) {
  const gutter = useRef<HTMLDivElement | null>(null)
  const lines = countLines(value)
  return (
    <div className={styles.frame}>
      <div className={styles.gutter} aria-hidden="true" ref={gutter} data-testid="code-gutter">
        {lines.map((line) => (
          <span key={line} className={styles.number}>
            {line}
          </span>
        ))}
      </div>
      <textarea
        {...rest}
        value={value}
        spellCheck={false}
        wrap="off"
        className={[styles.area, className].filter(Boolean).join(' ')}
        onScroll={(event) => {
          const node = gutter.current
          if (node !== null) node.scrollTop = event.currentTarget.scrollTop
          rest.onScroll?.(event)
        }}
      />
    </div>
  )
}

/**
 * The line numbers this text has.
 *
 * A trailing newline does not open a line that is not there: `"a\n"` is one
 * line, and numbering the empty tail 2 would put a number beside nothing. An
 * empty buffer is still line 1, because that is where the cursor is.
 */
function countLines(value: string): number[] {
  const count = Math.max(1, value.split('\n').length - (value.endsWith('\n') ? 1 : 0))
  return Array.from({ length: count }, (_, index) => index + 1)
}
