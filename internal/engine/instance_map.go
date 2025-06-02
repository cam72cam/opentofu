package engine

import "github.com/opentofu/opentofu/internal/addrs"

type InstanceMap[T any] struct {
	entries map[string]map[addrs.InstanceKey]T
	//keys    []addrs.ModuleInstance
}

func NewInstanceMap[T any]() InstanceMap[T] {
	return InstanceMap[T]{
		entries: map[string]map[addrs.InstanceKey]T{},
	}
}

func (i InstanceMap[T]) Get(mi addrs.ModuleInstance, k addrs.InstanceKey) (T, bool) {
	t, ok := i.entries[mi.String()][k]
	return t, ok
}
func (i InstanceMap[T]) All(mi addrs.ModuleInstance) map[addrs.InstanceKey]T {
	return i.entries[mi.String()]
}

/*func (i InstanceMap[T]) Keys() []addrs.ModuleInstance {
	return i.keys
}*/

func (i *InstanceMap[T]) Set(mi addrs.ModuleInstance, k addrs.InstanceKey, t T) {
	//i.keys = append(i.keys, mi)
	if _, ok := i.entries[mi.String()]; !ok {
		i.entries[mi.String()] = map[addrs.InstanceKey]T{}
	}
	i.entries[mi.String()][k] = t
}
