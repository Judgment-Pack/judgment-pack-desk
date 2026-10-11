import { Menu as DropdownMenu } from '../ui/Menu'
import { useState } from 'react'
import { LANGUAGES, type LanguagePreference } from './locales'
import { languageLoadFailed, languagePreference, languageReady, msg, setLanguage, useLocale } from './index'

export function LanguageMenu() {
  useLocale()
  const [persisted, setPersisted] = useState(true)
  const [loading, setLoading] = useState(false)
  return <DropdownMenu.Sub>
    <DropdownMenu.SubTrigger>{msg('Language')}</DropdownMenu.SubTrigger>
    <DropdownMenu.Portal><DropdownMenu.SubContent sideOffset={6} collisionPadding={12}>
      <DropdownMenu.RadioGroup aria-label={msg('Language')} aria-busy={loading} value={languagePreference()} onValueChange={async value => {
        setLoading(true)
        setPersisted(setLanguage(value as LanguagePreference))
        await languageReady()
        setLoading(false)
      }}>
        {[{ id: 'system', label: msg('Use system language') }, ...LANGUAGES].map(choice =>
          <DropdownMenu.RadioItem key={choice.id} value={choice.id} >
            <span lang={choice.id === 'system' ? undefined : choice.id}>{choice.label}</span>
          </DropdownMenu.RadioItem>)}
      </DropdownMenu.RadioGroup>
      {!persisted && <DropdownMenu.Label role="status">{msg('Language changed for this visit. This browser could not save your preference.')}</DropdownMenu.Label>}
      {languageLoadFailed() && <DropdownMenu.Label role="status">{msg('The language could not be loaded. Using English for now. Select the language again to retry.')}</DropdownMenu.Label>}
    </DropdownMenu.SubContent></DropdownMenu.Portal>
  </DropdownMenu.Sub>
}
