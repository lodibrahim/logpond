package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lodibrahim/logpond/internal/parser"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	debugStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	searchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	searchActive = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
)

func renderView(m *Model) string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	var b strings.Builder

	b.WriteString(renderHeaderRow(m))
	b.WriteByte('\n')

	if m.filterMode || m.filterQuery != nil {
		b.WriteString(renderSearchBar(m))
		b.WriteByte('\n')
	}

	entries := m.visibleEntries()
	tableH := m.tableHeight()

	if m.filterQuery != nil && len(entries) == 0 {
		// Active filter with no matches
		b.WriteString(warnStyle.Render("No matches"))
		b.WriteByte('\n')
		for i := 1; i < tableH; i++ {
			b.WriteByte('\n')
		}
	} else {
		startIdx := len(entries) - m.offset - tableH
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := len(entries) - m.offset
		if endIdx > len(entries) {
			endIdx = len(entries)
		}

		for i := startIdx; i < endIdx; i++ {
			b.WriteString(renderRow(m, entries[i]))
			b.WriteByte('\n')
		}

		rendered := endIdx - startIdx
		for i := rendered; i < tableH; i++ {
			b.WriteByte('\n')
		}
	}

	b.WriteString(renderBottomPanel(m))

	return b.String()
}

func renderHeaderRow(m *Model) string {
	var cells []string
	for _, col := range m.cfg.Columns {
		w := col.Width
		if col.Flex {
			w = flexWidth(m)
		}
		name := padOrTrunc(col.Name, w)
		cells = append(cells, headerStyle.Render(name))
	}
	return strings.Join(cells, " ")
}

func renderRow(m *Model, entry *parser.Entry) string {
	cells := m.parser.ResolveColumns(entry)
	var parts []string
	for i, col := range m.cfg.Columns {
		w := col.Width
		if col.Flex {
			w = flexWidth(m)
		}
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		cell = padOrTrunc(cell, w)
		parts = append(parts, cell)
	}

	line := strings.Join(parts, " ")
	line = colorBySeverity(entry.Severity, line)
	return line
}

func renderSearchBar(m *Model) string {
	if m.filterMode {
		return searchActive.Render(fmt.Sprintf("Search: %s█", m.filterInput))
	}
	if m.filterQuery != nil {
		return searchActive.Render(fmt.Sprintf("Search: %s  (Esc to clear)", m.filterQuery.Text))
	}
	return searchStyle.Render("Search: press / to filter")
}

var (
	keyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	descStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	msgStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

func renderBottomPanel(m *Model) string {
	var b strings.Builder

	// Line 1: separator
	b.WriteString(statusStyle.Render(strings.Repeat("─", m.width)))
	b.WriteByte('\n')

	// Line 2: shortcuts or status message
	if m.statusMsg != "" {
		b.WriteString(msgStyle.Render(m.statusMsg))
	} else {
		shortcuts := []struct{ key, desc string }{
			{"/", "search"},
			{"Esc", "clear"},
			{"y", "copy"},
			{"c", "clear logs"},
			{"k/j", "scroll"},
			{"g/G", "top/tail"},
			{"q", "quit"},
		}
		var parts []string
		for _, s := range shortcuts {
			parts = append(parts, keyStyle.Render(s.key)+" "+descStyle.Render(s.desc))
		}
		b.WriteString(strings.Join(parts, statusStyle.Render("  |  ")))
	}

	// Pad to width and add entry count on the right
	total := m.store.Len()
	right := fmt.Sprintf("%d entries", total)
	if m.inputClosed {
		right = fmt.Sprintf("%d entries (closed)", total)
	}
	if m.filtered != nil {
		right = fmt.Sprintf("%d/%d", len(m.filtered), total)
	}
	right = statusStyle.Render(right)

	// Calculate gap for right-alignment on line 2
	lineW := lipgloss.Width(b.String()) - lipgloss.Width(statusStyle.Render(strings.Repeat("─", m.width))) - 1
	rightW := lipgloss.Width(right)
	gap := m.width - lineW - rightW
	if gap < 1 {
		gap = 1
	}
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(right)

	return b.String()
}

func flexWidth(m *Model) int {
	fixed := 0
	for _, col := range m.cfg.Columns {
		if !col.Flex {
			fixed += col.Width
		}
	}
	gaps := len(m.cfg.Columns) - 1
	w := m.width - fixed - gaps
	if w < 10 {
		w = 10
	}
	return w
}

func padOrTrunc(s string, w int) string {
	runes := []rune(s)
	if len(runes) > w {
		if w > 1 {
			return string(runes[:w-1]) + "…"
		}
		return string(runes[:w])
	}
	return s + strings.Repeat(" ", w-len(runes))
}

func colorBySeverity(severity, line string) string {
	switch severity {
	case "WARN":
		return warnStyle.Render(line)
	case "ERROR", "FATAL":
		return errorStyle.Render(line)
	case "DEBUG", "TRACE":
		return debugStyle.Render(line)
	default:
		return line
	}
}
