import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { FileBrowser } from './FileBrowser'
afterEach(cleanup)
const files=['jpack.json','packs/same.json','archive/same.json','.desk/job-drafts/uuid.json'].map(path=>({path,bytes:2,sha256:''}))
const props={files,dirty:true,selected:'packs/same.json',expanded:['packs'],search:'',scroll:0,onSearch:vi.fn(),onExpand:vi.fn(),onChoose:vi.fn(),onScroll:vi.fn()}
it('keeps generated folders collapsed, shows single-line filenames, and marks unsaved selection',()=>{
 render(<FileBrowser {...props}/>)
 expect(screen.getByRole('button',{name:'.desk'}).getAttribute('aria-expanded')).toBe('false')
 expect(screen.queryByRole('button',{name:'.desk/job-drafts/uuid.json'})).toBeNull()
 expect(screen.getByRole('button',{name:'packs/same.json'}).getAttribute('aria-current')).toBe('true')
 expect(screen.getByRole('img',{name:'unsaved changes'})).toBeTruthy()
 expect(screen.queryByText('packs/same.json')).toBeNull()
})
it('searches full paths across collapsed folders and shows parent paths for duplicate names',()=>{
 const view=render(<FileBrowser {...props} search="same"/>)
 expect(screen.getByRole('button',{name:'packs/same.json'})).toBeTruthy()
 expect(screen.getByRole('button',{name:'archive/same.json'})).toBeTruthy()
 expect(screen.getByRole('status').textContent).toBe('2 of 4 files')
 view.rerender(<FileBrowser {...props} search="job-drafts"/>)
 expect(screen.getByRole('button',{name:'.desk/job-drafts/uuid.json'})).toBeTruthy()
 view.rerender(<FileBrowser {...props} search="nothing matches"/>)
 expect(screen.getByText('No matching files.')).toBeTruthy()
})
it('moves focus without opening or discarding edits, and can focus a parent folder',()=>{
 const choose=vi.fn();render(<FileBrowser {...props} onChoose={choose}/>)
 const file=screen.getByRole('button',{name:'packs/same.json'})
 file.focus();fireEvent.keyDown(file,{key:'ArrowLeft'})
 expect(document.activeElement).toBe(screen.getByRole('button',{name:'packs'}))
 expect(choose).not.toHaveBeenCalled()
 fireEvent.click(file);expect(choose).toHaveBeenCalledWith('packs/same.json')
})
