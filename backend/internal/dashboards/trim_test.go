package dashboards

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

func TestTrimPortBlanksIdentifiersKeepsNames(t *testing.T) {
	neighbor := uuid.New()
	p := services.PortView{DeviceInterface: models.DeviceInterface{
		ID: uuid.New(), DeviceID: uuid.New(), MAC: "aa:bb:cc:dd:ee:ff", NeighborDeviceID: &neighbor,
		IfIndex: 7, Name: "Gi1/0/7", Alias: "uplink",
		Role: "uplink", RoleSource: "auto", RoleReason: "Found by LLDP: CORE-ES48-A port 49",
	}}
	trimPort(&p)
	if p.ID != uuid.Nil || p.DeviceID != uuid.Nil || p.MAC != "" || p.NeighborDeviceID != nil {
		t.Errorf("trimPort left an identifier: %+v", p)
	}
	if p.RoleReason != "" || p.RoleSource != "" {
		t.Errorf("trimPort left why the role was chosen (it names the far end): %+v", p)
	}
	if p.Role != "uplink" {
		t.Errorf("trimPort lost the role, which the drawings frame: %+v", p)
	}
	if p.Name != "Gi1/0/7" || p.Alias != "uplink" || p.IfIndex != 7 {
		t.Errorf("trimPort lost name, alias or if_index: %+v", p)
	}
}
