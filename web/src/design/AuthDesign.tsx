/** Design-only entry point. No session bootstrap, API calls, storage, or OAuth.
 * Vite serves /auth-design.html; the production entry does not import this file.
 * English copy is a review artifact; shipped routes must use the i18n catalog. */
import { useEffect, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { Disclosure } from '../ui/Disclosure'
import { IconChevronLeft, IconChevronDown, IconClose, IconHelp, IconLink, IconPanelLeft } from '../shell/icons'
import '../styles.css'
import styles from './AuthDesign.module.css'

type Screen = 'local' | 'signin' | 'setup'
type Theme = 'system' | 'light' | 'dark'
const screens: { id: Screen; label: string }[] = [
  { id: 'local', label: 'Local recovery' },
  { id: 'signin', label: 'Hosted sign-in' },
  { id: 'setup', label: 'Admin setup' }
]
function currentScreen(): Screen {
  const value = new URLSearchParams(location.hash.slice(1)).get('screen')
  return value === 'signin' || value === 'setup' ? value : 'local'
}
function Brand({ compact = false }: { compact?: boolean }) {
  return <div className={[styles.brand, compact ? styles.compactBrand : ''].join(' ')}>
    <span className={styles.brandMark} aria-hidden="true">U</span><span>Unveil</span>
  </div>
}
function App() {
  const [screen, setScreen] = useState<Screen>(currentScreen)
  const [theme, setTheme] = useState<Theme>('system')
  const [notice, setNotice] = useState('Static mock · No sign-in or settings changes')
  useEffect(() => {
    const change = () => { setScreen(currentScreen()); setNotice('Static mock · No sign-in or settings changes') }
    window.addEventListener('hashchange', change)
    return () => window.removeEventListener('hashchange', change)
  }, [])
  useEffect(() => {
    if (theme === 'system') document.documentElement.removeAttribute('data-theme')
    else document.documentElement.setAttribute('data-theme', theme)
  }, [theme])
  function preview(action: string) { setNotice(`Preview only: ${action}. No request was sent.`) }
  return <div className={styles.review}>
    <div className={styles.reviewBar}>
      <span className={styles.reviewLabel}>Authentication design</span>
      <nav className={styles.screenNav} aria-label="Design screens">
        {screens.map(item => <a key={item.id} href={`#screen=${item.id}`} aria-current={screen === item.id ? 'page' : undefined}>{item.label}</a>)}
      </nav>
      <label className={styles.themeLabel}>Appearance
        <select aria-label="Preview appearance" value={theme} onChange={event => setTheme(event.target.value as Theme)}>
          <option value="system">System</option><option value="light">Light</option><option value="dark">Dark</option>
        </select>
      </label>
    </div>
    <div className={styles.stage} key={screen}>
      {screen === 'setup' ? <Setup preview={preview} /> : <Entry screen={screen} preview={preview} />}
    </div>
    <div className={styles.reviewNote} role="status">{notice}</div>
  </div>
}
function Entry({ screen, preview }: { screen: 'local' | 'signin'; preview: (action: string) => void }) {
  const [sso, setSso] = useState(false)
  const [steps, setSteps] = useState(false)
  const heading = useRef<HTMLHeadingElement>(null)
  function changeSSO(next: boolean) { setSso(next); requestAnimationFrame(() => heading.current?.focus()) }
  return <div className={styles.entry}>
    <header className={styles.entryHeader}><Brand compact />
      <Button variant="quiet" className={styles.language} onClick={() => preview('language selection')}>English <IconChevronDown /></Button>
    </header>
    <main className={styles.entryMain}>
      <div className={styles.entryContent}>
        <span className={styles.largeMark} aria-hidden="true">U</span>
        {screen === 'local' ? <>
          <h1>Open your local Desk</h1>
          <p className={styles.lead}>Reopen Desk on this computer to continue.<br />Your saved packs and chats are still here.</p>
          <Button className={styles.authButton} variant="primary" aria-expanded={steps} aria-controls="reopening-steps" onClick={() => setSteps(!steps)}>
            {steps ? 'Hide reopening steps' : 'How to reopen Desk'}
          </Button>
          {steps && <div id="reopening-steps" className={styles.reopenSteps}>
            <ol><li>Open the Desk application on this computer.</li><li>If you started Desk from a terminal, open the launch link it printed.</li><li>Continue in the newly opened browser tab.</li></ol>
            <p>You don’t need to create an account.</p>
          </div>}
          <p className={styles.localHint}>Local session · No cloud account required</p>
        </> : sso ? <>
          <h1 ref={heading} tabIndex={-1}>Sign in to your workspace</h1>
          <p className={styles.lead}>Use the workspace address from your invitation.</p>
          <form className={styles.authForm} onSubmit={event => { event.preventDefault(); preview('workspace lookup and company sign-in') }}>
            <label htmlFor="workspace-address">Workspace address</label>
            <Input id="workspace-address" autoComplete="off" placeholder="your-company" required />
            <Button type="submit" variant="primary" className={styles.authButton}>Continue</Button>
          </form>
          <Button variant="quiet" onClick={() => changeSSO(false)}><IconChevronLeft /> All sign-in options</Button>
        </> : <>
          <h1 ref={heading} tabIndex={-1}>Sign in to Unveil</h1>
          <p className={styles.lead}>Continue to your workspace.</p>
          <div className={styles.providerButtons}>
            <Button className={styles.authButton} onClick={() => preview('redirect to Google')}>Continue with Google</Button>
            <Button className={styles.authButton} onClick={() => preview('redirect to Microsoft')}>Continue with Microsoft</Button>
          </div>
          <div className={styles.or}><span>or</span></div>
          <Button className={styles.authButton} variant="quiet" onClick={() => changeSSO(true)}>Use company SSO</Button>
          <p className={styles.signinHint}>Use the account associated with your workspace.</p>
        </>}
      </div>
    </main>
    <footer className={styles.entryFooter}>
      <span>{screen === 'local' ? 'On this computer' : 'Unveil workspace'}</span>
      <Button variant="quiet" onClick={() => preview('sign-in help')}><IconHelp /> Need help?</Button>
    </footer>
  </div>
}
function Setup({ preview }: { preview: (action: string) => void }) {
  const [open, setOpen] = useState(true)
  const setupButton = useRef<HTMLButtonElement>(null)
  const paneHeading = useRef<HTMLHeadingElement>(null)
  function openPane() { setOpen(true); requestAnimationFrame(() => paneHeading.current?.focus()) }
  function closePane() { setOpen(false); requestAnimationFrame(() => setupButton.current?.focus()) }
  return <div className={styles.admin}>
    <header className={styles.adminHeader}>
      <span className={styles.decorativeToggle}><IconPanelLeft /></span><Brand compact />
      <span className={styles.projectName}>Example workspace</span>
      <span className={styles.account}><span className={styles.avatar}>AD</span> Administrator <IconChevronDown /></span>
    </header>
    <div className={styles.adminBody}>
      <nav className={styles.sidebar} aria-label="Example Admin navigation">
        <Button variant="quiet" onClick={() => preview('return to workspace')}><IconChevronLeft /> Back to app</Button>
        <p>This workspace</p>
        <span>General</span><span>Storage &amp; data</span><span>Assistant</span><span>Connections</span>
        <span className={styles.selectedNav} aria-current="page">Sign-in &amp; access</span>
      </nav>
      <main className={styles.settings}>
        <div className={styles.breadcrumb}><span>Admin</span><span>/</span><strong>Sign-in &amp; access</strong></div>
        <div className={styles.settingsBody}>
          <h1>Sign-in &amp; access</h1>
          <p className={styles.sectionDescription}>Choose how people sign in to this workspace.</p>
          <section className={styles.settingsSection}>
            <h2>Identity provider</h2>
            <div className={styles.providerRow}>
              <span className={styles.providerIcon}><IconLink /></span>
              <div><strong>Microsoft Entra ID</strong><p>Company sign-in</p><span className={styles.draftTag}>Draft · Not enabled</span></div>
              <Button ref={setupButton} onClick={openPane} aria-expanded={open}>Set up</Button>
            </div>
            <p className={styles.settingHint}>Test your provider before requiring sign-in.</p>
          </section>
          <section className={styles.settingsSection}>
            <h2>Access requirement</h2>
            <div className={styles.requirement}><div><strong>Require sign-in</strong><p>Available after a successful provider test.</p></div><span className={styles.draftTag}>Not enabled</span></div>
            <p className={styles.settingHint}>For a hosted workspace, sign-in must be enabled before people can access it.</p>
          </section>
          <p className={styles.connectionHint}>Drive, Gmail and other sources are managed in Connections.</p>
        </div>
      </main>
      {open && <aside className={styles.setupPane} aria-labelledby="setup-title" onKeyDown={event => { if (event.key === 'Escape') closePane() }}>
        <header className={styles.paneHeader}><h2 id="setup-title" ref={paneHeading} tabIndex={-1}>Set up company sign-in</h2><Button variant="quiet" aria-label="Close setup" onClick={closePane}><IconClose /></Button></header>
        <div className={styles.paneBody}>
          <div className={styles.paneIntro}><strong>Microsoft Entra ID</strong><p>Use your organization’s Microsoft accounts.</p></div>
          <div className={styles.field}><label htmlFor="signin-name">Display name</label><Input id="signin-name" value="Company sign-in" readOnly /><p>Shown on your workspace sign-in page.</p></div>
          <div className={styles.field}><label htmlFor="issuer">Issuer URL</label><Input id="issuer" value="https://login.microsoftonline.com/{tenant-id}/v2.0" readOnly /><p>Use the tenant that can access this workspace.</p></div>
          <div className={styles.field}><label htmlFor="client-id">Application (client) ID</label><Input id="client-id" placeholder="Client ID from your app registration" readOnly /></div>
          <div className={styles.field}><label htmlFor="callback">Redirect URL</label><div className={styles.copyField}><Input id="callback" value="https://workspace.example/auth/callback" readOnly /><Button variant="quiet" onClick={() => preview('copy redirect URL')}>Copy</Button></div><p>Add this URL to your application registration.</p></div>
          <p className={styles.credentialNote}>Client credentials are supplied securely by your deployment administrator.</p>
          <Disclosure title="Setup instructions" className={styles.guide}>
            <ol>
              <li><strong>Register an application</strong><p>In Microsoft Entra, create an app registration for this workspace and choose its allowed accounts.</p></li>
              <li><strong>Add the redirect URL</strong><p>For a hosted workspace, add the HTTPS redirect URL above as a Web redirect.</p></li>
              <li><strong>Configure the provider</strong><p>Use the tenant’s issuer URL and application ID. Supply the client credential through the deployment’s protected secret store.</p></li>
              <li><strong>Test, then enable</strong><p>Confirm the account and workspace access. Keep an administrator recovery path before requiring sign-in.</p></li>
            </ol>
            <a href="https://learn.microsoft.com/en-us/entra/identity-platform/quickstart-register-app" target="_blank" rel="noreferrer">Microsoft’s official setup documentation ↗</a>
          </Disclosure>
        </div>
        <footer className={styles.paneFooter}><p>Sign-in stays off until setup is tested and enabled.</p><div><Button onClick={closePane}>Cancel</Button><Button variant="primary" onClick={() => preview('test the configured provider')}>Test sign-in</Button></div></footer>
      </aside>}
    </div>
  </div>
}

createRoot(document.getElementById('root')!).render(<App />)
