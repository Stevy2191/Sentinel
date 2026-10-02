import { useNavigate } from 'react-router-dom'
import Faceplate from '@/components/network/Faceplate'
import type { PortGridData } from '@/types/dashboards'

export default function PortGridWidget({ data, linkable }: { data: PortGridData; linkable: boolean }) {
  const navigate = useNavigate()
  return (
    <Faceplate
      faceplates={data.faceplates ?? []}
      ports={data.ports ?? []}
      title={data.device_name}
      subtitle={data.model || undefined}
      selected={null}
      onSelect={(ifIndex) => {
        if (linkable && data.device_id) navigate(`/network/devices/${data.device_id}/ports/${ifIndex}`)
      }}
    />
  )
}
