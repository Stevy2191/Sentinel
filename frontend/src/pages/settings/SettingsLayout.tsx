import { Navigate, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useAuthContext } from '@/context/AuthContext'
import { useAppConfig } from '@/context/AppConfigContext'
import { sectionForPath, visibleSections, type SettingsGroup, type SettingsSection } from '@/utils/settingsNav'

const GROUPS: SettingsGroup[] = ['Sentinel', 'Network', 'Help']

function linkClass({ isActive }: { isActive: boolean }) {
  return `block rounded-md px-3 py-1.5 text-sm transition ${
    isActive ? 'bg-white/10 text-white' : 'text-slate-300 hover:bg-white/5 hover:text-white'
  }`
}

/** SettingsMenu is the left-hand menu: groups, with Notifications' two pages
 *  under one heading. Channels uses `end` so History does not light it too. */
function SettingsMenu({ sections }: { sections: SettingsSection[] }) {
  return (
    <nav className="space-y-4" aria-label="Settings sections">
      {GROUPS.map((group) => {
        const inGroup = sections.filter((s) => s.group === group)
        if (inGroup.length === 0) return null
        return (
          <div key={group} className="space-y-0.5">
            <div className="px-3 pb-1 text-[11px] font-semibold uppercase tracking-widest text-slate-500">{group}</div>
            {inGroup.map((s, i) => {
              const heading = s.parent && inGroup[i - 1]?.parent !== s.parent
              return (
                <div key={s.key}>
                  {heading && <div className="px-3 pb-0.5 pt-1 text-sm text-slate-400">{s.parent}</div>}
                  <NavLink to={s.path} end={s.key === 'notifications'} className={linkClass}>
                    <span className={s.parent ? 'pl-3' : ''}>{s.label}</span>
                  </NavLink>
                </div>
              )
            })}
          </div>
        )
      })}
    </nav>
  )
}

/** SettingsLayout is the one Settings area: the menu on the left (a select on
 *  narrow screens) and the chosen section on the right. A non-admin who opens
 *  an admin section (an old bookmark, a shared link) is sent to About; the API
 *  refuses them anyway, so this is tidiness, not security. */
export default function SettingsLayout() {
  const { currentUser } = useAuthContext()
  const { appName } = useAppConfig()
  const isAdmin = currentUser?.is_admin ?? false
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const section = sectionForPath(pathname)
  const sections = visibleSections(isAdmin)

  if (section && section.adminOnly && !isAdmin) {
    return <Navigate to="/settings/about" replace />
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="vs-title text-4xl">Settings</h1>
        <p className="text-sm text-slate-400">{isAdmin ? `Configure this ${appName} instance` : 'Instance information'}</p>
      </div>

      <label className="block md:hidden">
        <span className="sr-only">Settings section</span>
        <select className="rd-select w-full" value={section?.path ?? ''} onChange={(e) => navigate(e.target.value)}>
          {section === undefined && <option value="">Choose a section…</option>}
          {GROUPS.map((group) => {
            const inGroup = sections.filter((s) => s.group === group)
            if (inGroup.length === 0) return null
            return (
              <optgroup key={group} label={group}>
                {inGroup.map((s) => (
                  <option key={s.key} value={s.path}>
                    {s.parent ? `${s.parent} — ${s.label}` : s.label}
                  </option>
                ))}
              </optgroup>
            )
          })}
        </select>
      </label>

      <div className="md:flex md:gap-8">
        <aside className="hidden w-56 shrink-0 md:block">
          <SettingsMenu sections={sections} />
        </aside>
        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
