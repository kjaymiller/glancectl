package ui

import "github.com/charmbracelet/lipgloss"

var (
	colSubtle = lipgloss.Color("244")
	colOK     = lipgloss.Color("42")
	colBad    = lipgloss.Color("203")
	colWarn   = lipgloss.Color("214")
	colAccent = lipgloss.Color("69")

	titleBar = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(colAccent).
		Padding(0, 1)

	paneTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colAccent)

	paneTitleFocused = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("230")).
		Background(colAccent).
		Padding(0, 1)

	paneBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colSubtle).
		Padding(0, 1)

	paneBoxFocused = paneBox.
			BorderForeground(colAccent)

	subtle  = lipgloss.NewStyle().Foreground(colSubtle)
	good    = lipgloss.NewStyle().Foreground(colOK)
	bad     = lipgloss.NewStyle().Foreground(colBad)
	warn    = lipgloss.NewStyle().Foreground(colWarn)
	accent  = lipgloss.NewStyle().Foreground(colAccent)
	groupSt = lipgloss.NewStyle().Bold(true).Foreground(colWarn)

	footer = lipgloss.NewStyle().
		Foreground(colSubtle).
		Padding(0, 1)

	selected = lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(colAccent)
)
