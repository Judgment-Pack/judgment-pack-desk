import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { GraphSources } from './GraphSources'
vi.mock('../mcp/queries', () => ({usePack: () => ({data:{document:{title:'Eligibility',sources:[{id:'policy',title:'Policy',publisher:'Team',citation:{excerpt:'The declared policy.'}}]}}})}))
afterEach(cleanup)
it('groups repeated pack references, identifies their steps, and keeps details out of chat', () => {
  render(<MemoryRouter><GraphSources document={{id:'graph',nodes:{first:{pack:'eligibility'},second:{pack:'eligibility'}}}}/></MemoryRouter>)
  expect(screen.getAllByRole('heading',{name:'Eligibility'})).toHaveLength(1)
  expect(screen.getByText('Used by: first, second')).toBeTruthy()
  fireEvent.click(screen.getByText('Policy',{selector:'summary span'}))
  expect(screen.getByText('The declared policy.')).toBeTruthy()
  expect(screen.getByRole('link',{name:'Open pack sources'}).getAttribute('href')).toBe('/packs/eligibility?view=evidence')
})
