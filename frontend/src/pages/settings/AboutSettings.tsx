import { ExternalLink } from 'lucide-react'
import SettingsCard from '@/components/SettingsCard'
import { useAppConfig } from '@/context/AppConfigContext'
import { useSystemVersion } from '@/hooks/useSystemVersion'

const GITHUB_URL = 'https://github.com/Stevy2191/Sentinel'

/** True for a version CI actually tagged, e.g. "0.1.0" — false for "dev" or a branch name. */
const isReleasedVersion = (v: string) => /^\d+\.\d+\.\d+/.test(v)

/** Settings → About: what this instance is running, for everyone. */
export default function AboutSettings() {
  const { appName } = useAppConfig()
  const version = useSystemVersion()
  return (
    <div className="max-w-3xl space-y-6">
      <h2 className="text-2xl font-light text-white">About</h2>
      <SettingsCard title="Application">
        <dl className="space-y-2 text-sm">
          <div className="flex justify-between">
            <dt className="text-slate-500">Name</dt>
            <dd className="font-medium">{appName}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-slate-500">Version</dt>
            <dd className="font-medium">
              {version === null ? (
                '…'
              ) : isReleasedVersion(version) ? (
                <a
                  className="hover:underline"
                  href={`${GITHUB_URL}/releases/tag/v${version}`}
                  target="_blank"
                  rel="noreferrer"
                >
                  Sentinel v{version}
                </a>
              ) : (
                `Sentinel (development build)`
              )}
            </dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-slate-500">License</dt>
            <dd className="font-medium">
              <a
                className="hover:underline"
                href={`${GITHUB_URL}/blob/main/LICENSE`}
                target="_blank"
                rel="noreferrer"
              >
                AGPL-3.0
              </a>
            </dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-slate-500">Frontend</dt>
            <dd className="font-medium">React + TypeScript + Vite</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-slate-500">Database</dt>
            <dd className="font-medium">PostgreSQL</dd>
          </div>
        </dl>
      </SettingsCard>
      <SettingsCard title="Links">
        <div className="flex flex-wrap gap-2">
          <a className="btn-secondary" href={`${GITHUB_URL}#readme`} target="_blank" rel="noreferrer">
            <ExternalLink className="h-4 w-4" /> Documentation
          </a>
          <a className="btn-secondary" href={GITHUB_URL} target="_blank" rel="noreferrer">
            <ExternalLink className="h-4 w-4" /> GitHub
          </a>
          <a className="btn-secondary" href={`${GITHUB_URL}/issues`} target="_blank" rel="noreferrer">
            <ExternalLink className="h-4 w-4" /> Report Issue
          </a>
        </div>
      </SettingsCard>
    </div>
  )
}
