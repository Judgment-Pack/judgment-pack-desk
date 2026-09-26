import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {afterEach,expect,it} from 'vitest'
import {OpenQuestions,openQuestions} from './OpenQuestions'
afterEach(cleanup)
it('labels uncertainties and preserves separate items instead of collapsing bullets into prose', () => {
 const text='• Whether test evidence is signed.\nContinue this question.\n• Whether maintainers allow exceptions.'
 const view=render(<OpenQuestions text={text}/>)
 const summary=view.container.querySelector('summary')!
 expect(summary.textContent).toBe('Assumptions and open questions · 2')
 expect(view.container.querySelector('details')?.open).toBe(false)
 fireEvent.click(summary)
 expect(screen.getAllByRole('listitem')).toHaveLength(2)
 expect(screen.getAllByRole('listitem')[0]?.textContent).toContain('Continue this question.')
 expect(screen.getByText('Points the assistant could not confirm from the supplied information.')).toBeTruthy()
 expect(openQuestions('One question without an old bullet marker.')).toEqual(['One question without an old bullet marker.'])
})
