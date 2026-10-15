import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MappedCaseFields } from './MappedCaseFields'
import { languageReady, setLanguage } from '../i18n'
import type { PackDocument } from '../mcp/types'
afterEach(async()=>{cleanup();setLanguage('en');await languageReady()})
it('translates a canonical rule error when displaying case input',async()=>{
 setLanguage('fr');await languageReady()
 render(<MappedCaseFields doc={{} as PackDocument} mapping={{version:2}} text={'{"count":1.0}'} disabled={false} onChange={vi.fn()} onValid={vi.fn()}/>)
 expect(screen.getByRole('alert').textContent).toBe('Écrivez des entiers sans fractions ni exposants (§1).')
})
