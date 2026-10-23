package field

import (
	"testing"
	"time"

	"github.com/ashleyconnor/plx/internal/model"
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

func sampleAndMovie() *model.Item {
	return &model.Item{Versions: []model.Version{
		{Duration: 150 * time.Minute, Size: 20 << 30, Resolution: "4k", Bitrate: 20000},
		{Duration: 2 * time.Minute, Size: 370 << 20, Resolution: "1080", Bitrate: 20000},
	}}
}

func TestVersionedFormat(t *testing.T) {
	it := sampleAndMovie()
	cases := map[*Field]string{
		Dur:      "2m|2h30m",
		Size:     "370.0M|20.0G",
		Res:      "1080|4k", // shortest version first, like the numbers
		Bitrate:  "20000",   // equal across versions: shown once
		Versions: "2",
	}
	for f, want := range cases {
		if got := f.Format(it); got != want {
			t.Errorf("%s = %q, want %q", f.Name, got, want)
		}
	}
	if f, ok := Lookup("dur.min"); !ok || f.Num(it) != int64(2*time.Minute) {
		t.Errorf("dur.min lookup failed")
	}
	if f, ok := Lookup("dur.max"); !ok || f != Dur {
		t.Errorf("dur.max should be the plain field")
	}
}
