package tui

type Viewport struct {
	Lines  []string
	Height int
	Offset int
}

func NewViewport(lines []string, height int) Viewport {
	v := Viewport{Lines: append([]string(nil), lines...), Height: height}
	v.ScrollToBottom()
	return v
}

func (v *Viewport) SetLines(lines []string) {
	v.Lines = append([]string(nil), lines...)
	v.clamp()
}

func (v *Viewport) Scroll(delta int) {
	v.Offset += delta
	v.clamp()
}

func (v *Viewport) Page(delta int) {
	step := v.Height
	if step <= 0 {
		step = 1
	}
	v.Scroll(delta * step)
}

func (v *Viewport) HalfPage(delta int) {
	step := v.Height / 2
	if step <= 0 {
		step = 1
	}
	v.Scroll(delta * step)
}

func (v *Viewport) ScrollToTop() {
	v.Offset = 0
}

func (v *Viewport) ScrollToBottom() {
	v.Offset = len(v.Lines) - v.Height
	v.clamp()
}

func (v Viewport) Visible() []string {
	if v.Height <= 0 || len(v.Lines) == 0 {
		return nil
	}
	start := v.Offset
	end := start + v.Height
	if end > len(v.Lines) {
		end = len(v.Lines)
	}
	return append([]string(nil), v.Lines[start:end]...)
}

func (v *Viewport) clamp() {
	if v.Height <= 0 {
		v.Offset = 0
		return
	}
	maxOffset := len(v.Lines) - v.Height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.Offset < 0 {
		v.Offset = 0
	}
	if v.Offset > maxOffset {
		v.Offset = maxOffset
	}
}

type Selection struct {
	Items   []string
	Focused int
}

func NewSelection(items []string) Selection {
	return Selection{Items: append([]string(nil), items...)}
}

func (s *Selection) Move(delta int) {
	if len(s.Items) == 0 {
		s.Focused = 0
		return
	}
	s.Focused += delta
	if s.Focused < 0 {
		s.Focused = 0
	}
	if s.Focused >= len(s.Items) {
		s.Focused = len(s.Items) - 1
	}
}

func (s Selection) Current() (string, bool) {
	if len(s.Items) == 0 || s.Focused < 0 || s.Focused >= len(s.Items) {
		return "", false
	}
	return s.Items[s.Focused], true
}

func (s Selection) Render(width int, height int) []string {
	if height <= 0 {
		return nil
	}
	start := s.Focused - height/2
	if start < 0 {
		start = 0
	}
	if start+height > len(s.Items) {
		start = len(s.Items) - height
		if start < 0 {
			start = 0
		}
	}
	end := start + height
	if end > len(s.Items) {
		end = len(s.Items)
	}
	var lines []string
	for i := start; i < end; i++ {
		prefix := "  "
		if i == s.Focused {
			prefix = "> "
		}
		lines = append(lines, padOrTrim(prefix+s.Items[i], width))
	}
	for len(lines) < height {
		lines = append(lines, padOrTrim("", width))
	}
	return lines
}
