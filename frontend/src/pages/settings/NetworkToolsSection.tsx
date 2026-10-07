import NetToolsSettings from '@/components/settings/NetToolsSettings'
import { useToasts, Toaster } from '@/components/Toast'

/** Settings → Network tools: the port-check allowlist, the Sentinel-server
 *  switch and how long run history is kept. */
export default function NetworkToolsSection() {
  const { toasts, push } = useToasts()
  return (
    <div className="max-w-3xl space-y-6">
      <h2 className="text-2xl font-light text-white">Network tools</h2>
      <NetToolsSettings push={push} />
      <Toaster toasts={toasts} />
    </div>
  )
}
