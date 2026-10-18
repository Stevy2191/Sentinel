import { useNavigate } from 'react-router-dom'
import Faceplate from '@/components/network/Faceplate'
import RealisticFaceplate from '@/components/network/RealisticFaceplate'
import type { PortGridData } from '@/types/dashboards'

/** A device's front panel on a dashboard: its realistic drawing when the
 *  model has one (always shown whole), else the simple faceplate. */
export default function PortGridWidget({ data, linkable }: { data: PortGridData; linkable: boolean }) {
  const navigate = useNavigate()
  const faceplates = data.faceplates ?? []
  const onSelect = (ifIndex: number) => {
    if (linkable && data.device_id) navigate(`/network/devices/${data.device_id}/ports/${ifIndex}`)
  }
  if (faceplates.some((f) => !!f.drawing)) {
    return (
      <RealisticFaceplate
        faceplates={faceplates}
        ports={data.ports ?? []}
        deviceState={data.device_state}
        indicators={data.indicators}
        title={data.device_name}
        subtitle={data.model || undefined}
        selected={null}
        onSelect={onSelect}
        fit="widget"
      />
    )
  }
  return (
    <Faceplate
      faceplates={faceplates}
      ports={data.ports ?? []}
      title={data.device_name}
      subtitle={data.model || undefined}
      selected={null}
      onSelect={onSelect}
    />
  )
}
