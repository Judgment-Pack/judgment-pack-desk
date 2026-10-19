import {useState} from 'react'
import {msg} from '../i18n'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {TextArea} from '../ui/TextArea'
import {Button} from '../ui/Button'
import type {RecordReview} from './client'
import styles from './RecordReviewDecision.module.css'

export function RecordReviewDecision({decision,note,onDecision,onNote,disabled=false,batch=false}:{decision?:RecordReview['decision'];note:string;onDecision:(decision:RecordReview['decision'])=>void;onNote:(note:string)=>void;disabled?:boolean;batch?:boolean}){
 const [showNote,setShowNote]=useState(!!note)
 return <div className={styles.root}>
  <div className={styles.row}><Field label={msg('Review decision')}>{props=><Select {...props} disabled={disabled} value={decision} placeholder={msg('Choose a review decision')} options={[{value:'accepted',label:msg('Ready for evaluation')},{value:'needs-information',label:msg('Needs more information')},{value:'rejected',label:msg('Exclude')}]} onValueChange={value=>onDecision(value as RecordReview['decision'])}/>}</Field><Button variant="quiet" disabled={disabled} aria-expanded={showNote} onClick={()=>setShowNote(!showNote)}>{showNote?msg('Hide note'):note?msg('Edit note'):msg('Add note')}</Button></div>
  {showNote&&<Field label={msg('Review note')} hint={batch?msg('Optional note for all selected records'):undefined}>{props=><TextArea {...props} rows={2} maxLength={2000} disabled={disabled} value={note} onChange={event=>onNote(event.target.value)}/>}</Field>}
 </div>
}
