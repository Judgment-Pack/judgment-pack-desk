import {cleanup,fireEvent,render,screen,within} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {CodeDiff} from './CodeDiff'
vi.mock('./DiffEditor',()=>({default:(props:{before:string;after:string;language:string;ariaLabel:string;beforeLabel:string;afterLabel:string;split:boolean})=><div role="region" aria-label={props.ariaLabel} data-language={props.language} data-split={props.split}><span>{props.beforeLabel}</span><span>{props.afterLabel}</span><pre data-before>{props.before}</pre><pre>{props.after}</pre></div>}))
afterEach(cleanup)
it('retains JSON proposal labels, dialog and unified inline defaults',async()=>{
 render(<CodeDiff before='{"a":1}' after='{"a":2}'/>);
 const region=await screen.findByRole('region',{name:'Proposed changes'})
 expect(region.getAttribute('data-language')).toBe('json');expect(region.getAttribute('data-split')).toBe('false')
 expect(within(region).getByText('Before')).toBeTruthy();expect(within(region).getByText('Proposed')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Expand diff'}))
 const dialog=await screen.findByRole('dialog',{name:'Review proposed changes'})
 expect(within(dialog).getByRole('region',{name:'Proposed changes'}).getAttribute('data-split')).toBe('true')
})
it('uses caller text, labels, title and accessible region for saved source comparisons',async()=>{
 const before='Café 中文\r\n',after='Café 中文\n'
 render(<CodeDiff before={before} after={after} language="text" beforeLabel="Retained source" afterLabel="Saved Policy" title="Exact comparison" ariaLabel="Policy text changes"/>);
 const region=await screen.findByRole('region',{name:'Policy text changes'})
 expect(region.getAttribute('data-language')).toBe('text');expect(region.querySelector('pre')?.textContent).toBe(before)
 expect(screen.queryByText('Proposed')).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:'Expand diff'}));expect(await screen.findByRole('dialog',{name:'Exact comparison'})).toBeTruthy()
})
it('retains a changed suffix beyond 128 KiB in inline and expanded proposal review',async()=>{
 const prefix='x'.repeat(128*1024),before=prefix+'old suffix',after=prefix+'changed suffix'
 render(<CodeDiff before={before} after={after}/>);
 const region=await screen.findByRole('region',{name:'Proposed changes'})
 expect(region.querySelectorAll('pre')[0]?.textContent).toBe(before)
 expect(region.querySelectorAll('pre')[1]?.textContent).toBe(after)
 fireEvent.click(screen.getByRole('button',{name:'Expand diff'}))
 const dialog=await screen.findByRole('dialog',{name:'Review proposed changes'})
 expect(within(dialog).getByRole('region',{name:'Proposed changes'}).querySelectorAll('pre')[1]?.textContent).toBe(after)
})
