package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plex"
)

// Poster size in cells; half-block rendering gives 2 pixels per row.
const posterCols, posterRows = 24, 18

// info is the item detail overlay. Full metadata and the poster load
// asynchronously; the list item is shown until they arrive.
type info struct {
	item       *model.Item
	meta       *plex.Metadata
	err        error
	poster     string
	posterDone bool
	scroll     int
}

type infoMsg struct {
	key  string
	meta *plex.Metadata
	err  error
}

type posterMsg struct {
	key string
	art string
}

func (m *Model) openInfo(it *model.Item) tea.Cmd {
	m.info, m.mode = &info{item: it}, modeInfo
	meta := func() tea.Msg {
		meta, err := m.cat.C.Metadata(m.ctx, it.Key)
		return infoMsg{key: it.Key, meta: meta, err: err}
	}
	if it.MembershipKnown {
		return meta
	}
	return tea.Batch(meta, m.ensureMembership(m.top(), true))
}

// posterPath picks the most recognisable artwork: the show poster for
// episodes, the album cover for tracks.
func posterPath(md *plex.Metadata) string {
	candidates := []string{md.Thumb}
	switch md.Type {
	case "episode":
		candidates = []string{md.GrandparentThumb, md.ParentThumb, md.Thumb}
	case "track":
		candidates = []string{md.ParentThumb, md.GrandparentThumb, md.Thumb}
	}
	for _, p := range candidates {
		if p != "" {
			return p
		}
	}
	return ""
}

func (m *Model) loadPoster(md *plex.Metadata) tea.Cmd {
	path := posterPath(md)
	if path == "" {
		m.info.posterDone = true
		return nil
	}
	key := string(md.RatingKey)
	return func() tea.Msg {
		// Ask for 4× the cell resolution so the box filter has detail to average.
		data, err := m.cat.C.Image(m.ctx, path, posterCols*4, posterRows*8)
		if err != nil {
			return posterMsg{key: key}
		}
		art, err := halfBlocks(data, posterCols, posterRows)
		if err != nil {
			return posterMsg{key: key}
		}
		return posterMsg{key: key, art: art}
	}
}

func (m *Model) infoKey(msg tea.KeyMsg) tea.Cmd {
	in := m.info
	switch msg.String() {
	case "esc", "q", "enter", "h", "left", "i":
		m.info, m.mode = nil, modeNormal
	case "j", "down":
		in.scroll++
	case "k", "up":
		in.scroll = max(0, in.scroll-1)
	case "g":
		in.scroll = 0
	case " ":
		m.setSelected(in.item, !m.isSelected(in.item))
	case "x", ";":
		m.info, m.mode = nil, modeNormal
		return m.openMenu()
	}
	return nil
}

func (m *Model) infoView() string {
	in := m.info
	it := in.item
	boxW := min(m.w-4, 120)
	textW := boxW - posterCols - 3 - 4
	if m.w < 70 { // too narrow for the poster
		textW = boxW - 4
	}
	lines := m.infoLines(textW)
	bodyH := max(posterRows, min(len(lines), m.h-8))
	in.scroll = min(in.scroll, max(0, len(lines)-bodyH))
	shown := lines[in.scroll:min(len(lines), in.scroll+bodyH)]
	text := strings.Join(shown, "\n")

	var body string
	if m.w < 70 {
		body = text
	} else {
		poster := in.poster
		if poster == "" {
			msg := "no artwork"
			if !in.posterDone {
				msg = m.spinner.View() + " loading"
			}
			poster = lipgloss.Place(posterCols, posterRows, lipgloss.Center, lipgloss.Center, stDim.Render(msg))
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, poster, "   ", text)
	}

	sel := "select"
	if m.isSelected(it) {
		sel = "unselect"
	}
	footer := renderHints([]hint{{"j/k", "scroll"}, {"space", sel}, {"x", "actions"}, {"esc", "close"}}, boxW-4)
	return stBox.Width(boxW).Render(body + "\n\n" + footer)
}

// infoLines renders the details as wrapped lines for scrolling.
func (m *Model) infoLines(w int) []string {
	in := m.info
	it := in.item
	md := in.meta
	if md == nil {
		md = it.Raw
	}
	var b strings.Builder
	wrap := lipgloss.NewStyle().Width(w)
	line := func(s string) { b.WriteString(wrap.Render(s) + "\n") }
	kv := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			line(stDim.Render(fmt.Sprintf("%-11s", k)) + " " + v)
		}
	}
	tags := func(ts []plex.Tag, n int) string {
		var out []string
		for i, t := range ts {
			if n > 0 && i == n {
				out = append(out, fmt.Sprintf("+%d more", len(ts)-n))
				break
			}
			out = append(out, t.Tag)
		}
		return strings.Join(out, ", ")
	}

	title := it.Title
	if it.Year > 0 {
		title += fmt.Sprintf(" (%d)", it.Year)
	}
	line(cell(icons.kind(it.Kind)) + stBold.Render(title))
	if it.Kind == "episode" {
		line(stDim.Render(fmt.Sprintf("%s · S%02dE%02d · %s", it.Show, it.SeasonNum, it.Index, it.Season)))
	} else if it.Show != "" {
		line(stDim.Render(it.Show))
	}
	if md.Tagline != "" {
		line(lipgloss.NewStyle().Italic(true).Render(md.Tagline))
	}
	if m.isSelected(it) {
		line(stSel.Render("● selected"))
	}
	if in.err != nil {
		line(stErr.Render("couldn't load full details: " + in.err.Error()))
	}
	b.WriteString("\n")

	kv("library", it.SectionTitle+" · "+string(it.Kind))
	kv("released", md.OriginallyAvailableAt)
	kv("runtime", field.HumanDuration(it.Duration))
	var ratings []string
	if md.AudienceRating > 0 {
		ratings = append(ratings, fmt.Sprintf("★ %.1f audience", float64(md.AudienceRating)))
	}
	if md.Rating > 0 {
		ratings = append(ratings, fmt.Sprintf("%.1f critics", float64(md.Rating)))
	}
	if md.UserRating > 0 {
		ratings = append(ratings, fmt.Sprintf("%.1f yours", float64(md.UserRating)))
	}
	if md.ContentRating != "" {
		ratings = append(ratings, md.ContentRating)
	}
	kv("rating", strings.Join(ratings, " · "))
	kv("studio", md.Studio)
	kv("genres", tags(md.Genre, 0))
	kv("director", tags(md.Director, 3))
	kv("writer", tags(md.Writer, 3))
	kv("cast", tags(md.Role, 6))
	kv("country", tags(md.Country, 0))
	labels := strings.Join(it.Labels, ", ")
	if md != it.Raw && len(md.Label) > 0 {
		labels = tags(md.Label, 0)
	}
	kv("labels", labels)
	switch {
	case it.MembershipKnown:
		kv("collections", strings.Join(it.Collections, ", "))
		pl := strings.Join(it.Playlists, ", ")
		if pl == "" {
			pl = stDim.Render("none")
		}
		kv("playlists", pl)
	default:
		kv("collections", tags(md.Collection, 0))
		kv("playlists", stDim.Render(m.spinner.View()+" indexing…"))
	}
	b.WriteString("\n")

	watch := "unwatched"
	if it.ViewCount > 0 {
		watch = fmt.Sprintf("%s, last %s", plural(it.ViewCount, "play"), dateOr(it.LastViewed, "?"))
	}
	if it.ViewOffset > 0 && it.Duration > 0 {
		watch += fmt.Sprintf(" · %d%% in progress", int(100*it.ViewOffset/it.Duration))
	}
	kv("watched", watch)
	kv("added", dateOr(it.Added, ""))
	if md.Summary != "" {
		b.WriteString("\n")
		line(md.Summary)
	}

	for i, media := range md.Media {
		b.WriteString("\n")
		head := fmt.Sprintf("version %d", i+1)
		if len(md.Media) == 1 {
			head = "media"
		}
		var spec []string
		for _, s := range []string{media.VideoResolution, media.VideoCodec, media.Container, media.VideoFrameRate} {
			if s != "" {
				spec = append(spec, s)
			}
		}
		if media.Bitrate > 0 {
			spec = append(spec, fmt.Sprintf("%.1f Mbps", float64(media.Bitrate)/1000))
		}
		if media.AudioChannels > 0 {
			spec = append(spec, fmt.Sprintf("%d ch", media.AudioChannels))
		}
		line(stBold.Render(head) + "  " + strings.Join(spec, " · "))
		for _, p := range media.Part {
			line("  " + p.File + stDim.Render("  "+field.HumanBytes(int64(p.Size))))
			var audio, subs []string
			for _, s := range p.Stream {
				switch s.StreamType {
				case plex.StreamAudio:
					audio = append(audio, s.DisplayTitle)
				case plex.StreamSubtitle:
					subs = append(subs, s.DisplayTitle)
				}
			}
			if len(audio) > 0 {
				line("  " + stDim.Render("audio ") + strings.Join(audio, ", "))
			}
			if len(subs) > 0 {
				line("  " + stDim.Render("subs  ") + strings.Join(subs, ", "))
			}
		}
	}
	b.WriteString("\n")
	line(stDim.Render(fmt.Sprintf("ratingKey %s  %s", it.Key, md.Guid)))
	if md.UpdatedAt > 0 {
		line(stDim.Render("updated " + time.Unix(int64(md.UpdatedAt), 0).Format("2006-01-02")))
	}
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}
