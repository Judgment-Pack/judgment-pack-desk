import type { UserQuestion } from '../assistant/questions'
export const interviewQuestion: UserQuestion = {
  title:'Buyer', question:'Who should we focus on as the first buyer?', selection:'single',
  options:[{id:'operations',label:'Head of operations',description:'Owns recurring exception approvals.'},{id:'risk',label:'Risk or compliance lead',description:'Owns oversight and decision records.'},{id:'it',label:'IT or systems lead',description:'Owns systems and integrations.'}],
  recommendedOptionId:'operations', recommendation:'A starting hypothesis for recurring exception approvals.'
}
