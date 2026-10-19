import { useEffect, useId, useState, type ComponentPropsWithRef } from 'react'
import { msg, useLocale } from '../i18n'
import { IconPreview, IconHide } from '../shell/icons'
import { Button } from './Button'
import { Input } from './Input'
import { Tooltip } from './Tooltip'
import styles from './PasswordInput.module.css'

type PasswordInputProps = Omit<ComponentPropsWithRef<'input'>, 'type'> & {
  showLabel?: string
  hideLabel?: string
}

/** Visibility belongs to this field only; nothing is persisted or submitted. */
export function PasswordInput({ id, className, disabled, showLabel, hideLabel, ...rest }: PasswordInputProps) {
  useLocale()
  const generatedId = useId()
  const [visible, setVisible] = useState(false)
  useEffect(() => { if (disabled) setVisible(false) }, [disabled])
  const revealed = visible && !disabled
  const inputId = id ?? generatedId
  const label = revealed ? hideLabel ?? msg('Hide password') : showLabel ?? msg('Show password')
  return <div className={styles.field}>
    <Input {...rest} id={inputId} disabled={disabled} type={revealed ? 'text' : 'password'} spellCheck={false} autoCapitalize="none" autoCorrect="off" className={[styles.input, className].filter(Boolean).join(' ')} />
    <Tooltip content={label}>
      <Button variant="quiet" size="icon" className={styles.toggle} disabled={disabled} aria-label={label} aria-controls={inputId}
        onMouseDown={event => event.preventDefault()} onClick={() => setVisible(value => !value)}>
        {revealed ? <IconHide /> : <IconPreview />}
      </Button>
    </Tooltip>
  </div>
}
