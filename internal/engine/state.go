package engine

import (
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/states"
)

type StateModuleReader struct {
	path    addrs.ModuleInstance
	backing states.State
}

func (s *StateModuleReader) Module(child addrs.ModuleCallInstance, fn func(*StateModuleReader)) {
	fn(&StateModuleReader{
		path:    s.path.Child(child.Call.Name, child.Key),
		backing: s.backing,
	})
}

func (s *StateModuleReader) ResourceInstance(addr addrs.ResourceInstance, fn func(*StateResourceReader)) {
	//fn(s.backing.Module(s.path).ResourceInstance(addr))
}

type StateResourceReader struct {
}

type WriteState struct {
	backing states.SyncState
}
