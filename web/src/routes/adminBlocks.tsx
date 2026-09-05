/**
 * The two things every Admin section prints: where a value came from, and the
 * exact JSON to paste to change it.
 *
 * Their own module rather than exports from `AdminView`, because the Assistant
 * section imports them and `AdminView` imports the Assistant section. A cycle
 * that a bundler resolves is still a cycle, and this one had nowhere to go but
 * here.
 */
import { useState } from 'react'
import { Json } from '../components/primitives'
import { IconCopy } from '../shell/icons'
import type { ValueSource } from '../config/deskConfig'

/**
 * Where an effective value came from, and where it did not.
 *
 * Three sources rather than two: a value can now come from the project's own
 * `jpack-desk.json`, from this machine's desk-level `desk.json`, or from the
 * built-in defaults. The badge names the file in each of the first two cases,
 * because "source: desk file" without a path is an answer nobody can act on.
 */
export function SourceBadge({
  source,
  path,
  deskPath
}: {
  source: ValueSource
  path: string
  deskPath?: string
}) {
  if (source === 'project file') {
    return (
      <span className="quiet">
        source: project file{' · '}
        <code>{path}</code>
      </span>
    )
  }
  if (source === 'desk file') {
    return (
      <span className="quiet">
        source: desk file
        {deskPath !== undefined && (
          <>
            {' · '}
            <code>{deskPath}</code>
          </>
        )}
      </span>
    )
  }
  return (
    <span className="quiet">
      source: default
      {' · no value for this section was read from '}
      <code>{path}</code>
      {deskPath !== undefined && (
        <>
          {' or '}
          <code>{deskPath}</code>
        </>
      )}
    </span>
  )
}

/**
 * The exact JSON to paste, and a button that copies it.
 *
 * The button reports what happened rather than what it attempted. There is no
 * `navigator.clipboard` in an insecure context and the write can be refused by
 * permission, and a page whose whole argument is that it never states what it
 * did not observe cannot say "copied" on the strength of having asked.
 */
export function PasteBlock({ label, json }: { label: string; json: unknown }) {
  const [copied, setCopied] = useState<boolean | undefined>(undefined)
  const text = JSON.stringify(json, null, 2)
  return (
    <div>
      <Json value={json} label={label} />
      <button
        type="button"
        onClick={() => {
          const written = navigator.clipboard?.writeText(text)
          if (!written) {
            setCopied(false)
            return
          }
          written.then(
            () => setCopied(true),
            () => setCopied(false)
          )
        }}
      >
        <IconCopy /> Copy
      </button>{' '}
      {copied === true && <span className="quiet">copied</span>}
      {copied === false && (
        <span className="quiet">this browser did not allow the copy — the JSON is above</span>
      )}
    </div>
  )
}
