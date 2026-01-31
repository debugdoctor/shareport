package ui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type TUI struct {
	reader  *bufio.Reader
	logs    []string
	maxLogs int
	batch   int
}

func NewTUI() *TUI {
	return &TUI{
		reader:  bufio.NewReader(os.Stdin),
		logs:    make([]string, 0, 128),
		maxLogs: 200,
	}
}

func (ui *TUI) Println(a ...any) {
	ui.log(fmt.Sprint(a...))
	if ui.batch == 0 {
		ui.render("")
	}
}

func (ui *TUI) Printf(format string, a ...any) {
	ui.log(fmt.Sprintf(format, a...))
	if ui.batch == 0 {
		ui.render("")
	}
}

// Batch groups multiple log writes into a single screen render.
func (ui *TUI) Batch(fn func()) {
	ui.batch++
	defer func() {
		ui.batch--
		if ui.batch < 0 {
			ui.batch = 0
		}
		if ui.batch == 0 {
			ui.render("")
		}
	}()
	fn()
}

func (ui *TUI) Prompt(prompt, def string) string {
	ui.render(formatPrompt(prompt, def))
	text, _ := ui.reader.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		text = def
	}
	if text != "" {
		ui.log("> " + text)
		ui.render("")
	}
	return text
}

func (ui *TUI) WaitEnter(prompt string) {
	ui.render(prompt)
	_, _ = ui.reader.ReadString('\n')
	ui.render("")
}

func (ui *TUI) log(line string) {
	ui.logs = append(ui.logs, line)
	if ui.maxLogs > 0 && len(ui.logs) > ui.maxLogs {
		ui.logs = ui.logs[len(ui.logs)-ui.maxLogs:]
	}
}

func (ui *TUI) render(prompt string) {
	fmt.Print("\033[2J\033[H")
	for _, line := range ui.logs {
		fmt.Println(line)
	}
	fmt.Println()
	if prompt != "" {
		fmt.Print(prompt)
	}
}

func (ui *TUI) Clear() {
	if cmd := exec.Command("clear"); cmd != nil {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}
}

func (ui *TUI) Reset() {
	ui.logs = nil
	ui.Clear()
}

func formatPrompt(prompt, def string) string {
	if def != "" {
		return fmt.Sprintf("%s (默认 %s): ", prompt, def)
	}
	return fmt.Sprintf("%s: ", prompt)
}
