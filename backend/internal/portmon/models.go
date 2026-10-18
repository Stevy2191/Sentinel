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
