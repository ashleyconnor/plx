package field

import (
	"testing"
	"time"
)

func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                               "",
		20 * time.Second:                "1m",
		57*time.Minute + 40*time.Second: "58m",
		2 * time.Hour:                   "2h",
		time.Hour + 45*time.Minute:      "1h45m",
		2*time.Hour + 5*time.Minute:     "2h05m",
		12*time.Hour + 30*time.Minute:   "12h30m",
	}
	for in, want := range cases {
		if got := HumanDuration(in); got != want {
			t.Errorf("HumanDuration(%v) = %q, want %q", in, got, want)
		}
	}
}
