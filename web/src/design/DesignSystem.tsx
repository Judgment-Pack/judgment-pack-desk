/** Development reference, served by Vite at /design-system.html.
 * It renders production primitives without a project, runtime, or credentials. */
import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { applyDensity, applyTheme } from '../config/theme'
import type { Density, ThemeChoice } from '../config/deskConfig'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { SettingsSection } from '../ui/SettingsSection'
import { TextArea } from '../ui/TextArea'
import '../styles.css'
import styles from './DesignSystem.module.css'

function DesignSystem() {
  const [theme, setTheme] = useState<ThemeChoice>('system')
  const [density, setDensity] = useState<Density>('comfortable')
  const [provider, setProvider] = useState('gemini')
  return (
    <main className={styles.page}>
      <header className={styles.header}>
        <div>
          <p className={styles.eyebrow}>Unveil · Design system</p>
          <h1>A calm workspace, built consistently.</h1>
          <p>Shared foundations and live production components for every screen.</p>
        </div>
        <div className={styles.preferences}>
          <Field label="Theme">
            {(wiring) => <Select {...wiring} value={theme} options={[
              { value: 'system', label: 'System' }, { value: 'light', label: 'Light' }, { value: 'dark', label: 'Dark' }
            ]} onValueChange={(value) => { setTheme(value as ThemeChoice); applyTheme(value as ThemeChoice) }} />}
          </Field>
          <Field label="Density">
            {(wiring) => <Select {...wiring} value={density} options={[
              { value: 'comfortable', label: 'Comfortable' }, { value: 'compact', label: 'Compact' }
            ]} onValueChange={(value) => { setDensity(value as Density); applyDensity(value as Density) }} />}
          </Field>
        </div>
      </header>

      <div className={styles.grid}>
        <SettingsSection level={2} title="Actions" description="One primary action per task. Every state comes from the same component.">
          <div className={styles.actions}>
            <Button variant="primary">Save changes</Button>
            <Button>Test connection</Button>
            <Button variant="quiet">Cancel</Button>
            <Button variant="danger">Remove endpoint</Button>
            <Button disabled>Unavailable</Button>
            <Button variant="primary" disabled aria-busy="true">Saving…</Button>
          </div>
        </SettingsSection>

        <SettingsSection level={2} title="Color roles" description="Neutral surfaces, readable text, and a restrained teal accent.">
          <div className={styles.swatches}>
            <div className={styles.surface}>Surface</div>
            <div className={styles.raised}>Raised</div>
            <div className={styles.accent}>Accent</div>
            <div className={styles.warning}>Warning</div>
            <div className={styles.danger}>Danger</div>
          </div>
        </SettingsSection>

        <div className={styles.form}>
          <SettingsSection level={2} title="Settings form" description="Labels, controls, helpers, and feedback follow one spacing scale."
            footer={<div className={styles.actions}><Button variant="primary">Save settings</Button><Button variant="quiet">Cancel</Button></div>}>
            <Field label="Provider">
              {(wiring) => <Select {...wiring} value={provider} onValueChange={setProvider} options={[
                { value: 'gemini', label: 'Google Gemini' }, { value: 'anthropic', label: 'Anthropic' }
              ]} />}
            </Field>
            <Field label="Display name" hint="A name people recognize in the workspace.">
              {(wiring) => <Input {...wiring} defaultValue="Team assistant" />}
            </Field>
            <Field label="Endpoint URL" error="Enter a complete URL.">
              {(wiring) => <Input {...wiring} defaultValue="incomplete-address" />}
            </Field>
            <Field label="Notes" hint="Optional context for this configuration.">
              {(wiring) => <TextArea {...wiring} placeholder="Add a note…" />}
            </Field>
            <Field label="Unavailable field" hint="Disabled controls retain their size and a readable label.">
              {(wiring) => <Input {...wiring} value="Managed by your organization" disabled readOnly />}
            </Field>
          </SettingsSection>
        </div>

        <div>
          <SettingsSection level={2} title="Typography" description="One scale for navigation, forms, and page content.">
            <p className={styles.pageType}>Page title · 24</p>
            <p className={styles.headingType}>Section title · 20</p>
            <p className={styles.bodyType}>Body text · 15</p>
            <p className={styles.labelType}>Labels and controls · 14</p>
            <p className={styles.helperType}>Supporting text · 13</p>
            <p className={styles.captionType}>Compact metadata · 12</p>
          </SettingsSection>
          <SettingsSection level={2} title="Layout rules" description="Comfortable settings, compact working surfaces.">
            <ul className={styles.rules}>
              <li>36px standard controls; 40px settings controls.</li>
              <li>Compact density reduces controls to 32px and 36px.</li>
              <li>4, 8, 12, 16, 24, 32, and 48px spacing steps.</li>
              <li>24px page gutters and 224px default navigation.</li>
              <li>Inline actions stay attached to the task they affect.</li>
            </ul>
          </SettingsSection>
        </div>
      </div>
    </main>
  )
}

const root = document.getElementById('root')
if (root) createRoot(root).render(<DesignSystem />)
