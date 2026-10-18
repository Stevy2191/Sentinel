package services

import (
	"context"
	"fmt"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
)

// FaceplateLayoutLookup is FaceplateLayoutService.For: the saved layout for
// a model, or nil.
type FaceplateLayoutLookup interface {
	For(model string) *models.FaceplateLayout
}

// SetFaceplateLayouts lets DevicePorts draw saved layouts. Without it the
// faceplate uses the built-in table and the automatic rules only.
func (s *PortService) SetFaceplateLayouts(l FaceplateLayoutLookup) { s.layouts = l }

// FaceplateLayoutNotice says the saved layout for Model does not fit this
// device's ports (Fit), so it is drawn automatically.
type FaceplateLayoutNotice struct {
	Model string      `json:"model"`
	Fit   portmon.Fit `json:"fit"`
}

func (s *PortService) savedLayout(d *DeviceView) *models.FaceplateLayout {
	if s.layouts == nil {
		return nil
	}
	return s.layouts.For(d.EffectiveModel)
}

// deviceFaceplateSet reports whether the device has its own faceplate
// settings (Edit details), which beat any model layout.
func deviceFaceplateSet(d *DeviceView) bool {
	return d.FaceplateRows != nil || len(d.FaceplateSFPPorts) > 0 ||
		(d.FaceplatePortStyle != nil && *d.FaceplatePortStyle != portmon.PortStyleAuto)
}

func layoutOptions(d *DeviceView) portmon.LayoutOptions {
	o := portmon.LayoutOptions{Rows: d.FaceplateRows, SFPPorts: d.FaceplateSFPPorts, Model: d.EffectiveModel,
		Numbering: portmon.NumberingFor(d.SysDescr)}
	if d.FaceplatePortStyle != nil {
		o.Style = *d.FaceplatePortStyle
	}
	return o
}

// layoutPorts is the device's present physical ports, as the faceplate
// sees them.
func (s *PortService) layoutPorts(ctx context.Context, d *DeviceView) ([]portmon.LayoutPort, error) {
	var rows []models.DeviceInterface
	if err := s.db.WithContext(ctx).Where("device_id = ? AND present", d.ID).Order("if_index").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing ports: %w", err)
	}
	var out []portmon.LayoutPort
	for _, r := range rows {
		if portmon.IsPhysical(portmon.IfInfo{Name: r.Name, Descr: r.Descr, Type: r.IfType, ConnectorPresent: r.ConnectorPresent}) {
			out = append(out, portmon.LayoutPort{IfIndex: r.IfIndex, Name: r.Name, Descr: r.Descr, Unit: r.StackUnit,
				Up: r.OperStatus == "up", SpeedBps: r.SpeedBps})
		}
	}
	return out, nil
}

// FaceplateEditorView is what the layout editor opens with.
type FaceplateEditorView struct {
	Model   string                  `json:"model"`
	Saved   *models.FaceplateLayout `json:"saved"`
	Builtin []portmon.LayoutGroup   `json:"builtin"`
	// Ports is the first stack member's main port numbers.
	Ports []int `json:"ports"`
	// DeviceSettings is whether the device's own Edit details faceplate
	// settings are set (they override any model layout).
	DeviceSettings bool `json:"device_settings"`
	// Start is the saved layout, else the built-in one, else the device's
	// current drawing as groups.
	Start []portmon.LayoutGroup `json:"start"`
}

// FaceplateEditor is the editor's starting state for a device.
func (s *PortService) FaceplateEditor(ctx context.Context, d *DeviceView) (*FaceplateEditorView, error) {
	ports, err := s.layoutPorts(ctx, d)
	if err != nil {
		return nil, err
	}
	first := firstUnit(ports)
	v := &FaceplateEditorView{Model: d.EffectiveModel, Saved: s.savedLayout(d), Ports: portmon.MainPortNumbers(first, portmon.NumberingFor(d.SysDescr)),
		DeviceSettings: deviceFaceplateSet(d)}
	if b := portmon.BuiltinFor(d.EffectiveModel); b != nil {
		v.Builtin = b.Groups
	}
	switch {
	case v.Saved != nil:
		v.Start = v.Saved.Groups
	case v.Builtin != nil:
		v.Start = v.Builtin
	default:
		// The drawing the device shows now, its own settings included.
		v.Start = portmon.GroupsFromFaceplate(portmon.Layout(first, layoutOptions(d)))
	}
	return v, nil
}

// firstUnit is the ports of the first stack member LayoutUnits draws.
func firstUnit(ports []portmon.LayoutPort) []portmon.LayoutPort {
	units := portmon.LayoutUnits(ports, portmon.LayoutOptions{})
	if len(units) == 0 {
		return nil
	}
	var out []portmon.LayoutPort
	for _, p := range ports {
		if p.Unit == units[0].Unit {
			out = append(out, p)
		}
	}
	return out
}

// FaceplatePreview is a device drawn with a layout not yet saved.
type FaceplatePreview struct {
	Faceplates []portmon.UnitFaceplate `json:"faceplates"`
	Fit        portmon.Fit             `json:"fit"`
}

// PreviewFaceplate draws the device with groups as its model's layout, as
// it will look once the device's own settings are cleared, and says how
// the groups and its ports disagree.
func (s *PortService) PreviewFaceplate(ctx context.Context, d *DeviceView, groups []portmon.LayoutGroup) (*FaceplatePreview, error) {
	if err := validateGroups(groups); err != nil {
		return nil, err
	}
	ports, err := s.layoutPorts(ctx, d)
	if err != nil {
		return nil, err
	}
	p := &FaceplatePreview{
		Faceplates: portmon.LayoutUnits(ports, portmon.LayoutOptions{Model: d.EffectiveModel, Saved: groups, IgnoreDeviceSettings: true,
			Numbering: portmon.NumberingFor(d.SysDescr)}),
		Fit: portmon.Fit{Missing: []int{}, Extra: []int{}},
	}
	if fit := portmon.FitUnits(ports, groups, portmon.NumberingFor(d.SysDescr)); fit != nil {
		p.Fit = *fit
	}
	return p, nil
}
