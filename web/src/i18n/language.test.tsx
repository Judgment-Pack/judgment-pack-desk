import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { initializeLanguage, i18n, LANGUAGE_KEY, language, languagePreference, languageReady, formatDate, formatNumber, msg, setLanguage, systemMessage, useLocale } from './index'
import { Message } from './Message'
import { currentLanguage } from './locales'
import { sourceMessage } from './source'
import { CodeBlock } from '../ui/CodeBlock'

afterEach(async () => { cleanup(); vi.restoreAllMocks(); localStorage.clear(); setLanguage('en'); await languageReady(); localStorage.clear() })

describe('personal language preference', () => {
  it('uses browser preferences, persists only an explicit choice, and observes system changes', async () => {
    const langs = vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['fr-CA', 'en'])
    const release = initializeLanguage(); await languageReady()
    expect(language()).toBe('fr')
    expect(localStorage.getItem(LANGUAGE_KEY)).toBeNull()
    expect(document.documentElement.lang).toBe('fr')
    expect(msg('Save')).toBe('Enregistrer')
    setLanguage('ja'); await languageReady()
    expect(currentLanguage()).toBe('ja')
    expect(localStorage.getItem(LANGUAGE_KEY)).toBe('ja')
    langs.mockReturnValue(['de-DE'])
    window.dispatchEvent(new Event('languagechange')); await languageReady()
    expect(language()).toBe('ja')
    setLanguage('system'); await languageReady()
    expect(language()).toBe('de')
    expect(localStorage.getItem(LANGUAGE_KEY)).toBeNull()
    langs.mockReturnValue(['ko-KR'])
    window.dispatchEvent(new Event('languagechange')); await languageReady()
    expect(language()).toBe('ko')
    release()
  })
  it('ignores unsupported or corrupt stored choices', async () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['xx', 'es-ES'])
    localStorage.setItem(LANGUAGE_KEY, '{broken')
    const release = initializeLanguage(); await languageReady()
    expect(language()).toBe('es')
    expect(languagePreference()).toBe('system')
    release()
  })
  it('applies the language for this visit when storage is blocked', async () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    expect(setLanguage('fr')).toBe(false); await languageReady()
    expect(language()).toBe('fr')
  })
  it('switches labels without remounting or translating the user’s draft', async () => {
    function Editor() {
      useLocale()
      const [value, setValue] = useState('Save <script>alert(1)</script>')
      return <><label>{msg('Description')}<input value={value} onChange={event => setValue(event.target.value)} /></label><button>{msg('Save')}</button></>
    }
    render(<Editor />)
    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'My unsent message' } })
    await act(async () => { setLanguage('fr'); await languageReady() })
    expect(screen.getByRole('button', { name: 'Enregistrer' })).toBeTruthy()
    expect(screen.getByRole('textbox')).toBe(input)
    expect((input as HTMLInputElement).value).toBe('My unsent message')
  })
  it('reorders inline content without translating or interpreting it as markup', async () => {
    i18n.addResource('fr', 'translation', 'Review <0/> with <1/>.', 'Avec <1/>, révisez <0/>.')
    setLanguage('fr'); await languageReady()
    render(<p><Message text="Review <0/> with <1/>." slots={[<code>{'Save<script>secret</script>'}</code>, <a href="/packs">Alice</a>]} /></p>)
    expect(screen.getByRole('link').getAttribute('href')).toBe('/packs')
    expect(document.querySelector('p')?.textContent).toBe('Avec Alice, révisez Save<script>secret</script>.')
    expect(document.querySelector('script')).toBeNull()
  })
  it('updates an open window when another window changes the preference', async () => {
    const release = initializeLanguage(); await languageReady()
    localStorage.setItem(LANGUAGE_KEY, 'pt-BR')
    window.dispatchEvent(new StorageEvent('storage', { key: LANGUAGE_KEY, newValue: 'pt-BR' })); await languageReady()
    expect(language()).toBe('pt-BR')
    release()
  })
  it('retains regional date and number conventions in system mode', async () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-GB'])
    const release = initializeLanguage(); await languageReady()
    const date = new Date(2026, 8, 17)
    expect(formatDate(date)).toBe(new Intl.DateTimeFormat('en-GB').format(date))
    setLanguage('de'); await languageReady()
    expect(formatNumber(12345.6)).toBe(new Intl.NumberFormat('de').format(12345.6))
    release()
  })
  it('uses locale plural rules rather than English word fragments', async () => {
    setLanguage('en'); await languageReady()
    expect(msg('Work · {{count}} steps', { count: 1 })).toBe('Work · 1 step')
    expect(msg('Work · {{count}} steps', { count: 2 })).toBe('Work · 2 steps')
    setLanguage('fr'); await languageReady()
    expect(msg('Work · {{count}} steps', { count: 0 })).toBe('Travail · 0 étape')
    expect(msg('Work · {{count}} steps', { count: 2 })).toBe('Travail · 2 étapes')
  })
  it('keeps the latest language choice when two catalogues are requested together', async () => {
    setLanguage('it')
    const previous = languageReady()
    setLanguage('es')
    await Promise.all([previous, languageReady()])
    expect(language()).toBe('es')
    expect(document.documentElement.lang).toBe('es')
  })

  it('localizes saved Desk notices at display time while keeping their original values', async () => {
    const file = 'Save <script>source</script>.txt'
    const source = sourceMessage('{{value0}} is not a text file.', { value0: file })
    setLanguage('fr'); await languageReady()
    expect(source).toBe(`${file} is not a text file.`)
    expect(systemMessage(source)).toBe(`${file} n’est pas un fichier texte.`)
    setLanguage('de'); await languageReady()
    expect(systemMessage(source)).toBe(`${file} ist keine Textdatei.`)
    expect(sourceMessage('{{value0}} is not a text file.', { value0: file })).toBe(source)
    expect(systemMessage('Provider diagnostic: Save')).toBe('Provider diagnostic: Save')
  })

  it('updates existing copy feedback without changing the copied source', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', Object.assign(Object.create(navigator), { clipboard: { writeText } }))
    render(<CodeBlock text={'{"outcomeId":"Save"}'} />)
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Copy JSON' })) })
    expect(screen.getByRole('status').textContent).toBe('Copied')
    await act(async () => { setLanguage('fr'); await languageReady() })
    expect(screen.getByRole('status').textContent).toBe('Copié')
    await act(async () => { setLanguage('ja'); await languageReady() })
    expect(screen.getByRole('status').textContent).toBe(msg('Copied'))
    expect(writeText).toHaveBeenCalledExactlyOnceWith('{"outcomeId":"Save"}')
    expect(document.querySelector('pre code')?.textContent).toBe('{"outcomeId":"Save"}')
    vi.unstubAllGlobals()
  })

})
