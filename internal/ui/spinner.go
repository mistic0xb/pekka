package ui

import (
	"fmt"
	"time"

	"github.com/briandowns/spinner"
)

type Spinner struct {
	spinner *spinner.Spinner
}

func NewSpinner(msg string) *Spinner {
	s := spinner.New(spinner.CharSets[11], 100*time.Millisecond)
	s.Start()
	s.Color("blue", "bold")
	s.Suffix = fmt.Sprintf(" %s\n", msg)

	return &Spinner{spinner: s}
}

func (s *Spinner) Stop() {
	s.spinner.Stop()
}
