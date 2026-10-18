import {msg,useLocale} from '../i18n'
import type {PolicyAuthoringSource} from './authoringSource'
import styles from './PolicyPage.module.css'
export function PolicySourceIdentity({source}:{source:PolicyAuthoringSource}){
 useLocale()
 return <><dl className={styles.facts}><dt>{msg('Title')}</dt><dd>{source.title}</dd><dt>{msg('Artifact version')}</dt><dd>{source.artifactVersion}</dd><dt>{msg('Policy source')}</dt><dd>{source.publicationId?msg('Selected publication'):msg('Saved revision · no publication selected')}</dd><dt>{msg('Policy')}</dt><dd className={styles.digest}>{source.policyId}</dd><dt>{msg('Saved revision')}</dt><dd className={styles.digest}>{source.revisionId}</dd>{source.publicationId&&<><dt>{msg('Publication')}</dt><dd className={styles.digest}>{source.publicationId}</dd></>}<dt>{msg('Candidate digest')}</dt><dd className={styles.digest}>{source.candidateDigest}</dd></dl><p>{msg('Informational authoring source. Human review must establish the interpretation and its coverage.')}</p></>
}
