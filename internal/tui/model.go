package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lodibrahim/logpond/internal/config"
	"github.com/lodibrahim/logpond/internal/parser"
	"github.com/lodibrahim/logpond/internal/search"
	"github.com/lodibrahim/logpond/internal/store"
)

// NewEntryMsg signals new log entries arrived.
type NewEntryMsg struct{}

// InputClosedMsg signals stdin has closed.
type InputClosedMsg struct{}

// clearStatusMsg clears the temporary status message.
type clearStatusMsg struct{}

// Model is the bubbletea TUI model.
type Model struct {
	cfg    *config.Config
	parser *parser.Parser
	store  *store.Store

	// View state
	width, height int
	offset   int // scroll offset from bottom
	atBottom bool

	// Filter
	filterMode  bool
	filterInput string
	filterQuery *search.Query
	filtered    []*parser.Entry

	// Status
	inputClosed bool
	lastCount   int
	statusMsg   string
}

// New creates a new TUI model.
func New(cfg *config.Config, p *parser.Parser, s *store.Store) *Model {
	return &Model{
		cfg:       cfg,
		parser:    p,
		store:     s,
		atBottom: true,
	}
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case NewEntryMsg:
		m.refreshEntries()
		return m, nil

	case InputClosedMsg:
		m.inputClosed = true
		return m, nil

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}

	return m, nil
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.scrollUp()
	case tea.MouseButtonWheelDown:
		m.scrollDown()
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filterMode {
		return m.handleFilterKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.scrollDown()
	case "k", "up":
		m.scrollUp()
	case "G":
		m.scrollToBottom()
	case "g":
		m.scrollToTop()
	case "/":
		m.filterMode = true
		m.filterInput = ""
	case "esc":
		m.clearFilter()
	case "y":
		return m.copyEntries()
	case "c":
		m.store.Clear()
		m.clearFilter()
		m.statusMsg = "Logs cleared"
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{} })
	}

	return m, nil
}

func (m *Model) copyEntries() (tea.Model, tea.Cmd) {
	entries := m.visibleEntries()
	if len(entries) == 0 {
		m.statusMsg = "Nothing to copy"
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{} })
	}

	var b strings.Builder
	for _, e := range entries {
		cells := m.parser.ResolveColumns(e)
		b.WriteString(strings.Join(cells, "\t"))
		b.WriteByte('\n')
	}

	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(b.String())
	if err := cmd.Run(); err != nil {
		m.statusMsg = fmt.Sprintf("Copy failed: %v", err)
	} else {
		count := len(entries)
		if m.filterQuery != nil {
			m.statusMsg = fmt.Sprintf("Copied %d filtered entries", count)
		} else {
			m.statusMsg = fmt.Sprintf("Copied %d entries", count)
		}
	}

	return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{} })
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.filterMode = false
		return m, nil
	case tea.KeyEsc:
		m.filterMode = false
		m.clearFilter()
		return m, nil
	case tea.KeyBackspace:
		runes := []rune(m.filterInput)
		if len(runes) > 0 {
			m.filterInput = string(runes[:len(runes)-1])
		}
	case tea.KeyRunes:
		m.filterInput += string(msg.Runes)
	case tea.KeySpace:
		m.filterInput += " "
	default:
		return m, nil
	}
	// Live search: apply filter on every keystroke
	m.liveFilter()
	return m, nil
}

func (m *Model) liveFilter() {
	if m.filterInput == "" {
		m.filterQuery = nil
		m.filtered = nil
		m.offset = 0
		m.atBottom = true
		return
	}
	q := &search.Query{Text: m.filterInput}
	results, err := search.Run(m.store, *q)
	if err != nil {
		// Invalid regex while typing — keep previous results
		return
	}
	m.filterQuery = q
	if results == nil {
		results = []*parser.Entry{} // empty, not nil — distinguishes "no matches" from "no filter"
	}
	m.filtered = results
	m.offset = 0
	m.atBottom = true
}

func (m *Model) refreshEntries() {
	newCount := m.store.Len()

	// When scrolled up, adjust offset so the view stays pinned to the same entries
	if !m.atBottom && m.lastCount > 0 {
		added := newCount - m.lastCount
		if added > 0 {
			m.offset += added
		}
	}

	if m.filterQuery != nil {
		results, _ := search.Run(m.store, *m.filterQuery)
		if results == nil {
			results = []*parser.Entry{}
		}
		m.filtered = results
	} else {
		m.filtered = nil
	}
	if m.atBottom {
		m.offset = 0
	}
	m.lastCount = newCount
}

func (m *Model) visibleEntries() []*parser.Entry {
	if m.filtered != nil {
		return m.filtered
	}
	return m.store.All()
}

func (m *Model) tableHeight() int {
	chrome := 3 // header + separator + shortcuts
	if m.filterMode || m.filterQuery != nil {
		chrome = 4 // + search bar
	}
	h := m.height - chrome
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) scrollDown() {
	if m.offset > 0 {
		m.offset--
	}
	if m.offset == 0 {
		m.atBottom = true
	}
}

func (m *Model) scrollUp() {
	entries := m.visibleEntries()
	tableH := m.tableHeight()
	maxOffset := len(entries) - tableH
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset < maxOffset {
		m.offset++
		m.atBottom = false
	}
}

func (m *Model) scrollToBottom() {
	m.offset = 0
	m.atBottom = true
}

func (m *Model) scrollToTop() {
	entries := m.visibleEntries()
	maxOffset := len(entries) - m.tableHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	m.offset = maxOffset
	m.atBottom = false
}

func (m *Model) applyFilter() {
	if m.filterInput == "" {
		m.clearFilter()
		return
	}
	q := &search.Query{Text: m.filterInput}
	m.filterQuery = q
	results, err := search.Run(m.store, *q)
	if err != nil {
		m.filterQuery = nil
		m.filtered = nil
		return
	}
	m.filtered = results
	m.offset = 0
	m.atBottom = true
}

func (m *Model) clearFilter() {
	m.filterQuery = nil
	m.filtered = nil
	m.filterInput = ""
	m.offset = 0
	m.atBottom = true
}

func (m *Model) View() string {
	return renderView(m)
}
