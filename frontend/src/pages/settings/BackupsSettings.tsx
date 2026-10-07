import BackupRestore from '@/components/BackupRestore'
import { useToasts, Toaster } from '@/components/Toast'

/** Settings → Backups: download, schedule and restore database backups. */
export default function BackupsSettings() {
  const { toasts, push } = useToasts()
  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h2 className="text-2xl font-light text-white">Backups</h2>
        <p className="mt-1 text-sm text-slate-400">Copies of Sentinel&apos;s database, and restoring from one.</p>
      </div>
      <BackupRestore push={push} />
      <Toaster toasts={toasts} />
    </div>
  )
}
