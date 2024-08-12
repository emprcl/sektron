// Packge ui handles the user interface for viewing and interacting with the
// sequencer package.
//
// It uses the bubbletea tui framework and lipgloss to make things look good.
package ui

import (
	"time"

	"sektron/filesystem"
	"sektron/sequencer"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	carousel "github.com/xaviergodart/bubble-carousel"
)

// tickMsg is a message that triggers ui rrefresh
type tickMsg time.Time

// mode represents the different modes of the ui
type mode uint8

const (
	// trackMode allows the user to select the tracks using the step keys.
	trackMode mode = iota

	// stepMode allows the user to activate/deactivate steps using the step
	// keys.
	stepMode

	// patternMode allows the user to select a specific pattern using the
	// step keys.
	patternMode

	// paramSelectMode allows the user to add new midi controls to the track.
	paramSelectMode
)

const (
	// We don't need to refresh the ui as often as the sequencer.
	// It saves some cpu. Right now we run it at 30 fps.
	refreshFrequency = 33 * time.Millisecond

	stepModeTimeout = 40
)

type mainModel struct {
	seq               sequencer.Sequencer
	parameters        parameters
	paramCarousel     carousel.Model
	paramMidiTable    table.Model
	keymap            keyMap
	width             int
	height            int
	mode              mode
	activeTrack       int
	activeTrackPage   int
	activeStep        int
	activeParams      []struct{ track, step int }
	activePatternPage int
	stepModeTimer     int
	help              help.Model
}

// New creates a new mainModel that hols the ui state. It takes a new sequencer.
// Check teh sequencer package.
func New(config filesystem.Configuration, seq sequencer.Sequencer) tea.Model {
	model := mainModel{
		seq:          seq,
		keymap:       newKeyMap(config.KeyMap),
		activeParams: make([]struct{ track, step int }, 10),
		help:         help.New(),
	}
	model.initParameters()
	model.initMidiControls()
	return model
}

func tick() tea.Cmd {
	return tea.Tick(refreshFrequency, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m mainModel) Init() tea.Cmd {
	return tea.Batch(tea.EnterAltScreen, tick())
}

func (m mainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		m.paramMidiTable.SetWidth(msg.Width)
		m.paramCarousel.SetWidth(msg.Width - lipgloss.Width(m.parameters.title) - 8)
		m.updateParams()
		return m, nil

	case tickMsg:
		if m.mode == stepMode {
			m.stepModeTimer++
		}
		if m.stepModeTimer > stepModeTimeout {
			m.stepModeTimer = 0
			m.mode = trackMode
			m.updateParams()
		}
		return m, tick()

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keymap.Play):
			m.seq.TogglePlay()
			return m, nil

		case key.Matches(msg, m.keymap.ParamMode):
			m.activeStep = 0
			if m.mode == trackMode {
				m.mode = stepMode
			} else {
				m.mode = trackMode
			}
			m.updateParams()
			return m, nil

		case key.Matches(msg, m.keymap.PatternMode):
			if m.mode == patternMode {
				m.mode = trackMode
			} else {
				m.mode = patternMode
			}
			m.updateParams()
			return m, tea.ClearScreen

		case key.Matches(msg, m.keymap.AddTrack):
			m.seq.AddTrack()
			return m, nil

		case key.Matches(msg, m.keymap.RemoveTrack):
			if m.activeTrack > 0 && m.activeTrack == len(m.seq.Tracks())-1 {
				m.activeTrack--
			}
			m.seq.RemoveTrack()
			return m, nil

		case key.Matches(msg, m.keymap.AddStep):
			m.seq.AddStep(m.activeTrack)
			return m, nil

		case key.Matches(msg, m.keymap.RemoveStep):
			remainingStepsInPage := (len(m.getActiveTrack().Steps()) - 1) % stepsPerPage
			if m.activeTrackPage > 0 && remainingStepsInPage == 0 {
				m.activeTrackPage--
			}
			m.activeStep = 0
			m.seq.RemoveStep(m.activeTrack)
			return m, nil

		case key.Matches(msg, m.keymap.PreviousStep):
			if m.mode == stepMode || m.mode == trackMode {
				newIndex := m.activeStep - 1
				if newIndex < 0 {
					newIndex = len(m.getActiveTrack().Steps()) - 1
				}

				m.activeStep = newIndex
				// Paginate if needed
				m.activeTrackPage = (newIndex / stepsPerPage)

				m.mode = stepMode
				m.stepModeTimer = 0
				m.updateParams()
			}
			return m, nil

		case key.Matches(msg, m.keymap.NextStep):
			if m.mode == stepMode || m.mode == trackMode {
				newIndex := m.activeStep + 1
				if newIndex > len(m.getActiveTrack().Steps())-1 {
					newIndex = 0
				}
				m.activeStep = newIndex
				// Paginate if needed
				m.activeTrackPage = (newIndex / stepsPerPage)

				m.mode = stepMode
				m.stepModeTimer = 0
				m.updateParams()
			}
			return m, nil

		case key.Matches(msg, m.keymap.Step):
			number := m.keymap.StepIndex[msg.String()]
			if m.mode == patternMode {
				pattern := number + (m.activePatternPage * patternsPerPage)
				if m.seq.IsPlaying() {
					m.seq.ChainNow(pattern)
				} else {
					m.seq.Save()
					m.seq.Load(pattern)
				}
				return m, nil
			}
			if number >= len(m.getActiveTrack().Steps())-(m.activeTrackPage*stepsPerPage) {
				return m, nil
			}
			m.activeStep = number + (m.activeTrackPage * stepsPerPage)
			m.mode = stepMode
			m.stepModeTimer = 0
			m.updateParams()
			return m, nil

		case key.Matches(msg, m.keymap.StepToggle):
			number := m.keymap.StepToggleIndex[msg.String()]
			if m.mode == patternMode {
				pattern := number + (m.activePatternPage * patternsPerPage)
				m.seq.Chain(pattern)
				return m, nil
			}
			if number >= len(m.getActiveTrack().Steps()) {
				return m, nil
			}
			m.activeStep = number + (m.activeTrackPage * stepsPerPage)
			m.seq.ToggleStep(m.activeTrack, m.activeStep)
			m.mode = stepMode
			m.stepModeTimer = 0
			m.updateParams()
			return m, nil

		case key.Matches(msg, m.keymap.Track):
			number := m.keymap.TrackIndex[msg.String()]
			if number >= len(m.seq.Tracks()) {
				return m, nil
			}
			m.activeTrack = number
			m.activeTrackPage = 0
			m.activeStep = 0
			m.mode = trackMode
			m.updateParams()
			return m, nil

		case key.Matches(msg, m.keymap.TrackToggle):
			number := m.keymap.TrackToggleIndex[msg.String()]
			m.seq.ToggleTrack(number)
			return m, nil

		case key.Matches(msg, m.keymap.PageUp):
			if m.mode == patternMode {
				m.activePatternPage = (m.activePatternPage + 1) % patternPages
			} else {
				pageNb := m.trackPagesNb()
				m.activeTrackPage = (m.activeTrackPage + 1) % pageNb
			}
			return m, nil

		case key.Matches(msg, m.keymap.PageDown):
			if m.mode == patternMode {
				if m.activePatternPage-1 < 0 {
					m.activePatternPage = patternPages - 1
				} else {
					m.activePatternPage = m.activePatternPage - 1
				}
			} else {
				pageNb := m.trackPagesNb()
				if m.activeTrackPage-1 < 0 {
					m.activeTrackPage = pageNb - 1
				} else {
					m.activeTrackPage = m.activeTrackPage - 1
				}
			}
			return m, nil

		case key.Matches(msg, m.keymap.TempoUp):
			m.seq.SetTempo(m.seq.Tempo() + 1)
			return m, nil

		case key.Matches(msg, m.keymap.TempoDown):
			m.seq.SetTempo(m.seq.Tempo() - 1)
			return m, nil

		case key.Matches(msg, m.keymap.AddParam):
			m.mode = paramSelectMode
			m.updateParams()
			return m, nil

		case key.Matches(msg, m.keymap.RemoveParam):
			m.mode = trackMode
			m.updateParams()
			nb := m.parameters.getParamIndex(m.getActiveParam()) - m.parameters.fixedParamNb
			if nb >= 0 {
				m.getActiveTrack().RemoveControl(nb)
				m.previousParam()
			}
			return m, nil

		case key.Matches(msg, m.keymap.Validate):
			if m.mode == paramSelectMode {
				m.getActiveTrack().AddControl(m.paramMidiTable.Cursor())
				m.paramMidiTable.SetCursor(0)
				m.mode = trackMode
				m.updateParams()
			}
			if m.mode == stepMode {
				m.seq.ToggleStep(m.activeTrack, m.activeStep)
			}
			return m, nil

		case key.Matches(msg, m.keymap.Left):
			if m.mode == paramSelectMode {
				m.mode = trackMode
				return m, nil
			}
			m.previousParam()
			m.stepModeTimer = 0
			return m, nil

		case key.Matches(msg, m.keymap.Right):
			m.stepModeTimer = 0
			m.nextParam()
			return m, nil

		case key.Matches(msg, m.keymap.Up):
			if m.mode == stepMode && m.getActiveStep().IsActive() {
				m.parameters.getStepParam(m.getActiveParam()).increase(m.getActiveStep())
			} else if m.mode == trackMode {
				m.parameters.getTrackParam(m.getActiveParam()).increase(m.getActiveTrack())
			} else if m.mode == paramSelectMode {
				var cmd tea.Cmd
				m.paramMidiTable, cmd = m.paramMidiTable.Update(msg)
				m.updateParams()
				return m, cmd
			}
			m.stepModeTimer = 0
			m.updateParams()
			return m, nil

		case key.Matches(msg, m.keymap.Down):
			if m.mode == stepMode && m.getActiveStep().IsActive() {
				m.parameters.getStepParam(m.getActiveParam()).decrease(m.getActiveStep())
			} else if m.mode == trackMode {
				m.parameters.getTrackParam(m.getActiveParam()).decrease(m.getActiveTrack())
			} else if m.mode == paramSelectMode {
				var cmd tea.Cmd
				m.paramMidiTable, cmd = m.paramMidiTable.Update(msg)
				m.updateParams()
				return m, cmd
			}
			m.stepModeTimer = 0
			m.updateParams()
			return m, nil

		case key.Matches(msg, m.keymap.Help):
			m.help.ShowAll = !m.help.ShowAll
			return m, tea.ClearScreen

		case key.Matches(msg, m.keymap.Quit):
			if m.seq.IsPlaying() {
				m.seq.TogglePlay()
			}
			m.seq.Reset()
			m.seq.Save()
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m mainModel) View() string {
	mainView := lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderTransport(),
		m.renderSequencer(),
		m.renderParams(),
	)

	help := m.help.View(m.keymap)

	// Cleanup gibber
	cleanup := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height - lipgloss.Height(mainView) - lipgloss.Height(help)).
		Render("")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		mainView,
		cleanup,
		help,
	)
}

func (m *mainModel) getActiveTrack() sequencer.Track {
	return m.seq.Tracks()[m.activeTrack]
}

func (m *mainModel) getActiveStep() sequencer.Step {
	return m.seq.Tracks()[m.activeTrack].Steps()[m.activeStep]
}

func (m mainModel) getActiveParam() int {
	if m.mode == stepMode {
		return m.activeParams[m.activeTrack].step
	}
	return m.activeParams[m.activeTrack].track
}

func (m *mainModel) nextParam() {
	m.paramCarousel.MoveRight()
	if m.mode == stepMode {
		m.activeParams[m.activeTrack].step = m.paramCarousel.Cursor()
	} else {
		m.activeParams[m.activeTrack].track = m.paramCarousel.Cursor()
	}
	m.updateParams()
}

func (m *mainModel) previousParam() {
	m.paramCarousel.MoveLeft()
	if m.mode == stepMode {
		m.activeParams[m.activeTrack].step = m.paramCarousel.Cursor()
	} else {
		m.activeParams[m.activeTrack].track = m.paramCarousel.Cursor()
	}
	m.updateParams()
}
