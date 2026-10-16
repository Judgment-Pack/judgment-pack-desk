import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MappedCaseFields } from './MappedCaseFields'
import { languageReady, setLanguage } from '../i18n'
import type { PackDocument } from '../mcp/types'
afterEach(async()=>{cleanup();setLanguage('en');await languageReady()})
it('permits exact general JSON case input without rounding it through the form',async()=>{
 setLanguage('fr');await languageReady()
 render(<MappedCaseFields doc={{} as PackDocument} mapping={{version:2}} text={'{"count":1.0}'} disabled={false} onChange={vi.fn()} onValid={vi.fn()}/>)
 expect(screen.queryByRole('alert')).toBeNull();expect(screen.getByText(/JSON/)).toBeTruthy()
})
