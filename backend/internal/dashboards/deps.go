package dashboards

// Deps is what the widgets read their data from. Each widget takes only the
// parts it needs, as interfaces, so widgets are tested without the real
// services. main.go fills it in.
type Deps struct{}

// NewDefaultRegistry registers every widget type Sentinel ships, in the order
// the editor lists them.
func NewDefaultRegistry(d Deps) *Registry {
	return NewRegistry(
		labelWidget{},
	)
}
