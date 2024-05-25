package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type step int

const (
	stepOperation step = iota
	stepContext
	stepNamespace
	stepBackupSelection
	stepBackupName
	stepExecute
	helpMessage        = "Press Enter to confirm or 'ctrl+c' to quit"
	errorMessageFormat = "\nError: %v\n"
	viewFormat         = "\n\n%s\n%s"
)

type item struct {
	title       string
	description string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.description }
func (i item) FilterValue() string { return i.title }

type model struct {
	contextList     list.Model
	namespaceList   list.Model
	backupList      list.Model
	operationList   list.Model
	err             error
	selectedOp      item
	selectedCtx     item
	selectedBackup  item
	backupName      string
	selectedNS      []list.Item
	backupNameInput textinput.Model
	step            step
}

var (
	helpStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render
	titleStyle        = lipgloss.NewStyle().MarginLeft(2).Bold(true).Render
	errorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Render
	listStyle         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Margin(1).Width(70).Height(20)
	selectedListStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Margin(1).Width(50).Height(20).Foreground(lipgloss.Color("205"))
	logger            *log.Logger
	debug             bool
	logBuffer         bytes.Buffer
)

func initLogger() {
	if debug {
		logger = log.New(&logBuffer, "VELERO-UI: ", log.Ldate|log.Ltime|log.Lshortfile)
	} else {
		logger = log.New(nil, "", 0)
	}
}

func runShellCommand(cmdStr string, logOutput bool) (string, error) {
	if debug {
		logger.Printf("Running command: %s", cmdStr)
	}
	cmd := exec.Command("sh", "-c", cmdStr)
	output, err := cmd.CombinedOutput()
	if err != nil && debug {
		logger.Printf("Command error: %v", err)
	}
	if logOutput && debug {
		logger.Printf("Command output: %s", string(output))
	}
	return string(output), err
}

func fetchItems(command string) ([]list.Item, error) {
	output, err := runShellCommand(command, false)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	items := make([]list.Item, len(lines))
	for i, line := range lines {
		items[i] = item{title: line, description: ""}
	}
	return items, nil
}

func fetchBackups() ([]list.Item, error) {
	output, err := runShellCommand("velero backup get -o json", false)
	if err != nil {
		return nil, err
	}

	var backups []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}

	if err := json.Unmarshal([]byte(output), &backups); err != nil {
		return nil, err
	}

	items := make([]list.Item, len(backups))
	for i, backup := range backups {
		items[i] = item{title: backup.Metadata.Name, description: ""}
	}

	return items, nil
}

func waitForCompletion(operation, name string) error {
	for {
		statusCmd := fmt.Sprintf("velero %s describe %s --details -o json", operation, name)
		output, err := runShellCommand(statusCmd, false)
		if err != nil && debug {
			logger.Printf("Error fetching %s status: %v", operation, err)
		}
		if strings.Contains(output, "\"Phase\": \"Completed\"") {
			if debug {
				logger.Printf("%s %s completed successfully", operation, name)
			}
			return nil
		}
		if strings.Contains(output, "\"Phase\": \"Failed\"") {
			if debug {
				logger.Printf("%s %s failed", operation, name)
			}
			return fmt.Errorf("%s %s failed", operation, name)
		}
		time.Sleep(5 * time.Second)
	}
}

func initialModel() model {
	operations := []list.Item{
		item{title: "Backup", description: "Create a velero backup"},
		item{title: "Restore", description: "Restore a velero backup"},
	}

	delegate := list.NewDefaultDelegate()
	operationList := list.New(operations, delegate, 0, 0)
	operationList.Title = titleStyle("Velero-UI: Select Operation (Press Enter to Confirm)")
	operationList.SetShowStatusBar(false)
	operationList.SetFilteringEnabled(false)
	operationList.SetShowHelp(false)
	operationList.SetSize(70, 20)

	contextList := list.New([]list.Item{}, delegate, 0, 0)
	contextList.Title = titleStyle("Select Context (Press Enter to Confirm)")
	contextList.SetShowStatusBar(false)
	contextList.SetFilteringEnabled(false)
	contextList.SetShowHelp(false)
	contextList.SetSize(70, 20)

	namespaceList := list.New([]list.Item{}, delegate, 0, 0)
	namespaceList.Title = titleStyle("Select Namespaces (Press Enter to Confirm)")
	namespaceList.SetShowStatusBar(false)
	namespaceList.SetFilteringEnabled(false)
	namespaceList.SetShowHelp(false)
	namespaceList.SetSize(70, 20)

	backupList := list.New([]list.Item{}, delegate, 0, 0)
	backupList.Title = titleStyle("Select Backup (Press Enter to Confirm)")
	backupList.SetShowStatusBar(false)
	backupList.SetFilteringEnabled(false)
	backupList.SetShowHelp(false)
	backupList.SetSize(70, 20)

	backupNameInput := textinput.New()
	backupNameInput.Placeholder = "Enter backup name"
	backupNameInput.Width = 70

	return model{
		step:            stepOperation,
		operationList:   operationList,
		contextList:     contextList,
		namespaceList:   namespaceList,
		backupList:      backupList,
		backupNameInput: backupNameInput,
		selectedNS:      []list.Item{},
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m *model) toggleSelection(l *list.Model, selected *[]list.Item) {
	index := l.Index()
	if index < 0 || index >= len(l.Items()) {
		return
	}
	item := l.Items()[index]
	for i, selectedItem := range *selected {
		if selectedItem.FilterValue() == item.FilterValue() {
			*selected = append((*selected)[:i], (*selected)[i+1:]...)
			if debug {
				logger.Printf("Deselected item: %s", item.FilterValue())
			}
			return
		}
	}
	*selected = append(*selected, item)
	if debug {
		logger.Printf("Selected item: %s", item.FilterValue())
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			return m.handleEnter()
		case "ctrl+c":
			if debug {
				logger.Println("Program exited by user")
			}
			return m, tea.Quit
		case " ":
			return m.handleSpace()
		}
	}

	var cmd tea.Cmd
	switch m.step {
	case stepOperation:
		m.operationList, cmd = m.operationList.Update(msg)
	case stepBackupSelection:
		m.backupList, cmd = m.backupList.Update(msg)
	case stepContext:
		m.contextList, cmd = m.contextList.Update(msg)
	case stepNamespace:
		m.namespaceList, cmd = m.namespaceList.Update(msg)
	case stepBackupName:
		m.backupNameInput, cmd = m.backupNameInput.Update(msg)
	}
	return m, cmd
}

func (m model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepOperation:
		return m.handleOperationEnter()
	case stepBackupSelection:
		return m.handleBackupSelectionEnter()
	case stepContext:
		return m.handleContextEnter()
	case stepNamespace:
		return m.handleNamespaceEnter()
	case stepBackupName:
		return m.handleBackupNameEnter()
	case stepExecute:
		return m.handleExecuteEnter()
	}
	return m, nil
}

func (m model) handleSpace() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepNamespace:
		m.toggleSelection(&m.namespaceList, &m.selectedNS)
	}
	return m, nil
}

func (m model) handleOperationEnter() (tea.Model, tea.Cmd) {
	m.selectedOp = m.operationList.SelectedItem().(item)
	if debug {
		logger.Printf("Selected operation: %s", m.selectedOp.Title())
	}
	if m.selectedOp.Title() == "Backup" {
		m.step = stepContext
		return m.fetchContexts()
	} else {
		m.step = stepBackupSelection
		return m.fetchBackups()
	}
}

func (m model) handleBackupSelectionEnter() (tea.Model, tea.Cmd) {
	m.selectedBackup = m.backupList.SelectedItem().(item)
	if debug {
		logger.Printf("Selected backup: %s", m.selectedBackup.Title())
	}
	m.step = stepContext
	return m.fetchContexts()
}

func (m model) handleContextEnter() (tea.Model, tea.Cmd) {
	m.selectedCtx = m.contextList.SelectedItem().(item)
	if debug {
		logger.Printf("Selected context: %s", m.selectedCtx.Title())
	}
	m.step = stepNamespace
	return m.fetchNamespaces()
}

func (m model) handleNamespaceEnter() (tea.Model, tea.Cmd) {
	if len(m.selectedNS) > 0 {
		if debug {
			logger.Printf("Selected namespaces: %v", m.selectedNS)
		}
		m.step = stepBackupName
		return m, m.backupNameInput.Focus()
	} else {
		m.err = fmt.Errorf("no namespace selected")
		if debug {
			logger.Printf(errorMessageFormat, m.err)
		}
	}
	return m, nil
}

func (m model) handleBackupNameEnter() (tea.Model, tea.Cmd) {
	m.backupName = m.backupNameInput.Value()
	if debug {
		logger.Printf("Entered backup name: %s", m.backupName)
	}
	m.step = stepExecute
	return m.executeOperation()
}

func (m model) handleExecuteEnter() (tea.Model, tea.Cmd) {
	return m.executeOperation()
}

func (m model) executeOperation() (tea.Model, tea.Cmd) {
	var cmdStr string
	if m.selectedOp.Title() == "Backup" {
		namespaces := make([]string, len(m.selectedNS))
		for i, ns := range m.selectedNS {
			namespaces[i] = ns.(item).Title()
		}
		cmdStr = fmt.Sprintf("velero backup create %s --include-namespaces %s --kubecontext %s", m.backupName, strings.Join(namespaces, ","), m.selectedCtx.Title())
	} else {
		cmdStr = fmt.Sprintf("velero restore create --from-backup %s", m.selectedBackup.Title())
	}
	output, err := runShellCommand(cmdStr, true)
	if err != nil {
		m.err = fmt.Errorf("error executing command: %w", err)
		if debug {
			logger.Printf("Error: %v\nOutput: %s", m.err, output)
		}
		return m, tea.Quit
	}
	if debug {
		logger.Printf("Command output: %s", output)
	}
	if err := waitForCompletion(strings.ToLower(m.selectedOp.Title()), m.backupName); err != nil {
		m.err = fmt.Errorf("error waiting for completion: %w", err)
		if debug {
			logger.Printf(errorMessageFormat, m.err)
		}
		return m, tea.Quit
	}
	if debug {
		logger.Println("Operation completed successfully")
	}
	return m, tea.Quit
}

func (m model) fetchContexts() (tea.Model, tea.Cmd) {
	contextItems, err := fetchItems("kubectl config get-contexts -o name")
	if err != nil {
		m.err = fmt.Errorf("error fetching contexts: %w", err)
		if debug {
			logger.Printf(errorMessageFormat, m.err)
		}
		return m, tea.Quit
	}
	m.contextList.SetItems(contextItems)
	return m, nil
}

func (m model) fetchNamespaces() (tea.Model, tea.Cmd) {
	namespaceItems, err := fetchItems("kubectl get namespaces -o custom-columns=NAME:.metadata.name --no-headers")
	if err != nil {
		m.err = fmt.Errorf("error fetching namespaces: %w", err)
		if debug {
			logger.Printf(errorMessageFormat, m.err)
		}
		return m, tea.Quit
	}
	m.namespaceList.SetItems(namespaceItems)
	return m, nil
}

func (m model) fetchBackups() (tea.Model, tea.Cmd) {
	backupItems, err := fetchBackups()
	if err != nil {
		m.err = fmt.Errorf("error fetching backups: %w", err)
		if debug {
			logger.Printf(errorMessageFormat, m.err)
		}
		return m, tea.Quit
	}
	m.backupList.SetItems(backupItems)
	return m, nil
}

func renderSelectedItems(selected []list.Item) string {
	var builder strings.Builder
	for _, selectedItem := range selected {
		if i, ok := selectedItem.(item); ok {
			builder.WriteString(fmt.Sprintf("• %s\n", i.Title()))
		}
	}
	return builder.String()
}

func (m model) View() string {
	var errView string
	if m.err != nil {
		errView = errorStyle(fmt.Sprintf(errorMessageFormat, m.err))
	}

	var selectedView string
	switch m.step {
	case stepOperation:
		operationView := listStyle.Render(m.operationList.View())
		return operationView + fmt.Sprintf(viewFormat, helpStyle(helpMessage), errView)
	case stepBackupSelection:
		backupView := listStyle.Render(m.backupList.View())
		return backupView + fmt.Sprintf(viewFormat, helpStyle(helpMessage), errView)
	case stepContext:
		contextView := listStyle.Render(m.contextList.View())
		return contextView + fmt.Sprintf(viewFormat, helpStyle(helpMessage), errView)
	case stepNamespace:
		selectedView = selectedListStyle.Render(fmt.Sprintf("Selected Namespaces:\n%s", renderSelectedItems(m.selectedNS)))
		namespaceView := listStyle.Render(m.namespaceList.View())
		return lipgloss.JoinHorizontal(lipgloss.Top, namespaceView, selectedView) + fmt.Sprintf(viewFormat, helpStyle(helpMessage), errView)
	case stepBackupName:
		backupNameView := listStyle.Render(m.backupNameInput.View())
		return backupNameView + fmt.Sprintf(viewFormat, helpStyle(helpMessage), errView)
	}
	return ""
}

func main() {
	debugPtr := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	debug = *debugPtr

	initLogger()

	p := tea.NewProgram(initialModel())
	if err := func() error {
		_, err := p.Run()
		return err
	}(); err != nil {
		if debug {
			logger.Printf("Error: %v\n", err)
		}
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if debug {
		fmt.Println("\nDebug log:")
		fmt.Println(logBuffer.String())
	}
}
