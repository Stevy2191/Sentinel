package portmon

import "regexp"

// BuiltinLayout is the real front panel of a model whose ports the automatic
// rules cannot tell apart. Name is how the model is listed; a saved layout
// for the same model overrides it.
type BuiltinLayout struct {
	Name    string
	Groups  []LayoutGroup
	pattern *regexp.Regexp
}

// Matches reports whether model is this entry's model.
func (b BuiltinLayout) Matches(model string) bool {
	model = NormalizeModel(model)
	return model != "" && b.pattern.MatchString(model)
}

var builtinLayouts = []BuiltinLayout{
	// EdgeSwitch 16 XG: twelve SFP+ cages two high, then four 10GBASE-T RJ45
	// in one row. Its firmware calls every port "10G - Level", which Auto
	// would draw as sixteen cages. ENTITY-MIB gives "UBNT-ES16XG"; the
	// product name is "EdgeSwitch 16 XG" (ES-16-XG).
	{
		Name:    "UBNT-ES16XG",
		pattern: regexp.MustCompile(`(?i)ES-?16-?XG|EdgeSwitch ?16 ?XG`),
		Groups: []LayoutGroup{
			{First: 1, Last: 12, Kind: GroupKindSFP, Rows: 2, Numbering: NumberingAlternate},
			{First: 13, Last: 16, Kind: GroupKindRJ45, Rows: 1},
		},
	},
	// UniFi Dream Machine SE: LAN ports 1-8 two high, the 2.5G WAN port 9
	// on its own, then the two SFP+ cages stacked (10 over 11). Its ports
	// are eth0-eth10, numbered from 1 by NumberingFor.
	{
		Name:    "UDM-SE",
		pattern: regexp.MustCompile(`(?i)^UDM-?SE$`),
		Groups: []LayoutGroup{
			{First: 1, Last: 8, Kind: GroupKindRJ45, Rows: 2, Numbering: NumberingAlternate},
			{First: 9, Last: 9, Kind: GroupKindRJ45, Rows: 1},
			{First: 10, Last: 11, Kind: GroupKindSFP, Rows: 2, Numbering: NumberingAlternate},
		},
	},
}

// BuiltinLayouts lists the built-in table, copied so callers cannot change it.
func BuiltinLayouts() []BuiltinLayout {
	out := make([]BuiltinLayout, len(builtinLayouts))
	for i, b := range builtinLayouts {
		b.Groups = append([]LayoutGroup(nil), b.Groups...)
		out[i] = b
	}
	return out
}

// BuiltinFor is the built-in entry for model, or nil.
func BuiltinFor(model string) *BuiltinLayout {
	for _, b := range BuiltinLayouts() {
		if b.Matches(model) {
			return &b
		}
	}
	return nil
}
