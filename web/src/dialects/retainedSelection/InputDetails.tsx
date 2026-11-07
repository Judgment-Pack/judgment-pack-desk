import {useState} from 'react'
import {msg, useLocale} from '../../i18n'
import {Button} from '../../ui/Button'
import {Disclosure} from '../../ui/Disclosure'
import {Field} from '../../ui/Field'
import {Select} from '../../ui/Select'
import type {InputCursor, InputSpan, RetainedInputView} from './input'
import styles from '../DialectsView.module.css'

// A canonical boundary belongs to both adjacent windows. Moving it past a
// literal pair therefore neither duplicates nor loses either original unit.
export function textWindow(text: string, page: number, start = 0, end = text.length) {
 const boundary = (n: number) => {
  let at = Math.min(end, start + n * 4095)
  if (at > start && at < end && text.charCodeAt(at - 1) >= 0xd800 && text.charCodeAt(at - 1) <= 0xdbff && text.charCodeAt(at) >= 0xdc00 && text.charCodeAt(at) <= 0xdfff) at++
  return at
 }
 const from = boundary(page), to = boundary(page + 1)
 return {text: text.slice(from, to), from: from - start, to: to - start, total: end - start, next: to < end}
}
export function shortText(text: string) {
 let end = Math.min(120, text.length)
 if (end < text.length && text.charCodeAt(end - 1) >= 0xd800 && text.charCodeAt(end - 1) <= 0xdbff && text.charCodeAt(end) >= 0xdc00 && text.charCodeAt(end) <= 0xdfff) end--
 return text.slice(0, end) + (end < text.length ? '…' : '')
}
/** Sequential exact UTF-16 windows, including for long identities and keys. */
export function ExactText({text, start = 0, end = text.length}: {text: string; start?: number; end?: number}) {
 useLocale()
 const [page, setPage] = useState(0), window = textWindow(text, page, start, end)
 return <div className={styles.stack}>
  <pre className={styles.reviewNote} aria-label={msg('Exact text')}>{window.text}</pre>
  <p className={styles.note}>{msg('UTF-16 units {{start}}–{{end}} of {{total}}', {start: window.from, end: window.to, total: window.total})}</p>
  <div className={styles.toolbar}><Button disabled={page === 0} onClick={() => setPage(page - 1)}>{msg('Previous page')}</Button><Button disabled={!window.next} onClick={() => setPage(page + 1)}>{msg('Next page')}</Button></div>
 </div>
}
type Location = {span: InputSpan; label: string; parent?: Location}
function Explorer({reader, initial}: {reader: RetainedInputView; initial: InputSpan}) {
 useLocale()
 const [location, setLocation] = useState<Location>({span: initial, label: ''})
 const [cursor, setCursor] = useState<InputCursor>(), [offset, setOffset] = useState(0)
 const [full, setFull] = useState<InputSpan>()
 const span = location.span
 const kindLabel = (token: InputSpan) => token.kind === 'object' ? msg('Object') : token.kind === 'array' ? msg('Array') : token.kind === 'string' ? msg('Text') : token.kind === 'number' ? msg('Number') : token.kind === 'boolean' ? msg('Boolean') : msg('Null')
 const open = (next: Location) => {setLocation(next); setCursor(undefined); setOffset(0); setFull(undefined)}
 const window = span.kind === 'object' ? reader.members(span, 50, cursor) : span.kind === 'array' ? reader.elements(span, 50, cursor) : undefined
 const preview = (token: InputSpan) => {
  const text = reader.sourceText(token.source)
  let end = Math.min(token.end, token.start + 120)
  if (end < token.end && text.charCodeAt(end - 1) >= 0xd800 && text.charCodeAt(end - 1) <= 0xdbff && text.charCodeAt(end) >= 0xdc00 && text.charCodeAt(end) <= 0xdfff) end--
  return text.slice(token.start, end) + (end < token.end ? '…' : '')
 }
 return <div className={styles.stack}>
  <nav className={styles.toolbar} aria-label={msg('Input location')}>
   <Button onClick={() => open({span: initial, label: ''})}>{msg('Root')}</Button>
   <Button disabled={!location.parent} onClick={() => {if (location.parent) open(location.parent)}}>{msg('Parent')}</Button>
   <span>{shortText(location.label)}</span>
  </nav>
  {location.label.length > 120 && <Disclosure title={msg('Full name')}><ExactText key={location.label} text={location.label}/></Disclosure>}
  {window ? <>
   {window.entries.length === 0 && <pre className={styles.reviewNote}>{span.kind === 'object' ? '{}' : '[]'}</pre>}
   <div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>{msg('Name')}</th><th>{msg('Type')}</th><th>{msg('Value')}</th><th>{msg('Details')}</th></tr></thead><tbody>{window.entries.map((entry, index) => {
    const member = 'value' in entry ? entry : undefined, value = member ? member.value : entry as InputSpan
    const label = member ? member.name : String(offset + index)
    return <tr key={index}><td>{shortText(label)}{member && label.length > 120 && <Button onClick={() => setFull(member.key)}>{msg('Full name')}</Button>}</td>
     <td>{kindLabel(value)}</td><td><code>{preview(value)}</code></td><td><Button onClick={() => {
      if (value.kind === 'object' || value.kind === 'array') open({span: value, label, parent: location})
      else setFull(value)
     }}>{value.kind === 'object' || value.kind === 'array' ? msg('Open') : msg('View full value')}</Button>
     {(value.kind === 'object' || value.kind === 'array') && <Button onClick={() => setFull(value)}>{msg('View full value')}</Button>}</td></tr>
   })}</tbody></table></div>
   <div className={styles.toolbar}><Button disabled={!cursor} onClick={() => {setCursor(undefined); setOffset(0); setFull(undefined)}}>{msg('First page')}</Button><Button disabled={!window.next} onClick={() => {if (window.next) {setCursor(window.next); setOffset(offset + window.entries.length); setFull(undefined)}}}>{msg('Next page')}</Button></div>
  </> : <ExactText key={`${span.source}:${span.start}:${span.end}`} text={reader.sourceText(span.source)} start={span.start} end={span.end}/>}
  {full && <Disclosure open title={msg('Exact text')}><ExactText key={`${full.source}:${full.start}:${full.end}`} text={reader.sourceText(full.source)} start={full.start} end={full.end}/></Disclosure>}
 </div>
}
export function InputDetails({reader}: {reader: RetainedInputView}) {
 useLocale()
 const sections = [
  {id: 'facts', label: msg('Facts'), span: reader.sections.facts},
  {id: 'evidence', label: msg('Evidence availability'), span: reader.sections.evidence.kind === 'present' ? reader.sections.evidence.span : undefined},
  {id: 'originalValues', label: msg('Original values'), span: reader.sections.originalValues},
  {id: 'mapping', label: msg('Conversion rules'), span: reader.sections.mapping},
  {id: 'lineage', label: msg('Input lineage'), span: reader.sections.lineage},
  {id: 'cites', label: msg('Citations'), span: reader.sections.cites},
  {id: 'outcomes', label: msg('Outcomes'), span: reader.sections.outcomes},
  {id: 'source', label: msg('Source'), span: reader.sections.source},
  {id: 'preparation', label: msg('Preparation'), span: reader.sections.preparation},
  {id: 'custody', label: msg('Custody'), span: reader.sections.custody},
  {id: 'frame', label: msg('Frame'), span: reader.sections.frame},
  {id: 'root', label: msg('Input'), span: reader.sections.root},
  {id: 'origin', label: msg('Origin'), span: reader.sections.origin},
  {id: 'response', label: msg('Response'), span: reader.sections.response},
  {id: 'carrier', label: msg('Carrier'), span: reader.sections.carrier},
  {id: 'snapshot', label: msg('Snapshot'), span: reader.sections.snapshot},
  {id: 'record', label: msg('Record'), span: reader.sections.record},
  {id: 'research', label: msg('Research'), span: reader.sections.research},
  {id: 'declaration', label: msg('Declaration'), span: reader.sections.declaration},
  {id: 'history', label: msg('History'), span: reader.sections.history},
 ]
 const [section, setSection] = useState('facts'), [source, setSource] = useState(reader.sources[0]!.id)
 const [technical, setTechnical] = useState(false)
 const active = sections.find(item => item.id === section)!
 const sourceLabel = (id: string) => id === 'input' ? msg('Exact input JSON') : id === 'case' ? msg('Decoded asserted case JSON') : id === 'response' ? msg('Decoded response JSON') : msg('Decoded snapshot JSON')
 return <div className={styles.stack}>
  <Field label={msg('Section')}>{wiring => <Select {...wiring} value={section} options={sections.filter(item => item.span || item.id === 'evidence').map(item => ({value: item.id, label: item.label}))} onValueChange={setSection}/>}</Field>
  <Disclosure open title={active.label} key={section}>{active.span ? <Explorer reader={reader} initial={active.span}/> : <p>{msg('Not supplied')}</p>}</Disclosure>
  <Disclosure title={msg('Technical details')} onToggle={event => setTechnical(event.currentTarget.open)}>{technical && <div className={styles.stack}>
   <Field label={msg('Source')}>{wiring => <Select {...wiring} value={source} options={reader.sources.map(item => ({value: item.id, label: sourceLabel(item.id)}))} onValueChange={setSource}/>}</Field>
   <ExactText key={source} text={reader.sourceText(source)}/>
  </div>}</Disclosure>
 </div>
}
