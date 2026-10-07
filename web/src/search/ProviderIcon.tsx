import { IconSearch } from '../shell/icons'
import google from './assets/google-g.png'
import tavilyLight from './assets/tavily-mark-black.svg'
import tavilyDark from './assets/tavily-mark-offwhite.svg'
import styles from './SearchSources.module.css'

export function providerName(provider?:string){
 return provider==='google-grounding'?'Google Search':provider==='tavily'?'Tavily':provider
}
/** Only bundled, known assets. An unknown provider can never supply an image URL. */
export function ProviderIcon({provider}:{provider?:string}){
 return <span className={styles.providerIcon} aria-hidden="true">{provider==='google-grounding'?<img src={google} alt=""/>:provider==='tavily'?<><img className={styles.lightMark} src={tavilyLight} alt=""/><img className={styles.darkMark} src={tavilyDark} alt=""/></>:<IconSearch/>}</span>
}
