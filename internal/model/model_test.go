package model

import (
	"testing"
	"time"

	"github.com/ashleyconnor/plx/internal/plex"
)

func TestSampleListedFirstDoesNotSetDuration(t *testing.T) {
	m := &plex.Metadata{RatingKey: "1", Type: "movie", Duration: 60000, Media: []plex.Media{
		{ID: 10, Duration: 60000, VideoResolution: "4k", Part: []plex.Part{{Size: 300, File: "/m/Sample.mkv"}}},
		{ID: 11, Duration: 6000000, VideoResolution: "1080", Bitrate: 9000, Part: []plex.Part{{Size: 20000, File: "/m/Movie.mkv"}}},
	}}
	it := FromPlex(m)
	if it.Duration != 100*time.Minute || it.Resolution != "1080" || it.Bitrate != 9000 {
		t.Errorf("duration %v res %s bitrate %d: want the long version's", it.Duration, it.Resolution, it.Bitrate)
	}
	if len(it.Versions) != 2 || it.Versions[0].MediaID != "10" || it.Size != 20300 {
		t.Errorf("versions %+v size %d", it.Versions, it.Size)
	}
}
