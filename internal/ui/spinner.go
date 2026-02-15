package ui

import (
	"fmt"
	"time"

	"github.com/briandowns/spinner"
)

type Spinner struct {
	spinner *spinner.Spinner
}

// charset == 0 (no value passed) uses default spinner charset
func NewSpinner(msg string, charset int, color string) *Spinner {
	s := spinner.New(spinner.CharSets[charset], 100*time.Millisecond)
	s.Color(color, "bold")
	s.Suffix = fmt.Sprintf(" %s\n\n", msg)

	s.Start()
	return &Spinner{spinner: s}
}

func (s *Spinner) Stop() {
	s.spinner.Stop()
}
