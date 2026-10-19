import {useId,type ReactNode,type RefObject} from 'react'
import {msg} from '../i18n'
import {Field,FieldGroup} from '../ui/Field'
import {Input} from '../ui/Input'
import {TextArea} from '../ui/TextArea'
import styles from './RecipeSection.module.css'

/** All controls edit the parent's recipe buffer. Only the page saves the recipe. */
export function RecipeSection({title,children,attention=false,sectionRef}:{title:string;children:ReactNode;attention?:boolean;sectionRef?:RefObject<HTMLElement|null>}){
 const id=useId()
 return <section ref={sectionRef} tabIndex={-1} className={styles.section} aria-labelledby={id}>
  <header className={styles.heading}><h3 id={id}>{title}</h3></header>
  {attention&&<p className={styles.attention}>{msg('Setup needed')}</p>}
  <div className={styles.body}>{children}</div>
 </section>
}

export function RecipePurpose({name,intent,onName,onIntent,disabled=false}:{name:string;intent:string;onName:(name:string)=>void;onIntent:(intent:string)=>void;disabled?:boolean}){
 return <RecipeSection title={msg('Purpose')}>
  <FieldGroup><Field label={msg('Name')}>{props=><Input {...props} disabled={disabled} maxLength={160} value={name} onChange={event=>onName(event.target.value)}/>}</Field>
  <Field label={msg('What records do you need?')} hint={msg('Describe the records to find and what to research. Set record counts, request budgets and retries in Limits and retries. Qualification rules stay separate.')}>{props=><TextArea {...props} disabled={disabled} rows={4} maxLength={12000} value={intent} onChange={event=>onIntent(event.target.value)}/>}</Field></FieldGroup>
 </RecipeSection>
}
