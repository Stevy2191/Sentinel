import type { DrawingEntry } from '@/components/network/drawings/kit'
import UdmSe, { UDM_SE_SIZE } from '@/components/network/drawings/UdmSe'
import UswPro48Poe, { USW_PRO_48_POE_SIZE } from '@/components/network/drawings/UswPro48Poe'
import Es16Xg, { ES_16_XG_SIZE } from '@/components/network/drawings/Es16Xg'

/** Realistic drawings by the key the backend sends (portmon/drawings.go). */
export const DRAWINGS: Record<string, DrawingEntry> = {
  'udm-se': { component: UdmSe, ...UDM_SE_SIZE },
  'usw-pro-48-poe': { component: UswPro48Poe, ...USW_PRO_48_POE_SIZE },
  'es-16-xg': { component: Es16Xg, ...ES_16_XG_SIZE },
}
