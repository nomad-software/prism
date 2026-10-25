// Package history keeps the values a graph shows: each line of it is a
// series of values, each at the time it was measured, from the right edge of
// the graph, which is now, back to its left edge, which is a span of time
// before. It has no dependency on the UI.
package history

import "time"

// MinSpan is the least time a graph spans.
const MinSpan = time.Minute

// Span returns the time a graph spans when it is measured at the interval:
// a minute, or two intervals where they are longer than half a minute, so
// that there is always a line to show and not a single point.
func Span(interval time.Duration) time.Duration {
	return max(MinSpan, 2*interval)
}

// Point is a value measured at a time.
type Point struct {
	T time.Time
	V float64
}

// Series is the values of one line of a graph, oldest first.
type Series struct {
	points []Point
}

// Add adds a value measured at t, which is no earlier than the last.
func (s *Series) Add(t time.Time, v float64) {
	s.points = append(s.points, Point{T: t, V: v})
}

// Fill starts an empty series with the value at the span before the time,
// so that its line is drawn across the whole of a graph from its first
// reading, which is then added at the time.
func (s *Series) Fill(t time.Time, span time.Duration, v float64) {
	if len(s.points) == 0 {
		s.Add(t.Add(-span), v)
	}
}

// Trim drops the values from before the time, except the newest of those,
// which the line is drawn from so that it reaches the left edge of the graph.
func (s *Series) Trim(before time.Time) {
	i := 0
	for i+1 < len(s.points) && !s.points[i+1].T.After(before) {
		i++
	}
	if i > 0 {
		s.points = append(s.points[:0], s.points[i:]...)
	}
}

// Points returns the values, oldest first. They are not to be changed.
func (s *Series) Points() []Point {
	return s.points
}

// Last returns the newest value, and false if there are none.
func (s *Series) Last() (Point, bool) {
	if len(s.points) == 0 {
		return Point{}, false
	}
	return s.points[len(s.points)-1], true
}

// Max returns the largest value since the time, or 0 if there are none.
func (s *Series) Max(since time.Time) float64 {
	m := 0.0
	for _, p := range s.points {
		if !p.T.Before(since) {
			m = max(m, p.V)
		}
	}
	return m
}
