package engine

import "sort"

// CommandSpec describes a command (Rust CommandSpec): the id everything dispatches by, the
// label and ribbon location the UI shows, the default shortcut, a parameter description for
// agents, whether it changes the document, when it is enabled, and what it does.
type CommandSpec struct {
	ID       string
	Label    string
	Location string
	Shortcut string
	Params   string
	Mutates  bool
	// Enabled returns "" when the command can run, or why it can't.
	Enabled func(*Session) string
	Run     func(*Session, Params) (any, error)
}

// Registry holds the commands by id.
type Registry struct {
	byID map[string]CommandSpec
}

// NewRegistry builds a registry from specs.
func NewRegistry(specs ...[]CommandSpec) *Registry {
	r := &Registry{byID: map[string]CommandSpec{}}
	for _, group := range specs {
		for _, c := range group {
			r.byID[c.ID] = c
		}
	}
	return r
}

// Get finds a command.
func (r *Registry) Get(id string) (CommandSpec, bool) {
	c, ok := r.byID[id]
	return c, ok
}

// All lists the commands by id.
func (r *Registry) All() []CommandSpec {
	out := make([]CommandSpec, 0, len(r.byID))
	for _, c := range r.byID {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

var defaultRegistry = NewRegistry(textCommands(), formatCommands(), selectCommands(), rangeCommands(), editCommands())

// Commands lists the session's commands by id.
func (s *Session) Commands() []CommandSpec { return s.registry.All() }
