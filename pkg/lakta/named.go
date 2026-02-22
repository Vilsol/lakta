package lakta

// NamedBase can be embedded in module structs to satisfy the NamedModule interface.
type NamedBase struct{ name string }

// NewNamedBase creates a NamedBase with the given instance name.
func NewNamedBase(name string) NamedBase { return NamedBase{name: name} }

// Name returns the instance name.
func (n NamedBase) Name() string { return n.name }
