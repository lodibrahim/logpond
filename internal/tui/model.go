package tui

import (
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

// Model is the bubbletea TUI model.
type Model struct {
	cfg    *config.Config
	parser *parser.Parser
	store  *store.Store

	// View state
	width, height int
	offset        int // scroll offset from bottom
	cursor        int // selected row index within visible entries (0 = bottom)
	atBottom      bool

	// Filter
	filterMode  bool
	filterInput string
	filterQuery *search.Query
	filtered    []*parser.Entry

	// Expand
	expandIdx int // -1 = none
	expanded  bool

	// Status
	inputClosed bool
	lastCount   int
}

// New creates a new TUI model.
func New(cfg *config.Config, p *parser.Parser, s *store.Store) *Model {
	return &Model{
		cfg:       cfg,
		parser:    p,
		store:     s,
		atBottom:  true,
		expandIdx: -1,
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

	case tea.KeyMsg:
		return m.handleKey(msg)
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
	case "enter":
		m.toggleExpand()
	}

	return m, nil
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.applyFilter()
		m.filterMode = false
		return m, nil
	case tea.KeyEsc:
		m.filterMode = false
		m.filterInput = ""
		return m, nil
	case tea.KeyBackspace:
		runes := []rune(m.filterInput)
		if len(runes) > 0 {
			m.filterInput = string(runes[:len(runes)-1])
		}
		return m, nil
	case tea.KeyRunes:
		m.filterInput += string(msg.Runes)
		return m, nil
	case tea.KeySpace:
		m.filterInput += " "
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) refreshEntries() {
	if m.filterQuery != nil {
		results, _ := search.Run(m.store, *m.filterQuery)
		m.filtered = results
	} else {
		m.filtered = nil
	}
	if m.atBottom {
		m.offset = 0
	}
	m.lastCount = m.store.Len()
}

func (m *Model) visibleEntries() []*parser.Entry {
	if m.filtered != nil {
		return m.filtered
	}
	return m.store.All()
}

func (m *Model) tableHeight() int {
	h := m.height - 3
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) scrollDown() {
	if m.cursor > 0 {
		m.cursor--
	} else if m.offset > 0 {
		m.offset--
	}
	if m.offset == 0 && m.cursor == 0 {
		m.atBottom = true
	}
}

func (m *Model) scrollUp() {
	entries := m.visibleEntries()
	tableH := m.tableHeight()
	if m.cursor < tableH-1 && m.cursor < len(entries)-1 {
		m.cursor++
		m.atBottom = false
	} else {
		maxOffset := len(entries) - tableH
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.offset < maxOffset {
			m.offset++
			m.atBottom = false
		}
	}
}

func (m *Model) scrollToBottom() {
	m.offset = 0
	m.cursor = 0
	m.atBottom = true
}

func (m *Model) scrollToTop() {
	entries := m.visibleEntries()
	maxOffset := len(entries) - m.tableHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	m.offset = maxOffset
	m.cursor = 0
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

func (m *Model) toggleExpand() {
	if m.expanded {
		m.expanded = false
		m.expandIdx = -1
		return
	}
	entries := m.visibleEntries()
	if len(entries) == 0 {
		return
	}
	idx := len(entries) - m.offset - 1 - m.cursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(entries) {
		idx = len(entries) - 1
	}
	m.expandIdx = idx
	m.expanded = true
}

func (m *Model) View() string {
	return renderView(m)
}
