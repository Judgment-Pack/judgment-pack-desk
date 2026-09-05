/**
 * A text input that offers what the project has already consulted, and takes
 * anything.
 *
 * A `fact` condition's `path` is an RFC 6901 pointer into a facts document
 * nobody has yet written. The inventory reports the paths this project's packs
 * already consult, which is a useful list and is **not** a closed one: a rule
 * about a fact nothing consults yet is the ordinary case for a new rule, and a
 * control that refused it would refuse the edit the author came to make.
 *
 * So it is `<input list>` with a `<datalist>` and not a combobox: the value is
 * free text, the suggestions are a convenience, and a path that is in no list
 * is typed and kept. `radix-ui` 1.6.7 ships no combobox, and the two Radix
 * shapes that come close — `Select` and `DropdownMenu` — are both closed sets.
 */
import { useId, type InputHTMLAttributes } from 'react'
import styles from './SuggestInput.module.css'

export function SuggestInput({
  suggestions,
  className,
  ...rest
}: InputHTMLAttributes<HTMLInputElement> & { suggestions: readonly string[] }) {
  const listId = useId()
  return (
    <>
      <input
        {...rest}
        type="text"
        list={suggestions.length > 0 ? listId : undefined}
        className={[styles.input, className].filter(Boolean).join(' ')}
      />
      {suggestions.length > 0 && (
        <datalist id={listId}>
          {suggestions.map((suggestion) => (
            <option key={suggestion} value={suggestion} />
          ))}
        </datalist>
      )}
    </>
  )
}
