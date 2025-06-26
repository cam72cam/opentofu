package engine

type ErrCircularDependency struct {
}

func (e ErrCircularDependency) Error() string {
	return "Cycle Detected: TODO"
}

type ErrPromiseResolveFailed struct {
}

func (e ErrPromiseResolveFailed) Error() string {
	return "Promise Resolve Failed: TODO"
}
