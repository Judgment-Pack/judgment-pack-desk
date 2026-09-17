import { DropdownMenu } from 'radix-ui'
import { useState } from 'react'
import { LANGUAGES, type LanguagePreference } from './locales'
import { languageLoadFailed, languagePreference, languageReady, msg, setLanguage, useLocale } from './index'
import { IconCheck, IconChevronRight } from '../shell/icons'

export function LanguageMenu() {
  useLocale()
  const [persisted, setPersisted] = useState(true)
  const [loading, setLoading] = useState(false)
  return <DropdownMenu.Sub>
    <DropdownMenu.SubTrigger className="desk-menu-item desk-language-trigger">{msg('Language')}<IconChevronRight /></DropdownMenu.SubTrigger>
    <DropdownMenu.Portal><DropdownMenu.SubContent className="desk-menu" sideOffset={6} collisionPadding={12}>
      <DropdownMenu.RadioGroup aria-label={msg('Language')} aria-busy={loading} value={languagePreference()} onValueChange={async value => {
        setLoading(true)
        setPersisted(setLanguage(value as LanguagePreference))
        await languageReady()
        setLoading(false)
      }}>
        {[{ id: 'system', label: msg('Use system language') }, ...LANGUAGES].map(choice =>
          <DropdownMenu.RadioItem key={choice.id} value={choice.id} className="desk-menu-item desk-menu-choice">
            <DropdownMenu.ItemIndicator className="desk-menu-tick"><IconCheck /></DropdownMenu.ItemIndicator><span lang={choice.id === 'system' ? undefined : choice.id}>{choice.label}</span>
          </DropdownMenu.RadioItem>)}
      </DropdownMenu.RadioGroup>
      {!persisted && <p role="status" className="desk-menu-note">{msg('Language changed for this visit. This browser could not save your preference.')}</p>}
      {languageLoadFailed() && <p role="status" className="desk-menu-note">{msg('The language could not be loaded. Using English for now. Select the language again to retry.')}</p>}
    </DropdownMenu.SubContent></DropdownMenu.Portal>
  </DropdownMenu.Sub>
}
