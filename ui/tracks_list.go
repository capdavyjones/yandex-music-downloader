package ui

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"ya-music/ya/model"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

var (
	selectedItemStyle = lipgloss.NewStyle().Foreground(primaryTextColor).Background(focusSurfaceColor)
	emptyItemStyle    = lipgloss.NewStyle()

	// Unselected styles
	trackNumberStyle = lipgloss.NewStyle().PaddingLeft(4)
	descriptionStyle = lipgloss.NewStyle().Foreground(mutedColor)
	albumHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)

	// Selected styles
	selectedTrackNumberStyle      = lipgloss.NewStyle().Foreground(accentColor).PaddingLeft(2).Background(focusSurfaceColor).Bold(true)
	selectedTrackDescriptionStyle = lipgloss.NewStyle().Foreground(secondaryTextColor).Background(focusSurfaceColor)
	selectedAlbumHeaderStyle      = lipgloss.NewStyle().Bold(true).Foreground(primaryTextColor).Background(focusSurfaceColor)

	// Status styles for unselected items
	readyStatusStyle         = greenForeground
	duplicateStatusStyle     = grayForeground
	downloadedStatusStyle    = greenForeground
	errorStatusStyle         = redForeground
	notAvailableStatusStyle  = redForeground
	alreadyExistsStatusStyle = greenForeground
)

type TrackStatus int

const (
	TrackStatusReady TrackStatus = iota
	TrackStatusDownloaded
	TrackStatusDownloading
	TrackStatusError
	TrackStatusNotAvailable
	TrackStatusDuplicate
	TrackStatusAlreadyExists
)

func (t TrackStatus) String() string {
	switch t {
	case TrackStatusDuplicate:
		return "Duplicate"
	case TrackStatusDownloading:
		return "● Downloading..."
	case TrackStatusDownloaded:
		return "✓"
	case TrackStatusError:
		return "Error"
	case TrackStatusReady:
		return "Ready"
	case TrackStatusNotAvailable:
		return "Not Available"
	case TrackStatusAlreadyExists:
		return "Already Exists"
	default:
		return "Unknown"
	}
}

type ListSelectedItemMsg string

type TrackListItem struct {
	uid               string
	track             *model.Track
	status            TrackStatus
	format            string
	trackNumberDigits int
	numberLabel       string
}

type AlbumHeaderItem struct {
	label string
	title string
	year  int
}

func (t TrackListItem) FilterValue() string {
	if t.track == nil {
		return ""
	}
	title := t.track.FullTitle()
	if album := albumTitleOf(t.track); album != "" {
		return strings.TrimSpace(title + " " + album)
	}
	return title
}

func (t TrackListItem) Title() string {
	if t.track == nil {
		return ""
	}
	return t.track.FullTitle()
}

func (t TrackListItem) Description() string {
	if t.track == nil {
		return ""
	}
	return t.track.ArtistsString()
}

func (a AlbumHeaderItem) FilterValue() string {
	return strings.TrimSpace(a.title + " " + a.Title())
}

func (a AlbumHeaderItem) Title() string {
	if strings.TrimSpace(a.label) != "" {
		return a.label
	}
	return a.title
}

func (a AlbumHeaderItem) Description() string {
	return ""
}

func (t TrackListItem) Height() int  { return 1 }
func (t TrackListItem) Spacing() int { return 0 }
func (t TrackListItem) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	if m.SelectedItem() == nil {
		return nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k", "down", "j", "left", "right", "l", "h", "g", "G", "end", "home", "pgup", "pgdn":
			return selectedListItemMsg(m.SelectedItem())
		}
	}

	return nil
}

func selectedListItemMsg(item list.Item) tea.Cmd {
	switch item := item.(type) {
	case TrackListItem:
		return func() tea.Msg {
			return ListSelectedItemMsg(item.uid)
		}
	case AlbumHeaderItem:
		return func() tea.Msg {
			return ListSelectedItemMsg(item.Title())
		}
	default:
		return nil
	}
}

func (t TrackListItem) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	switch item := listItem.(type) {
	case AlbumHeaderItem:
		renderAlbumHeader(w, m, index, item)
		return
	case TrackListItem:
		t.renderTrack(w, m, index, item)
	}
}

func renderAlbumHeader(w io.Writer, m list.Model, index int, item AlbumHeaderItem) {
	isSelected := index == m.Index()
	prefix := "  "
	style := albumHeaderStyle
	if isSelected {
		prefix = "> "
		style = selectedAlbumHeaderStyle
	}

	width := m.Width()
	if width < 0 {
		width = 0
	}
	label := truncateToWidth(item.Title(), max(0, width-runewidth.StringWidth(prefix)))
	line := padToWidth(prefix+label, width)
	fmt.Fprint(w, style.Render(line))
}

func (t TrackListItem) renderTrack(w io.Writer, m list.Model, index int, item TrackListItem) {
	title := item.Title()
	desc := item.Description()

	statusPlain := formatStatusColumn(item.statusLabel())
	statusStr := statusPlain

	switch item.status {
	case TrackStatusDuplicate:
		statusStr = duplicateStatusStyle.Render(statusPlain)
	case TrackStatusDownloaded:
		statusStr = downloadedStatusStyle.Render(statusPlain)
	case TrackStatusError:
		statusStr = errorStatusStyle.Render(statusPlain)
	case TrackStatusNotAvailable:
		statusStr = notAvailableStatusStyle.Render(statusPlain)
	case TrackStatusAlreadyExists:
		statusStr = alreadyExistsStatusStyle.Render(statusPlain)
	case TrackStatusReady:
		statusStr = readyStatusStyle.Render(statusPlain)
	}

	isSelected := index == m.Index()

	trackNumber := item.numberLabel
	if trackNumber == "" {
		trackNumberDigits := item.trackNumberDigits
		if trackNumberDigits <= 0 {
			trackNumberDigits = trackNumberColumnDigits(len(m.Items()))
		}
		trackNumber = formatTrackNumber(index, trackNumberDigits)
	}
	trackNumberStyleToUse := trackNumberStyle
	if isSelected {
		trackNumberStyleToUse = selectedTrackNumberStyle
		trackNumber = "> " + trackNumber
	}

	titleStyleToUse := emptyItemStyle
	paddingStyleToUse := emptyItemStyle
	descStyleToUse := descriptionStyle

	if isSelected {
		titleStyleToUse = selectedItemStyle
		paddingStyleToUse = selectedItemStyle
		descStyleToUse = selectedTrackDescriptionStyle
		statusStr = selectedItemStyle.Render(statusPlain)
	}

	trackNumberRendered := trackNumberStyleToUse.Render(trackNumber)
	numberWidth := lipgloss.Width(trackNumberRendered)
	statusWidth := trackStatusColumnWidth

	textWidth := m.Width() - numberWidth - statusWidth
	if textWidth < 0 {
		textWidth = 0
	}

	combined := trackText(title, desc)
	displayText := truncateToWidth(combined, textWidth)
	titlePart, descPart := splitTrackText(displayText, title)
	paddingWidth := m.Width() - numberWidth - lipgloss.Width(displayText) - statusWidth
	if paddingWidth < 0 {
		paddingWidth = 0
	}
	padding := strings.Repeat(" ", paddingWidth)

	str := fmt.Sprintf("%s%s%s%s%s",
		trackNumberRendered,
		titleStyleToUse.Render(titlePart),
		descStyleToUse.Render(descPart),
		paddingStyleToUse.Render(padding),
		statusStr,
	)

	fmt.Fprint(w, str)
}

const trackStatusColumnWidth = 16

func formatStatusColumn(label string) string {
	return padToWidth(truncateToWidth(label, trackStatusColumnWidth), trackStatusColumnWidth)
}

func padToWidth(s string, width int) string {
	current := runewidth.StringWidth(s)
	if current >= width {
		return s
	}
	return s + strings.Repeat(" ", width-current)
}

func trackText(title, desc string) string {
	if desc == "" {
		return title
	}
	return fmt.Sprintf("%s %s", title, desc)
}

func splitTrackText(displayText, title string) (string, string) {
	titleWidth := runewidth.StringWidth(title)
	currentWidth := 0

	for i, r := range displayText {
		if currentWidth >= titleWidth && r == ' ' {
			return displayText[:i+1], displayText[i+1:]
		}

		currentWidth += runewidth.RuneWidth(r)
	}

	return displayText, ""
}

func truncateToWidth(s string, width int) string {
	if runewidth.StringWidth(s) <= width {
		return s
	}

	dots := "..."
	dotsWidth := runewidth.StringWidth(dots)
	if width <= dotsWidth {
		return strings.Repeat(".", width)
	}

	var b strings.Builder
	currentWidth := 0
	for _, r := range s {
		runeWidth := runewidth.RuneWidth(r)
		if currentWidth+runeWidth > width-dotsWidth {
			break
		}
		b.WriteRune(r)
		currentWidth += runeWidth
	}
	b.WriteString(dots)
	return b.String()
}

func (t TrackListItem) statusLabel() string {
	if t.status != TrackStatusDownloaded {
		return t.status.String()
	}
	format := strings.TrimSpace(t.format)
	if format == "" {
		format = "MP3"
	}
	return t.status.String() + " " + format
}

func trackNumberColumnDigits(totalItems int) int {
	return max(2, len(strconv.Itoa(totalItems)))
}

func formatTrackNumber(index int, numberDigits int) string {
	numberDigits = max(2, numberDigits)
	number := fmt.Sprintf("%02d. ", index+1)
	columnWidth := numberDigits + 2
	if width := runewidth.StringWidth(number); width < columnWidth {
		number = strings.Repeat(" ", columnWidth-width) + number
	}
	return number
}

func albumTitleOf(track *model.Track) string {
	if track == nil || len(track.Albums) == 0 {
		return ""
	}
	return strings.TrimSpace(track.Albums[0].Title)
}

func albumTrackNumberLabel(track *model.Track, fallback int) string {
	index := fallback
	volume := 0
	if track != nil && len(track.Albums) > 0 {
		pos := track.Albums[0].TrackPosition
		if pos.Index > 0 {
			index = pos.Index
		}
		volume = pos.Volume
	}
	if index <= 0 {
		index = fallback
	}
	if volume > 1 {
		return fmt.Sprintf("%d.%02d. ", volume, index)
	}
	return fmt.Sprintf("%02d. ", index)
}

func padTrackNumberLabels(items []list.Item) {
	maxWidth := 0
	for _, item := range items {
		track, ok := item.(TrackListItem)
		if !ok || track.numberLabel == "" {
			continue
		}
		if width := runewidth.StringWidth(track.numberLabel); width > maxWidth {
			maxWidth = width
		}
	}
	if maxWidth == 0 {
		return
	}
	for i, item := range items {
		track, ok := item.(TrackListItem)
		if !ok || track.numberLabel == "" {
			continue
		}
		track.numberLabel = padToWidth(track.numberLabel, maxWidth)
		items[i] = track
	}
}
