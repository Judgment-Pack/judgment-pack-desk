import type {ReactNode} from 'react'
import {msg} from '../i18n'
import {RecipeSection} from './RecipeSection'
import {DialectPackEditor} from './DialectPackEditor'
import type {DialectPackBinding,DialectRecord} from './client'
import type {ResearchRecipe} from './research'

export function RecipePackSection({value,records,research,disabled,onChange,onPendingChange,children}:{value?:DialectPackBinding;records:DialectRecord[];research?:ResearchRecipe;disabled:boolean;onChange:(value?:DialectPackBinding)=>void;onPendingChange:(pending:boolean)=>void;children?:ReactNode}){
 return <RecipeSection title={msg('Pack mapping')}>
  <DialectPackEditor value={value} records={records} research={research} disabled={disabled} onChange={onChange} onPendingChange={onPendingChange}/>
  {children}
 </RecipeSection>
}
