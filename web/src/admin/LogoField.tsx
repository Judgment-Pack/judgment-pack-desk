import { useEffect, useRef, useState } from 'react'
import { MAX_MARK_BYTES, ORGANIZATION_MARK_SAYS } from '../config/deskConfig'
import { msg, systemMessage, useLocale } from '../i18n'
import { BrandMark } from '../ui/BrandMark'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { Disclosure } from '../ui/Disclosure'
import { TextArea } from '../ui/TextArea'
import styles from './LogoField.module.css'

/** Upload stays in the settings draft until the existing verified Save succeeds. */
export function LogoField({ value, onChange, error, kind = 'logo', fallbackMark = '' }: {
  value: string; onChange: (value: string) => void; error?: string
  kind?: 'logo' | 'favicon'; fallbackMark?: string
}) {
  useLocale()
  const favicon = kind === 'favicon'
  const types = favicon ? ['image/svg+xml', 'image/png', 'image/x-icon', 'image/vnd.microsoft.icon'] : ['image/svg+xml', 'image/png', 'image/jpeg', 'image/webp']
  const input = useRef<HTMLInputElement>(null), reader = useRef<FileReader | null>(null)
  const change = useRef(onChange)
  change.current = onChange
  const [uploadError, setUploadError] = useState(''), [reading, setReading] = useState(false)
  useEffect(() => () => { reader.current?.abort() }, [])
  function upload(file?: File) {
    if (!file) return
    setUploadError('')
    if (!types.includes(file.type)) {
      setUploadError(favicon ? msg('Choose an SVG, PNG or ICO image.') : msg('Choose an SVG, PNG, JPEG or WebP image.')); return
    }
    const tooLarge = () => setUploadError(favicon ? msg('Choose a smaller favicon (up to 64 KB after encoding).') : msg('Choose a smaller logo (up to 64 KB after encoding).'))
    if (file.size > MAX_MARK_BYTES) { tooLarge(); return }
    const next = new FileReader()
    reader.current = next; setReading(true)
    next.onload = () => {
      setReading(false)
      const data = String(next.result)
      if (new TextEncoder().encode(data).length > MAX_MARK_BYTES) { tooLarge(); return }
      change.current(data)
    }
    next.onerror = () => { setReading(false); setUploadError(favicon ? msg('Could not read the favicon. Try another image.') : msg('Could not read the logo. Try another image.')) }
    next.readAsDataURL(file)
  }
  return <div className={styles.field}>
    <Field label={favicon ? msg('Favicon') : msg('Logo')} hint={favicon ? msg('Uses the logo automatically. Upload an SVG, PNG or ICO image to override it.') : msg('Upload an SVG, PNG, JPEG or WebP image. Leave blank to use the JPS logo.')} error={uploadError || error}>
      {wiring => <div className={styles.controls}>
        <span className={styles.previewFrame}><BrandMark mark={value.trim() ? value : fallbackMark} className={favicon ? styles.faviconPreview : styles.preview} /></span>
        <input {...wiring} ref={input} type="file" hidden accept={types.join(',')}
          onChange={event => { upload(event.target.files?.[0]); event.target.value = '' }} />
        <Button disabled={reading} onClick={() => input.current?.click()}>{reading ? msg('Loading…') : msg('Upload file')}</Button>
        <Button variant="quiet" disabled={reading || !value.trim()} onClick={() => { setUploadError(''); onChange('') }}>{favicon ? msg('Use logo') : msg('Reset to default')}</Button>
      </div>}
    </Field>
    <Disclosure title={msg('Advanced settings')}>
      <Field label={favicon ? msg('Favicon image data') : msg('Mark')} hint={systemMessage(ORGANIZATION_MARK_SAYS)}>
        {wiring => <TextArea {...wiring} value={value} rows={3} spellCheck={false} disabled={reading}
          onChange={event => { setUploadError(''); onChange(event.target.value) }} />}
      </Field>
    </Disclosure>
  </div>
}
