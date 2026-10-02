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
	}}
	trimPort(&p)
	if p.ID != uuid.Nil || p.DeviceID != uuid.Nil || p.MAC != "" || p.NeighborDeviceID != nil {
		t.Errorf("trimPort left an identifier: %+v", p)
	}
	if p.Name != "Gi1/0/7" || p.Alias != "uplink" || p.IfIndex != 7 {
		t.Errorf("trimPort lost name, alias or if_index: %+v", p)
	}
}
