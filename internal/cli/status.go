package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NorthernReach/contaigen/internal/progress"
	"github.com/spf13/cobra"
)

type statusDisplay struct {
	out         io.Writer
	palette     palette
	interactive bool
	lineWidth   int

	mu           sync.Mutex
	running      bool
	current      string
	currentSince time.Time
	lastLine     string
	lastLineAt   time.Time
	frame        int
	stop         chan struct{}
	stopped      chan struct{}
}

func runWithProgress(cmd *cobra.Command, failureMessage string, fn func(context.Context) error) error {
	display := newStatusDisplay(cmd)
	ctx := progress.WithReporter(cmd.Context(), display)
	if err := fn(ctx); err != nil {
		display.Fail(failureMessage, err)
		return err
	}
	display.Close()
	return nil
}

func runStatus(cmd *cobra.Command, message string, fn func(context.Context) error) error {
	return runWithProgress(cmd, message, func(ctx context.Context) error {
		progress.Active(ctx, message, "")
		if err := fn(ctx); err != nil {
			return err
		}
		progress.Done(ctx, message, "")
		return nil
	})
}

func newStatusDisplay(cmd *cobra.Command) *statusDisplay {
	out := cmd.ErrOrStderr()
	return &statusDisplay{
		out:         out,
		palette:     colorFor(cmd),
		interactive: isTerminal(out),
		lineWidth:   statusLineWidth(),
	}
}

func (s *statusDisplay) ReportProgress(event progress.Event) {
	text := formatProgressEvent(event)
	if text == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch event.State {
	case progress.StateDone:
		s.finishLineLocked(s.palette.green("OK"), text)
	case progress.StateInfo:
		s.writeInfoLocked(text)
	default:
		s.updateActiveLocked(text)
	}
}

func (s *statusDisplay) Fail(message string, err error) {
	text := strings.TrimSpace(message)
	if text == "" {
		text = "Operation failed"
	}
	if err != nil {
		text += " - " + err.Error()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishLineLocked(s.palette.red("ERR"), text)
}

func (s *statusDisplay) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopSpinnerLocked()
	if s.interactive && s.current != "" {
		s.clearLineLocked()
	}
	s.current = ""
}

func (s *statusDisplay) updateActiveLocked(text string) {
	now := time.Now()
	if s.current != text {
		s.current = text
		s.currentSince = now
	}
	if s.interactive {
		s.startSpinnerLocked()
		s.renderLocked()
		return
	}

	// Non-TTY output cannot update a single line, so keep it readable for logs
	// by emitting throttled progress lines instead of spinner frames.
	if text == s.lastLine && now.Sub(s.lastLineAt) < 2*time.Second {
		return
	}
	s.lastLine = text
	s.lastLineAt = now
	fmt.Fprintf(s.out, ".. %s\n", text)
}

func (s *statusDisplay) finishLineLocked(prefix string, text string) {
	if s.interactive {
		s.stopSpinnerLocked()
		s.clearLineLocked()
	}
	fmt.Fprintf(s.out, "%s %s", prefix, text)
	if !s.currentSince.IsZero() {
		fmt.Fprintf(s.out, " (%s)", shortDuration(time.Since(s.currentSince)))
	}
	fmt.Fprintln(s.out)
	s.current = ""
	s.currentSince = time.Time{}
	s.lastLine = ""
}

func (s *statusDisplay) writeInfoLocked(text string) {
	if s.interactive && s.running {
		s.clearLineLocked()
	}
	fmt.Fprintf(s.out, "%s %s\n", s.palette.cyan("info:"), text)
	if s.interactive && s.running {
		s.renderLocked()
	}
}

func (s *statusDisplay) startSpinnerLocked() {
	if s.running {
		return
	}
	s.running = true
	s.stop = make(chan struct{})
	s.stopped = make(chan struct{})
	go s.spin(s.stop, s.stopped)
}

func (s *statusDisplay) stopSpinnerLocked() {
	if !s.running {
		return
	}
	close(s.stop)
	stopped := s.stopped
	s.running = false
	s.stop = nil
	s.stopped = nil
	s.mu.Unlock()
	<-stopped
	s.mu.Lock()
}

func (s *statusDisplay) spin(stop <-chan struct{}, stopped chan<- struct{}) {
	ticker := time.NewTicker(120 * time.Millisecond)
	defer func() {
		ticker.Stop()
		close(stopped)
	}()
	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			if s.running {
				s.frame++
				s.renderLocked()
			}
			s.mu.Unlock()
		case <-stop:
			return
		}
	}
}

func (s *statusDisplay) renderLocked() {
	if s.current == "" {
		return
	}
	frames := []string{"-", "\\", "|", "/"}
	frame := frames[s.frame%len(frames)]
	elapsed := shortDuration(time.Since(s.currentSince))
	// Docker image names plus pull summaries can exceed narrow terminals. Cap
	// the message so carriage-return updates do not wrap into noisy output.
	available := s.lineWidth - len(elapsed) - 5
	if available < 12 {
		available = 12
	}
	fmt.Fprintf(s.out, "\r%s %s [%s]\x1b[K", s.palette.cyan(frame), truncate(s.current, available), elapsed)
}

func (s *statusDisplay) clearLineLocked() {
	fmt.Fprint(s.out, "\r\x1b[K")
}

func formatProgressEvent(event progress.Event) string {
	message := strings.TrimSpace(event.Message)
	detail := strings.TrimSpace(event.Detail)
	message = singleLine(message)
	detail = singleLine(detail)

	switch {
	case message == "":
		message = detail
	case detail != "":
		message += " - " + detail
	}
	if event.Total > 0 {
		message += fmt.Sprintf(" (%s/%s %d%%)", formatBytes(event.Current), formatBytes(event.Total), percent(event.Current, event.Total))
	}
	return message
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func percent(current int64, total int64) int64 {
	if total <= 0 {
		return 0
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	return current * 100 / total
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	v := float64(value)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		v /= unit
		if v < unit {
			return fmt.Sprintf("%.1f %s", v, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", v/unit)
}

func shortDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return d.Truncate(time.Second).String()
	}
	return d.Truncate(time.Second).String()
}

func statusLineWidth() int {
	const fallbackWidth = 78
	value := strings.TrimSpace(os.Getenv("COLUMNS"))
	if value == "" {
		return fallbackWidth
	}
	columns, err := strconv.Atoi(value)
	if err != nil || columns <= 0 {
		return fallbackWidth
	}
	if columns < 20 {
		return 20
	}
	if columns < 40 {
		return columns - 1
	}
	if columns > 100 {
		return 100
	}
	return columns - 1
}
