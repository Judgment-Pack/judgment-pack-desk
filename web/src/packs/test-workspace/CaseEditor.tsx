import { useEffect, useLayoutEffect, useState, useId, useRef } from 'react'
import { msg } from '../../i18n'
import { Field, FieldGroup } from '../../ui/Field'
import { Input } from '../../ui/Input'
import { TextArea } from '../../ui/TextArea'
import { Select } from '../../ui/Select'
import { Button } from '../../ui/Button'
import { Tooltip } from '../../ui/Tooltip'
import { Disclosure } from '../../ui/Disclosure'
import { CodeBlock } from '../../ui/CodeBlock'
import { IconClose } from '../../shell/icons'
import { SourceReader } from '../../documents/SourceReader'
import { useDetailsSlot } from '../../shell/DetailsSlot'
import { useReadingDetails } from '../../chat/ReadingDetails'
import { factFields, object, pointerGet, pointerSet, type TestCase } from './model'
import styles from './TestsWorkspace.module.css'

export function JsonInput({
  value,
  onChange,
  label,
  onValid,
}: {
  value: unknown
  onChange: (v: unknown) => void
  label: string
  onValid?: (ok: boolean) => void
}) {
  const [text, setText] = useState(JSON.stringify(value, null, 2) ?? '')
  const [error, setError] = useState('')
  const emitted = useRef(JSON.stringify(value))
  useEffect(() => {
    if (emitted.current !== JSON.stringify(value)) {
      setText(JSON.stringify(value, null, 2) ?? '')
      setError('')
      onValid?.(true)
      emitted.current = JSON.stringify(value)
    }
  }, [value])
  return (
    <FieldGroup>
      <Field label={label} error={error}>
        {(w) => (
          <TextArea
            {...w}
            value={text}
            rows={5}
            spellCheck={false}
            onChange={(e) => {
              const t = e.target.value
              setText(t)
              try {
                const v = JSON.parse(t)
                setError('')
                onValid?.(true)
                emitted.current = JSON.stringify(v)
                onChange(v)
              } catch {
                setError(msg('Enter valid JSON.'))
                onValid?.(false)
              }
            }}
          />
        )}
      </Field>
    </FieldGroup>
  )
}
function ScalarInput({
  value,
  type,
  label,
  onChange,
  onValid,
}: {
  value: string | number
  type: string
  label: string
  onChange: (v: unknown) => void
  onValid: (valid: boolean) => void
}) {
  const [text, setText] = useState(String(value)),
    emitted = useRef(value)
  useEffect(() => {
    if (value !== emitted.current) {
      emitted.current = value
      setText(String(value))
      onValid(true)
    }
  }, [value])
  return (
    <Input
      aria-label={label}
      inputMode={type === 'number' ? 'decimal' : undefined}
      value={text}
      onChange={(e) => {
        const next = e.target.value
        setText(next)
        if (type === 'number' && (!next.trim() || !Number.isFinite(Number(next)))) {
          onValid(false)
          return
        }
        const parsed = type === 'number' ? Number(next) : next
        emitted.current = parsed
        onValid(true)
        onChange(parsed)
      }}
    />
  )
}
function FactInput({
  value,
  type,
  onChange,
  label,
  onValid,
}: {
  value: unknown
  type: string
  onChange: (v: unknown) => void
  label: string
  onValid: (ok: boolean) => void
}) {
  const fieldId = useId()
  const kind = value === undefined ? 'unknown' : value === null ? 'null' : 'value'
  if (
    value !== undefined &&
    value !== null &&
    ['boolean', 'number', 'string'].includes(type) &&
    typeof value !== type
  )
    return <JsonInput label={label} value={value} onChange={onChange} onValid={onValid} />
  if (type === 'boolean')
    return (
      <Select
        id={fieldId}
        aria-label={label}
        value={value === undefined ? 'unknown' : value === null ? 'null' : String(value)}
        options={[
          { value: 'unknown', label: msg('Unknown') },
          { value: 'true', label: msg('Yes') },
          { value: 'false', label: msg('No') },
          { value: 'null', label: msg('Null') },
        ]}
        onValueChange={(v) => onChange(v === 'unknown' ? undefined : v === 'null' ? null : v === 'true')}
      />
    )
  return (
    <div className={styles.valueEditor}>
      <Select
        id={fieldId}
        aria-label={label + ' ' + msg('Value state')}
        value={kind}
        options={[
          { value: 'unknown', label: msg('Unknown') },
          { value: 'value', label: msg('Value') },
          { value: 'null', label: msg('Null') },
        ]}
        onValueChange={(v) =>
          onChange(
            v === 'unknown'
              ? undefined
              : v === 'null'
                ? null
                : type === 'number'
                  ? 0
                  : type === 'string'
                    ? ''
                    : type === 'array'
                      ? []
                      : {},
          )
        }
      />
      {kind === 'value' &&
        (type === 'string' || type === 'number' ? (
          <ScalarInput
            label={label}
            type={type}
            value={value as string | number}
            onChange={onChange}
            onValid={onValid}
          />
        ) : (
          <JsonInput label={label} value={value} onChange={onChange} onValid={onValid} />
        ))}
    </div>
  )
}
export function CaseEditor({
  owner,
  document,
  value,
  onChange,
  onSave,
  onDiscard,
  onAddSource,
  onAI,
  onRun,
  busy,
  error,
  dirty,
  proposal = false,
}: {
  owner: string
  document: unknown
  value: TestCase
  onChange: (c: TestCase) => void
  onSave: () => void
  onDiscard: () => void
  onAddSource: (el: HTMLElement) => void
  onAI: () => void
  onRun: () => void
  busy: boolean
  error: string
  dirty: boolean
  proposal?: boolean
}) {
  const fields = factFields(document).filter((f) => !f.path.split('/').some((k) => /^\d+$/.test(k)))
  const read = useReadingDetails(owner),
    details = useDetailsSlot()
  const live = useRef({ value, busy, onChange })
  live.current = { value, busy, onChange }
  const mounted = useRef(true)
  useEffect(()=>{mounted.current=true;return ()=>{mounted.current=false}},[])
  const body = useRef<HTMLDivElement>(null)
  const scrollPosition = useRef(0)
  useLayoutEffect(() => {
    if (details.open && body.current) body.current.scrollTop = scrollPosition.current
  }, [details.open])
  const [invalid, setInvalid] = useState<Record<string, boolean>>({})
  const valid = !Object.values(invalid).some(Boolean)
  const validity = (key: string) => (ok: boolean) =>
    setInvalid((prior) => (prior[key] === !ok ? prior : { ...prior, [key]: !ok }))
  const row = value.row
  const patch = (part: Partial<TestCase>) => onChange({ ...value, ...part })
  const patchRow = (part: Partial<TestCase['row']>) => patch({ row: { ...row, ...part } })
  const doc = object(document) ? document : {}
  const outcomes = Array.isArray(doc.outcomes) ? doc.outcomes.filter(object) : []
  const evidence = Array.isArray(doc.evidenceRequirements) ? doc.evidenceRequirements.filter(object) : []
  const expected = object(row.expectedDisposition) ? row.expectedDisposition : undefined
  const selected = row.expectedErrorClass
    ? 'refusal'
    : expected?.kind === 'outcome'
      ? 'outcome:' + String(expected.outcomeId)
      : expected?.kind === 'unresolved'
        ? 'unresolved'
        : expected?.kind === 'not-applicable'
          ? 'not-applicable'
          : expected
            ? 'advanced'
            : 'none'
  const expectationOptions = [
    { value: 'none', label: msg('Exploratory · no expectation') },
    ...outcomes.map((x) => ({ value: 'outcome:' + x.id, label: String(x.label ?? x.id) })),
    { value: 'unresolved', label: msg('Unresolved') },
    { value: 'not-applicable', label: msg('Not applicable') },
    { value: 'advanced', label: msg('Custom expectation') },
    { value: 'refusal', label: msg('Expected refusal') },
  ]
  if (selected.startsWith('outcome:') && !expectationOptions.some((o) => o.value === selected))
    expectationOptions.push({ value: selected, label: String(expected?.outcomeId) })
  return (
    <form
      className={styles.editor}
      noValidate
      onSubmit={(e) => {
        e.preventDefault()
        if (valid) onSave()
      }}
    >
      <div
        ref={body}
        className={styles.editorBody}
        onScroll={(event) => {
          if (details.open && event.currentTarget.getClientRects().length)
            scrollPosition.current = event.currentTarget.scrollTop
        }}
      >
        <fieldset disabled={busy} className={styles.editorFields}>
          <FieldGroup>
            <Field
              label={msg('Case name')}
              hint={
                <>
                  {value.origin === 'ai'
                    ? msg('AI proposal')
                    : value.origin === 'draft'
                      ? msg('From draft')
                      : value.origin === 'import'
                        ? msg('Imported')
                        : msg('Manual')}
                  {dirty ? ' · ' + msg('Unsaved changes') : ''}
                </>
              }
            >
              {(w) => (
                <Input {...w} value={value.name} required onChange={(e) => patch({ name: e.target.value })} />
              )}
            </Field>
          </FieldGroup>
          <section className={styles.editorSection}>
            <div className={styles.sectionTitle}>
              <h3>{msg('Inputs')}</h3>
              <Button variant="quiet" onClick={onAI} disabled={busy}>
                {msg('Fill with AI')}
              </Button>
            </div>
            {!fields.length && (
              <p className={styles.muted}>
                {msg('No input fields could be inferred. Use the facts JSON below.')}
              </p>
            )}
            <FieldGroup>
              {fields.map((field) => (
                <div key={field.path} className={styles.fact}>
                  <label>{field.label}</label>
                  <FactInput
                    onValid={validity(field.path)}
                    label={field.label}
                    type={field.type}
                    value={pointerGet(row.facts, field.path)}
                    onChange={(v) => patchRow({ facts: pointerSet(row.facts, field.path, v) })}
                  />
                  {value.sources.length > 0 && (
                    <Select
                      id={'source-' + field.path}
                      aria-label={field.label + ' ' + msg('Source')}
                      value={value.sourceMappings[field.path] ?? 'none'}
                      options={[
                        { value: 'none', label: msg('No source mapping') },
                        ...value.sources.map((s) => ({ value: s.id, label: s.name })),
                      ]}
                      onValueChange={(id) =>
                        patch({ sourceMappings: { ...value.sourceMappings, [field.path]: id } })
                      }
                    />
                  )}
                </div>
              ))}
            </FieldGroup>
          </section>
          {evidence.length > 0 && (
            <section className={styles.editorSection}>
              <header className={styles.sectionIntro}>
                <h3>{msg('Evidence availability')}</h3>
                <p className={styles.muted}>
                  {msg('Attaching a source does not mark an evidence requirement as satisfied.')}
                </p>
              </header>
              <FieldGroup>
                {evidence.map((e) => (
                  <Field key={String(e.id)} label={String(e.description ?? e.id)}>
                    {(w) => (
                      <Select
                        {...w}
                        value={String(
                          object(row.evidenceAvailability)
                            ? (row.evidenceAvailability[String(e.id)] ?? 'unknown')
                            : 'unknown',
                        )}
                        options={['unknown', 'present', 'absent'].map((v) => ({
                          value: v,
                          label:
                            v === 'present'
                              ? msg('Present')
                              : v === 'absent'
                                ? msg('Absent')
                                : msg('Unknown'),
                        }))}
                        onValueChange={(v) =>
                          patchRow({
                            evidenceAvailability: {
                              ...(object(row.evidenceAvailability) ? row.evidenceAvailability : {}),
                              [String(e.id)]: v,
                            },
                          })
                        }
                      />
                    )}
                  </Field>
                ))}
              </FieldGroup>
            </section>
          )}
          <section className={styles.editorSection}>
            <div className={styles.sectionTitle}>
              <h3>{msg('Sources')}</h3>
              <Button variant="quiet" disabled={busy} onClick={(e) => onAddSource(e.currentTarget)}>
                {msg('Add source')}
              </Button>
            </div>
            {value.sources.length > 0 && (
              <div className={styles.sources}>
                {value.sources.map((file) => (
                  <div key={file.id}>
                    <Button
                      variant="inline"
                      onClick={(e) =>
                        read(
                          <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
                            <Button
                              variant="inline"
                              className={styles.backToCase}
                              onClick={() => {
                                details.dismissInspection?.()
                                details.reveal()
                              }}
                            >
                              {msg('Back to case')}
                            </Button>
                            {file.document ? (
                              <SourceReader name={file.name} reference={file.document} link={file.link} onUse={next=>{
                                const current=live.current
                                if(!mounted.current||current.busy||current.value.id!==value.id||!current.value.sources.some(s=>s.id===file.id&&s.document?.digest===file.document?.digest))throw Error(msg('The case changed. Reopen its source before applying the refresh.'))
                                current.onChange({...current.value,sources:current.value.sources.map(s=>s.id===file.id?next:s),sourceMappings:Object.fromEntries(Object.entries(current.value.sourceMappings).map(([path,id])=>[path,id===file.id?next.id:id]))})
                                details.dismissInspection?.();details.reveal()
                              }}/>
                            ) : (
                              <CodeBlock text={file.text} label={file.name} />
                            )}
                          </div>,
                          e.currentTarget,
                        )
                      }
                    >
                      {file.name}
                    </Button>
                    <Tooltip content={msg('Remove source')}>
                      <Button
                        variant="quiet"
                        size="icon"
                        aria-label={msg('Remove source')}
                        onClick={() =>
                          patch({
                            sources: value.sources.filter((s) => s.id !== file.id),
                            sourceMappings: Object.fromEntries(
                              Object.entries(value.sourceMappings).filter(([, id]) => id !== file.id),
                            ),
                          })
                        }
                      >
                        <IconClose />
                      </Button>
                    </Tooltip>
                  </div>
                ))}
              </div>
            )}
            {!value.sources.length && (
              <p className={styles.muted}>{msg('Attach files or choose a connected source.')}</p>
            )}
          </section>
          <section className={styles.editorSection}>
            <FieldGroup>
              <Field label={msg('Expected result')}>
                {(w) => (
                  <Select
                    {...w}
                    value={selected}
                    options={expectationOptions}
                    onValueChange={(v) => {
                      const next = { ...row }
                      delete next.expectedDisposition
                      delete next.expectedErrorClass
                      delete next.expectedErrorPhase
                      delete next.expectedHandoffTarget
                      if (v.startsWith('outcome:'))
                        next.expectedDisposition = {
                          kind: 'outcome',
                          outcomeId: v.slice(8),
                          reasons: [],
                          handoff: { state: 'none' },
                        }
                      if (v === 'not-applicable')
                        next.expectedDisposition = {
                          kind: 'not-applicable',
                          reasons: ['not-applicable'],
                          handoff: { state: 'none' },
                        }
                      if (v === 'advanced' || v === 'unresolved')
                        next.expectedDisposition = {
                          kind: 'unresolved',
                          reasons: ['unknown'],
                          handoff: { state: 'none' },
                        }
                      if (v === 'refusal') next.expectedErrorClass = 'malformed-input'
                      setInvalid((prior) => ({ ...prior, expectation: false, triggers: false }))
                      patch({ row: next })
                    }}
                  />
                )}
              </Field>
              {selected === 'unresolved' && (
                <fieldset className={styles.reasonFields}>
                  <legend>{msg('Expected reasons')}</legend>
                  {[
                    ['unknown', msg('Unknown inputs')],
                    ['missing-required-evidence', msg('Missing required evidence')],
                    ['conflict', msg('Conflicting outcomes')],
                    ['no-match', msg('No matching rule')],
                    ['exception-escalation', msg('Special case requests a handoff')],
                  ].map(([id, label]) => (
                    <label key={id}>
                      <input
                        type="checkbox"
                        checked={Array.isArray(expected?.reasons) && expected.reasons.includes(id)}
                        onChange={(e) => {
                          const reasons = Array.isArray(expected?.reasons) ? expected.reasons : []
                          patchRow({
                            expectedDisposition: {
                              ...expected,
                              reasons: e.target.checked ? [...reasons, id] : reasons.filter((x) => x !== id),
                            },
                          })
                        }}
                      />
                      {label}
                    </label>
                  ))}
                </fieldset>
              )}
              {expected && selected !== 'advanced' && (
                <Field label={msg('Expected handoff')}>
                  {(w) => (
                    <Select
                      {...w}
                      value={object(expected.handoff) ? String(expected.handoff.state) : 'none'}
                      options={[
                        { value: 'none', label: msg('No handoff') },
                        { value: 'requested', label: msg('Handoff requested') },
                      ]}
                      onValueChange={(v) =>
                        patchRow({
                          expectedDisposition: {
                            ...expected,
                            handoff:
                              v === 'none'
                                ? { state: 'none' }
                                : {
                                    state: 'requested',
                                    triggeredBy: Array.isArray(expected.reasons) ? expected.reasons : [],
                                  },
                          },
                        })
                      }
                    />
                  )}
                </Field>
              )}
              {expected && object(expected.handoff) && expected.handoff.state === 'requested' && (
                <JsonInput
                  label={msg('Handoff triggers')}
                  value={expected.handoff.triggeredBy}
                  onValid={validity('triggers')}
                  onChange={(v) =>
                    patchRow({
                      expectedDisposition: { ...expected, handoff: { state: 'requested', triggeredBy: v } },
                    })
                  }
                />
              )}
              {selected === 'advanced' && (
                <JsonInput
                  label={msg('Expected disposition')}
                  value={row.expectedDisposition}
                  onChange={(v) => patchRow({ expectedDisposition: v })}
                  onValid={validity('expectation')}
                />
              )}
              {selected === 'refusal' && (
                <>
                  <Field label={msg('Expected error class')}>
                    {(w) => (
                      <Input
                        {...w}
                        value={row.expectedErrorClass ?? ''}
                        onChange={(e) => patchRow({ expectedErrorClass: e.target.value })}
                      />
                    )}
                  </Field>
                  <Field label={msg('Expected error phase (optional)')}>
                    {(w) => (
                      <Input
                        {...w}
                        value={row.expectedErrorPhase ?? ''}
                        onChange={(e) => patchRow({ expectedErrorPhase: e.target.value || undefined })}
                      />
                    )}
                  </Field>
                </>
              )}
              <Field label={msg('Why this result is expected')}>
                {(w) => (
                  <TextArea
                    {...w}
                    rows={3}
                    value={value.rationale}
                    onChange={(e) => patch({ rationale: e.target.value })}
                  />
                )}
              </Field>
            </FieldGroup>
          </section>
          <Disclosure title={msg('Technical details / JSON')}>
            <p className={styles.muted}>
              {msg('Exact input and expectation fields, including optional handoff target assertions.')}
            </p>
            <JsonInput
              label={msg('Case JSON')}
              value={row}
              onValid={validity('case-json')}
              onChange={(v) => {
                if (object(v) && Object.hasOwn(v, 'facts')) {
                  validity('case-json')(true)
                  patch({ row: { ...v, id: value.id } as TestCase['row'] })
                } else validity('case-json')(false)
              }}
            />
            <div>
              {fields.map((f) => (
                <p key={f.path}>
                  <code>{f.path}</code>
                </p>
              ))}
            </div>
          </Disclosure>
          {!valid && <p role="alert">{msg('Correct the invalid JSON field before saving or running.')}</p>}
          {error && (
            <p role="alert" className={styles.error}>
              {error}
            </p>
          )}
        </fieldset>
      </div>
      <footer className={styles.editorFooter}>
        <Button variant="quiet" onClick={onDiscard} disabled={busy}>
          {proposal ? msg('Back to proposed cases') : msg('Discard')}
        </Button>
        {!proposal && (
          <Button onClick={onRun} disabled={busy || dirty || !valid}>
            {msg('Run case')}
          </Button>
        )}
        <Button type="submit" variant="primary" disabled={busy || !value.name.trim() || !valid}>
          {proposal ? msg('Apply to proposal') : msg('Save case')}
        </Button>
      </footer>
    </form>
  )
}
