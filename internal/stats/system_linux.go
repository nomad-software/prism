package stats

// firstUserUID is the first user ID Linux gives people, as UID_MIN in
// /etc/login.defs is by default.
const firstUserUID = 1000

func newSystem() Source {
	return Linux{Root: "/"}
}
