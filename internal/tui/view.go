package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lodibrahim/logpond/internal/parser"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	debugStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	expandStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	cursorStyle  = lipgloss.NewStyle().Reverse(true)
)

func renderView(m *Model) string {
	if m.width == 0 || m.height == 0 {
		return "loading..."
	}

	if m.expanded && m.expandIdx >= 0 {
		return renderExpanded(m)
	}

	var b strings.Builder

	b.WriteString(renderHeaderRow(m))
	b.WriteByte('\n')

	b.WriteString(strings.Repeat("─", m.width))
	b.WriteByte('\n')

	entries := m.visibleEntries()
	tableH := m.tableHeight()

	startIdx := len(entries) - m.offset - tableH
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := len(entries) - m.offset
	if endIdx > len(entries) {
		endIdx = len(entries)
	}

	for i := startIdx; i < endIdx; i++ {
		row := renderRow(m, entries[i])
		// Highlight the cursor row (cursor 0 = bottom visible row)
		rowFromBottom := endIdx - 1 - i
		if rowFromBottom == m.cursor {
			row = cursorStyle.Render(row)
		}
		b.WriteString(row)
		b.WriteByte('\n')
	}

	rendered := endIdx - startIdx
	for i := rendered; i < tableH; i++ {
		b.WriteByte('\n')
	}

	b.WriteString(renderStatusBar(m))

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

func renderExpanded(m *Model) string {
	entries := m.visibleEntries()
	if m.expandIdx >= len(entries) {
		return ""
	}
	entry := entries[m.expandIdx]

	var b strings.Builder
	b.WriteString(expandStyle.Render("── Log Entry Detail ──"))
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("  Time:     %s\n", entry.Timestamp.Format("15:04:05.000")))
	b.WriteString(fmt.Sprintf("  Level:    %s\n", entry.Severity))
	b.WriteString(fmt.Sprintf("  Body:     %s\n", entry.Body))
	b.WriteByte('\n')

	keys := make([]string, 0, len(entry.Fields))
	for k := range entry.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString(expandStyle.Render("  Fields:"))
	b.WriteByte('\n')
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("    %s = %s\n", k, entry.Fields[k]))
	}
	b.WriteByte('\n')
	b.WriteString(statusStyle.Render("  Press Enter to close"))
	return b.String()
}

func renderStatusBar(m *Model) string {
	var left string
	if m.filterMode {
		left = fmt.Sprintf("Filter: /%s█", m.filterInput)
	} else if m.filterQuery != nil {
		left = fmt.Sprintf("Filter: /%s", m.filterQuery.Text)
	}

	total := m.store.Len()
	right := fmt.Sprintf("%d entries", total)
	if m.inputClosed {
		right = fmt.Sprintf("%d entries (input closed)", total)
	}
	if m.filtered != nil {
		right = fmt.Sprintf("%d/%d entries", len(m.filtered), total)
	}

	leftW := len([]rune(left))
	rightW := len([]rune(right))
	gap := m.width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	return statusStyle.Render(left + strings.Repeat(" ", gap) + right)
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
