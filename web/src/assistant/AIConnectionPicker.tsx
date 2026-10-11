import { useId } from 'react'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { Select } from '../ui/Select'
import { msg } from '../i18n'
import { deskAIConnections } from './aiConnections'

/** The connection and model controls refer to the same execution slot. */
export function AIConnectionPicker({ value, onValueChange, disabled }: {
  value?: string; onValueChange: (id: string) => void; disabled: boolean
}) {
  const id = useId()
  const effective = useEffectiveConfig(), registry = effective.aiConnections?.data
  if (!registry) return null
  return <Select id={id} aria-label={msg('AI connection')} value={value} placeholder={msg('Choose AI connection')}
    disabled={disabled} onValueChange={onValueChange}
    options={deskAIConnections(registry, effective.assistantProfile).map(c => ({ value: c.id, label: c.name }))} />
}
