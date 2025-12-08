package render

import (
	"strings"
	"time"
)

// SpinnerStyle defines different spinner animation styles.
type SpinnerStyle int

const (
	// SpinnerBraille uses smooth braille dot animation
	SpinnerBraille SpinnerStyle = iota
	// SpinnerDots uses growing dots animation
	SpinnerDots
	// SpinnerPulse uses a pulsing bar animation
	SpinnerPulse
	// SpinnerGlobe uses a rotating globe (box drawing)
	SpinnerGlobe
	// SpinnerWave uses a wave animation
	SpinnerWave
)

// Spinner provides animated loading indicators.
type Spinner struct {
	style    SpinnerStyle
	frame    int
	lastTick time.Time
	interval time.Duration
}

// NewSpinner creates a new spinner with the given style.
func NewSpinner(style SpinnerStyle) *Spinner {
	return &Spinner{
		style:    style,
		frame:    0,
		lastTick: time.Now(),
		interval: 80 * time.Millisecond,
	}
}

// Tick advances the spinner animation if enough time has passed.
// Returns true if the frame changed.
func (s *Spinner) Tick() bool {
	now := time.Now()
	if now.Sub(s.lastTick) >= s.interval {
		s.frame++
		s.lastTick = now
		return true
	}
	return false
}

// Reset resets the spinner to its initial state.
func (s *Spinner) Reset() {
	s.frame = 0
	s.lastTick = time.Now()
}

// Frame returns the current animation frame string.
func (s *Spinner) Frame() string {
	frames := s.frames()
	return frames[s.frame%len(frames)]
}

// Width returns the display width of the spinner.
func (s *Spinner) Width() int {
	return StringWidth(s.Frame())
}

func (s *Spinner) frames() []string {
	switch s.style {
	case SpinnerBraille:
		return []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	case SpinnerDots:
		return []string{"   ", ".  ", ".. ", "...", " ..", "  .", "   "}
	case SpinnerPulse:
		return []string{
			"[      ]",
			"[=     ]",
			"[==    ]",
			"[===   ]",
			"[====  ]",
			"[===== ]",
			"[======]",
			"[ =====]",
			"[  ====]",
			"[   ===]",
			"[    ==]",
			"[     =]",
		}
	case SpinnerGlobe:
		// Globe using box-drawing inspired characters
		return []string{"◐", "◓", "◑", "◒"}
	case SpinnerWave:
		return []string{
			"▁▂▃▄▅▆▇█",
			"▂▃▄▅▆▇█▇",
			"▃▄▅▆▇█▇▆",
			"▄▅▆▇█▇▆▅",
			"▅▆▇█▇▆▅▄",
			"▆▇█▇▆▅▄▃",
			"▇█▇▆▅▄▃▂",
			"█▇▆▅▄▃▂▁",
			"▇▆▅▄▃▂▁▂",
			"▆▅▄▃▂▁▂▃",
			"▅▄▃▂▁▂▃▄",
			"▄▃▂▁▂▃▄▅",
			"▃▂▁▂▃▄▅▆",
			"▂▁▂▃▄▅▆▇",
			"▁▂▃▄▅▆▇█",
		}
	default:
		return []string{"|", "/", "-", "\\"}
	}
}

// LoadingDisplay provides a complete loading indicator with spinner and message.
type LoadingDisplay struct {
	spinner *Spinner
	message string
}

// NewLoadingDisplay creates a new loading display.
func NewLoadingDisplay(style SpinnerStyle, message string) *LoadingDisplay {
	return &LoadingDisplay{
		spinner: NewSpinner(style),
		message: message,
	}
}

// Tick advances the animation.
func (ld *LoadingDisplay) Tick() bool {
	return ld.spinner.Tick()
}

// Draw renders the loading display centered on the canvas.
func (ld *LoadingDisplay) Draw(c *Canvas) {
	width := c.Width()
	height := c.Height()

	spinnerFrame := ld.spinner.Frame()

	// Format: spinner + space + message
	fullText := spinnerFrame + " " + ld.message
	textWidth := StringWidth(fullText)

	x := (width - textWidth) / 2
	y := height / 2

	// Draw spinner with bold style
	spinnerWidth := ld.spinner.Width()
	c.WriteString(x, y, spinnerFrame, Style{Bold: true, FgColor: ColorCyan})

	// Draw message
	c.WriteString(x+spinnerWidth+1, y, ld.message, Style{Dim: true})
}

// DrawBox renders the loading display in a centered box.
func (ld *LoadingDisplay) DrawBox(c *Canvas, title string) {
	ld.DrawBoxStyled(c, title, Style{Bold: true, FgColor: ColorCyan})
}

// DrawBoxStyled renders the loading display in a centered box with custom spinner style.
func (ld *LoadingDisplay) DrawBoxStyled(c *Canvas, title string, spinnerStyle Style) {
	width := c.Width()
	height := c.Height()

	spinnerFrame := ld.spinner.Frame()
	fullText := spinnerFrame + " " + ld.message
	textWidth := StringWidth(fullText)

	// Box dimensions
	boxWidth := textWidth + 6
	if boxWidth < 30 {
		boxWidth = 30
	}
	boxHeight := 5

	startX := (width - boxWidth) / 2
	startY := (height - boxHeight) / 2

	// Clear box area
	for y := startY; y < startY+boxHeight; y++ {
		for x := startX; x < startX+boxWidth; x++ {
			c.Set(x, y, ' ', Style{})
		}
	}

	// Draw border with rounded corners for a softer feel
	c.DrawBox(startX, startY, boxWidth, boxHeight, RoundedBox, Style{})

	// Draw title if provided
	if title != "" {
		titleWidth := StringWidth(title) + 2
		titleX := startX + (boxWidth-titleWidth)/2
		c.Set(titleX, startY, ' ', Style{})
		c.WriteString(titleX+1, startY, title, Style{Bold: true})
		c.Set(titleX+1+StringWidth(title), startY, ' ', Style{})
	}

	// Draw spinner and message centered in box
	contentX := startX + (boxWidth-textWidth)/2
	contentY := startY + 2

	spinnerWidth := ld.spinner.Width()
	c.WriteString(contentX, contentY, spinnerFrame, spinnerStyle)
	c.WriteString(contentX+spinnerWidth+1, contentY, ld.message, Style{})
}

// BrowseSpinner creates our signature "browse" spinner.
// It uses a wave animation that feels like browsing through content.
func BrowseSpinner() *Spinner {
	s := NewSpinner(SpinnerWave)
	s.interval = 60 * time.Millisecond // Slightly faster for fluid feel
	return s
}

// BrowseLoadingDisplay creates the standard loading display for Browse.
func BrowseLoadingDisplay(url string) *LoadingDisplay {
	// Truncate URL if too long
	displayURL := url
	if len(displayURL) > 40 {
		displayURL = displayURL[:37] + "..."
	}

	ld := &LoadingDisplay{
		spinner: BrowseSpinner(),
		message: displayURL,
	}
	return ld
}

// QuickSpinner returns spinner frames for use in simple loops.
// Call with incrementing index to animate.
func QuickSpinner(index int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[index%len(frames)]
}

// DrawLoadingBar draws a progress bar style loading indicator.
func DrawLoadingBar(c *Canvas, x, y, width int, progress float64, style Style) {
	if width < 3 {
		return
	}

	innerWidth := width - 2
	filled := int(float64(innerWidth) * progress)
	if filled > innerWidth {
		filled = innerWidth
	}

	c.Set(x, y, '[', style)
	for i := 0; i < innerWidth; i++ {
		if i < filled {
			c.Set(x+1+i, y, '█', style)
		} else {
			c.Set(x+1+i, y, '░', style)
		}
	}
	c.Set(x+width-1, y, ']', style)
}

// AnimatedLoadingBar creates an animated loading bar for indeterminate progress.
func AnimatedLoadingBar(c *Canvas, x, y, width, frame int, style Style) {
	if width < 5 {
		return
	}

	innerWidth := width - 2
	barWidth := innerWidth / 3
	if barWidth < 2 {
		barWidth = 2
	}

	// Calculate position of the moving bar
	totalPositions := innerWidth - barWidth + 1
	pos := frame % (totalPositions * 2)
	if pos >= totalPositions {
		pos = totalPositions*2 - pos - 1 // Bounce back
	}

	c.Set(x, y, '[', style)
	for i := 0; i < innerWidth; i++ {
		if i >= pos && i < pos+barWidth {
			c.Set(x+1+i, y, '▓', style)
		} else {
			c.Set(x+1+i, y, '░', style)
		}
	}
	c.Set(x+width-1, y, ']', style)
}

// CompactSpinner returns a minimal spinner suitable for status bars.
func CompactSpinner(frame int) string {
	frames := []string{"◐", "◓", "◑", "◒"}
	return frames[frame%len(frames)]
}

// PulseText creates a text with pulsing dots animation.
func PulseText(base string, frame int) string {
	dots := frame % 4
	return base + strings.Repeat(".", dots) + strings.Repeat(" ", 3-dots)
}
