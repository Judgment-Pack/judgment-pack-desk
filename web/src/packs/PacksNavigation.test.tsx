import { cleanup, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it } from 'vitest'
import { PacksNavigation } from './PacksNavigation'

afterEach(cleanup)
it('retains Browse, count and folder control without the retired collection destinations', () => {
  render(<MemoryRouter><PacksNavigation count={7} leading={<button>Expand folders</button>} /></MemoryRouter>)
  const navigation = screen.getByRole('navigation', { name: 'Packs workspace' })
  const links = within(navigation).getAllByRole('link')
  expect(links).toHaveLength(1)
  expect(links[0].getAttribute('href')).toBe('/packs')
  expect(links[0].textContent).toContain('7')
  expect(within(navigation).getByRole('button', { name: 'Expand folders' })).toBeTruthy()
  expect(screen.queryByRole('link', { name: 'Tests' })).toBeNull()
  expect(screen.queryByRole('link', { name: /Graphs|Pack flows/ })).toBeNull()
})
