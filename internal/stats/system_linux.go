package stats

func newSystem() Source {
	return Linux{Root: "/"}
}
