import { fireEvent, within } from '@testing-library/react'
import { beforeEach, afterEach, vi } from 'vitest'
/** Drive the actual dialog in existing editor lifecycle tests. Dedicated guard
 * tests cover manual typing, cancellation, focus, and save failures separately.
 * A legacy confirm mock specifies the user's choice, never production behavior. */
const originalConfirm = window.confirm
export function respondToDiscardDialogs() {
 let observer: MutationObserver | undefined
 beforeEach(()=>{
  const answered=new WeakSet<Element>()
  observer=new MutationObserver(()=>{
   for(const dialog of document.querySelectorAll('[role="dialog"]')) {
    const field=dialog.querySelector<HTMLInputElement>('input[autocomplete="off"]')
    if(!field||answered.has(dialog))continue
    const label=field.id?dialog.querySelector(`label[for="${CSS.escape(field.id)}"]`):null
    const match=label?.textContent?.match(/^Type (.*) to confirm\.$/)
    if(!match)continue
    const title=dialog.querySelector('[id="'+dialog.getAttribute('aria-labelledby')+'"]')?.textContent
    const question=dialog.querySelector('[id="'+dialog.getAttribute('aria-describedby')+'"]')?.textContent??''
    answered.add(dialog)
    // Other styled confirmations retain their explicit action in the test.
    const leave=title==='Unsaved changes'&&(vi.isMockFunction(window.confirm)||window.confirm!==originalConfirm)?window.confirm(question):true
    if(!leave) {fireEvent.click(within(dialog as HTMLElement).getByRole('button',{name:'Keep editing'}));continue}
    fireEvent.change(field,{target:{value:match[1]}})
    if(title==='Unsaved changes')fireEvent.click(within(dialog as HTMLElement).getByRole('button',{name:'Discard changes'}))
   }
  })
  observer.observe(document.body,{childList:true,subtree:true,attributes:true})
 })
 afterEach(()=>observer?.disconnect())
}
