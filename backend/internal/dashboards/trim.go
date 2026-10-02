package dashboards

import (
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// idFor is id for a logged-in viewer and nil for a public one: public
// responses never carry subject ids, so the public page cannot link into
// Sentinel or learn what exists.
func idFor(v Viewer, id uuid.UUID) *uuid.UUID {
	if v.Public {
		return nil
	}
	return &id
}

// trimPort blanks what a public view must not see on a port: its ids, its
// hardware address and the id of the device at its other end. Names,
// aliases, status and traffic stay.
func trimPort(p *services.PortView) {
	p.ID = uuid.Nil
	p.DeviceID = uuid.Nil
	p.MAC = ""
	p.NeighborDeviceID = nil
}
