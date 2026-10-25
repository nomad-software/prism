package history

import (
	"reflect"
	"testing"
	"time"
)

func TestSpan(t *testing.T) {
	for _, c := range []struct{ interval, want time.Duration }{
		{time.Second, time.Minute},
		{2 * time.Second, time.Minute},
		{30 * time.Second, time.Minute},
		{31 * time.Second, 62 * time.Second},
		{time.Hour, 2 * time.Hour},
	} {
		if got := Span(c.interval); got != c.want {
			t.Errorf("Span(%v) = %v, want %v", c.interval, got, c.want)
		}
	}
}

// at returns a time some seconds after a fixed one.
func at(seconds int) time.Time {
	return time.Unix(1_700_000_000+int64(seconds), 0)
}

func TestSeriesEmpty(t *testing.T) {
	var s Series
	if len(s.Points()) != 0 {
		t.Error("a new series is not empty")
	}
	if _, ok := s.Last(); ok {
		t.Error("Last of an empty series is ok")
	}
	if m := s.Max(at(0)); m != 0 {
		t.Errorf("Max of an empty series = %v", m)
	}
	s.Trim(at(10))
	if len(s.Points()) != 0 {
		t.Error("Trim of an empty series made points")
	}
}

func TestSeriesAddLast(t *testing.T) {
	var s Series
	s.Add(at(0), 1)
	s.Add(at(1), 2)
	if len(s.Points()) != 2 {
		t.Errorf("series has %d points after two Adds", len(s.Points()))
	}
	last, ok := s.Last()
	if !ok || last != (Point{at(1), 2}) {
		t.Errorf("Last = %v, %v", last, ok)
	}
}

func TestSeriesTrim(t *testing.T) {
	var s Series
	for i := range 10 {
		s.Add(at(i), float64(i))
	}
	// The newest point at or before 4.5 is kept, to draw from.
	s.Trim(at(4).Add(500 * time.Millisecond))
	var got []float64
	for _, p := range s.Points() {
		got = append(got, p.V)
	}
	if want := []float64{4, 5, 6, 7, 8, 9}; !reflect.DeepEqual(got, want) {
		t.Errorf("after Trim = %v, want %v", got, want)
	}

	// A point exactly at the time is the one kept.
	s.Trim(at(6))
	if p := s.Points()[0]; p.V != 6 {
		t.Errorf("first after Trim(6) = %v, want 6", p.V)
	}

	// Trimming past every point keeps the newest.
	s.Trim(at(100))
	if len(s.Points()) != 1 || s.Points()[0].V != 9 {
		t.Errorf("after Trim past the end = %v, want just 9", s.Points())
	}

	// Trimming before every point keeps them all.
	s.Add(at(10), 10)
	s.Trim(at(0))
	if len(s.Points()) != 2 {
		t.Errorf("after Trim before the start = %v, want 2 points", s.Points())
	}
}

func TestSeriesMax(t *testing.T) {
	var s Series
	s.Add(at(0), 50)
	s.Add(at(1), 10)
	s.Add(at(2), 30)
	if m := s.Max(at(0)); m != 50 {
		t.Errorf("Max(0) = %v, want 50", m)
	}
	if m := s.Max(at(1)); m != 30 {
		t.Errorf("Max(1) = %v, want 30", m)
	}
	if m := s.Max(at(3)); m != 0 {
		t.Errorf("Max(3) = %v, want 0", m)
	}
}

func TestSeriesFill(t *testing.T) {
	now := time.Unix(1000, 0)
	var s Series
	s.Fill(now, time.Minute, 5)
	s.Add(now, 5)
	want := []Point{{now.Add(-time.Minute), 5}, {now, 5}}
	if !reflect.DeepEqual(s.Points(), want) {
		t.Errorf("Points = %v, want %v", s.Points(), want)
	}
	// A series with values is left as it is.
	s.Fill(now.Add(time.Second), time.Minute, 9)
	if !reflect.DeepEqual(s.Points(), want) {
		t.Errorf("Fill changed a series with values: %v", s.Points())
	}
}
