// Command clihome lists, inspects, creates, and syncs the config homes of AI CLIs.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"clihome/internal/home"
	"clihome/internal/tui"
	"clihome/internal/ui"
)

// version is the release version, injected at build time via
// -ldflags "-X main.version=...". Defaults to "dev" for local builds.
var version = "dev"

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
	}
	switch cmd {
	case "list", "ls":
		cmdList()
	case "version", "--version", "-v":
		fmt.Println("clihome " + version)
	case "":
		runTUI()
	case "help", "-h", "--help":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		os.Exit(2)
	}
}

func runTUI() {
	// Force truecolor so exact hex shades aren't downgraded to 256-color indices
	// (which some terminal themes remap — e.g. dark grays rendered light).
	lipgloss.SetColorProfile(termenv.TrueColor)
	if _, err := tea.NewProgram(tui.New(), tea.WithAltScreen(), tea.WithMouseAllMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func cmdList() {
	fmt.Println(ui.Banner(""))
	homes := home.Discover()
	if len(homes) == 0 {
		fmt.Printf("   %s\n\n", ui.Dim.Render("no AI CLI homes found"))
		return
	}
	rows, w := ui.Rows(homes)
	const g = "   "
	fmt.Printf("     %s%s%s%s%s%s%s%s%s\n",
		ui.Dim.Render(ui.Pad("HOME", w.Name)), g, ui.Dim.Render(ui.Pad("TOOL", w.Tool)), g,
		ui.Dim.Render(ui.Pad("ACCOUNT", w.Account)), g, ui.Dim.Render(ui.Pad("MODEL", w.Model)), g,
		ui.Dim.Render("LAST ACTIVE"))
	for _, r := range rows {
		tint := ui.Tint(r.Codex)
		dot := ui.Dim.Render("○")
		if r.HasAcct {
			dot = tint.Render("●")
		}
		acct := ui.Dim.Render(ui.Pad(r.Account, w.Account))
		if r.HasAcct {
			acct = ui.Cyan.Render(ui.Pad(r.Account, w.Account))
		}
		model := ui.Dim.Render(ui.Pad(r.Model, w.Model))
		if r.Model != "—" {
			model = ui.Cream.Render(ui.Pad(r.Model, w.Model))
		}
		fmt.Printf("   %s %s%s%s%s%s%s%s%s%s\n",
			dot, tint.Bold(true).Render(ui.Pad(r.Name, w.Name)), g, ui.Dim.Render(ui.Pad(r.Tool, w.Tool)), g,
			acct, g, model, g, ui.Dim.Render(r.Last))
	}
	fmt.Printf("\n   %s  %s  %s %s\n\n",
		ui.Dim.Render(fmt.Sprintf("%d homes", len(homes))), ui.Dim.Render("·"),
		ui.Dim.Render("details:"), ui.Clay.Render("clihome info <name>"))
}

func printHelp() {
	fmt.Println(ui.Banner(""))
	fmt.Println("   clihome — manage the config homes of AI CLIs (claude, codex)")
	fmt.Println()
	fmt.Println("   USAGE")
	fmt.Println("     clihome            interactive homes table (Bubble Tea)")
	fmt.Println("     clihome list       list all homes")
	fmt.Println("     clihome info <n>   details for a home   (Go port in progress)")
	fmt.Println("     clihome sync ...   mirror one home onto another   (Go port in progress)")
	fmt.Println()
}
