package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
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
	stepResourceDecision
	stepResource
	stepBackupName
	stepExecute
)

type item struct {
	title       string
	description string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.description }
func (i item) FilterValue() string { return i.title }

type model struct {
	resourceDecisionList list.Model
	contextList          list.Model
	namespaceList        list.Model
	resourceList         list.Model
	backupList           list.Model
	operationList        list.Model
	err                  error
	selectedOp           item
	selectedCtx          item
	selectedBackup       item
	backupName           string
	selectedNS           []list.Item
	selectedRes          []list.Item
	backupNameInput      textinput.Model
	step                 step
}

var (
	helpStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render
	titleStyle        = lipgloss.NewStyle().MarginLeft(2).Bold(true).Render
	errorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Render
	listStyle         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Margin(1).Width(70).Height(20)
	selectedListStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Margin(1).Width(50).Height(20).Foreground(lipgloss.Color("205"))
	logFile           *os.File
	logger            *log.Logger
)

func init() {
	var err error
	logFile, err = os.OpenFile("program.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		fmt.Printf("Error opening log file: %v\n", err)
		os.Exit(1)
	}
	logger = log.New(logFile, "VELERO-UI: ", log.Ldate|log.Ltime|log.Lshortfile)
}

func runShellCommand(cmdStr string) (string, error) {
	logger.Printf("Running command: %s", cmdStr)
	cmd := exec.Command("sh", "-c", cmdStr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Printf("Command error: %v", err)
	}
	logger.Printf("Command output: %s", string(output))
	return string(output), err
}

func fetchItems(command string) ([]list.Item, error) {
	output, err := runShellCommand(command)
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

func waitForCompletion(operation, name string) error {
	for {
		statusCmd := fmt.Sprintf("velero %s describe %s --details -o json", operation, name)
		output, err := runShellCommand(statusCmd)
		if err != nil {
			logger.Printf("Error fetching %s status: %v", operation, err)
			return err
		}
		if strings.Contains(output, "\"Phase\": \"Completed\"") {
			logger.Printf("%s %s completed successfully", operation, name)
			return nil
		}
		if strings.Contains(output, "\"Phase\": \"Failed\"") {
			logger.Printf("%s %s failed", operation, name)
			return fmt.Errorf("%s %s failed", operation, name)
		}
		time.Sleep(5 * time.Second)
	}
}

func initialModel() model {
	operations := []list.Item{
		item{title: "Backup", description: "Backup the data"},
		item{title: "Restore", description: "Restore the data"},
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

	resourceList := list.New([]list.Item{}, delegate, 0, 0)
	resourceList.Title = titleStyle("Select Resources (Press Enter to Confirm)")
	resourceList.SetShowStatusBar(false)
	resourceList.SetFilteringEnabled(false)
	resourceList.SetShowHelp(false)
	resourceList.SetSize(70, 20)

	resourceDecisionItems := []list.Item{
		item{title: "Yes", description: "Select specific resources"},
		item{title: "No", description: "Restore the entire namespace"},
	}
	resourceDecisionList := list.New(resourceDecisionItems, delegate, 0, 0)
	resourceDecisionList.SetShowStatusBar(false)
	resourceDecisionList.SetFilteringEnabled(false)
	resourceDecisionList.SetShowHelp(false)
	resourceDecisionList.SetSize(70, 20)

	backupNameInput := textinput.New()
	backupNameInput.Placeholder = "Enter backup name"
	backupNameInput.Width = 70

	return model{
		step:                 stepOperation,
		operationList:        operationList,
		contextList:          contextList,
		namespaceList:        namespaceList,
		resourceList:         resourceList,
		backupList:           backupList,
		resourceDecisionList: resourceDecisionList,
		backupNameInput:      backupNameInput,
		selectedNS:           []list.Item{},
		selectedRes:          []list.Item{},
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
			logger.Printf("Deselected item: %s", item.FilterValue())
			return
		}
	}
	*selected = append(*selected, item)
	logger.Printf("Selected item: %s", item.FilterValue())
}

func parseResources(output string) []list.Item {
	lines := strings.Split(output, "\n")
	var items []list.Item
	re := regexp.MustCompile(`^(pod|service|deployment\.apps|replicaset\.apps)/([^ ]+)`)
	seen := make(map[string]bool)
	for _, line := range lines {
		matches := re.FindStringSubmatch(line)
		if matches != nil {
			resource := matches[0]
			if !seen[resource] {
				items = append(items, item{title: resource, description: ""})
				seen[resource] = true
			}
		}
	}
	return items
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			switch m.step {
			case stepOperation:
				m.selectedOp = m.operationList.SelectedItem().(item)
				logger.Printf("Selected operation: %s", m.selectedOp.Title())
				if m.selectedOp.Title() == "Backup" {
					m.step = stepContext
					contextItems, err := fetchItems("kubectl config get-contexts -o name")
					if err != nil {
						m.err = fmt.Errorf("error fetching contexts: %w", err)
						logger.Printf("Error: %v", m.err)
						return m, tea.Quit
					}
					m.contextList.SetItems(contextItems)
				} else {
					m.step = stepBackupSelection
					backupItems, err := fetchItems("velero backup get -o custom-columns=NAME:.metadata.name --no-headers")
					if err != nil {
						m.err = fmt.Errorf("error fetching backups: %w", err)
						logger.Printf("Error: %v", m.err)
						return m, tea.Quit
					}
					m.backupList.SetItems(backupItems)
				}
			case stepBackupSelection:
				m.selectedBackup = m.backupList.SelectedItem().(item)
				logger.Printf("Selected backup: %s", m.selectedBackup.Title())
				m.step = stepContext
				contextItems, err := fetchItems("kubectl config get-contexts -o name")
				if err != nil {
					m.err = fmt.Errorf("error fetching contexts: %w", err)
					logger.Printf("Error: %v", m.err)
					return m, tea.Quit
				}
				m.contextList.SetItems(contextItems)
			case stepContext:
				m.selectedCtx = m.contextList.SelectedItem().(item)
				logger.Printf("Selected context: %s", m.selectedCtx.Title())
				m.step = stepNamespace
				namespaceItems, err := fetchItems("kubectl get namespaces -o custom-columns=NAME:.metadata.name --no-headers")
				if err != nil {
					m.err = fmt.Errorf("error fetching namespaces: %w", err)
					logger.Printf("Error: %v", m.err)
					return m, tea.Quit
				}
				m.namespaceList.SetItems(namespaceItems)
			case stepNamespace:
				if len(m.selectedNS) > 0 {
					logger.Printf("Selected namespaces: %v", m.selectedNS)
					if m.selectedOp.Title() == "Backup" {
						m.resourceDecisionList.Title = titleStyle("Backup Specific Resources? (Press Enter to Confirm)")
					} else {
						m.resourceDecisionList.Title = titleStyle("Restore Specific Resources? (Press Enter to Confirm)")
					}
					m.step = stepResourceDecision
				} else {
					m.err = fmt.Errorf("no namespace selected")
					logger.Printf("Error: %v", m.err)
				}
			case stepResourceDecision:
				if m.resourceDecisionList.SelectedItem().(item).Title() == "Yes" {
					m.step = stepResource

					namespaceStrs := make([]string, 0, len(m.selectedNS))
					for _, namespaceItem := range m.selectedNS {
						if i, ok := namespaceItem.(item); ok {
							namespaceStrs = append(namespaceStrs, i.Title())
						}
					}
					resourceOutput, err := runShellCommand(fmt.Sprintf("kubectl get all -n %s --no-headers", strings.Join(namespaceStrs, " ")))
					if err != nil {
						m.err = fmt.Errorf("error fetching resources: %w", err)
						logger.Printf("Error: %v", m.err)
						return m, tea.Quit
					}
					resourceItems := parseResources(resourceOutput)
					m.resourceList.SetItems(resourceItems)
					m.resourceList.SetWidth(m.namespaceList.Width())
					m.resourceList.SetHeight(m.namespaceList.Height())
				} else {
					m.step = stepBackupName
					return m, m.backupNameInput.Focus()
				}
			case stepResource:
				if len(m.selectedRes) > 0 {
					logger.Printf("Selected resources: %v", m.selectedRes)
					m.step = stepBackupName
					return m, m.backupNameInput.Focus()
				} else {
					m.err = fmt.Errorf("no resource selected")
					logger.Printf("Error: %v", m.err)
				}
			case stepBackupName:
				m.backupName = m.backupNameInput.Value()
				logger.Printf("Entered backup name: %s", m.backupName)
				m.step = stepExecute
				return m, tea.Quit
			case stepExecute:
				if m.selectedOp.Title() == "Backup" {
					namespaceStrs := make([]string, 0, len(m.selectedNS))
					for _, namespaceItem := range m.selectedNS {
						if i, ok := namespaceItem.(item); ok {
							namespaceStrs = append(namespaceStrs, i.Title())
						}
					}
					backupCmd := fmt.Sprintf("velero backup create %s --include-namespaces %s --kubecontext %s", m.backupName, strings.Join(namespaceStrs, ","), m.selectedCtx.Title())
					output, err := runShellCommand(backupCmd)
					logger.Printf("Backup command output: %s", output)
					if err != nil {
						m.err = fmt.Errorf("error starting backup: %w", err)
						logger.Printf("Error: %v\nOutput: %s", m.err, output)
						return m, tea.Quit
					}
					if err := waitForCompletion("backup", m.backupName); err != nil {
						m.err = fmt.Errorf("error waiting for backup completion: %w", err)
						logger.Printf("Error: %v", m.err)
						return m, tea.Quit
					}
					logger.Println("Backup complete")
				} else {
					restoreCmd := fmt.Sprintf("velero restore create --from-backup %s", m.selectedBackup.Title())
					output, err := runShellCommand(restoreCmd)
					logger.Printf("Restore command output: %s", output)
					if err != nil {
						m.err = fmt.Errorf("error starting restore: %w", err)
						logger.Printf("Error: %v\nOutput: %s", m.err, output)
						return m, tea.Quit
					}
					if err := waitForCompletion("restore", m.selectedBackup.Title()); err != nil {
						m.err = fmt.Errorf("error waiting for restore completion: %w", err)
						logger.Printf("Error: %v", m.err)
						return m, tea.Quit
					}
					logger.Println("Restore complete")
				}
				return m, tea.Quit
			}
		case "ctrl+c", "q":
			logger.Println("Program exited by user")
			return m, tea.Quit
		case " ":
			switch m.step {
			case stepNamespace:
				m.toggleSelection(&m.namespaceList, &m.selectedNS)
			case stepResource:
				m.toggleSelection(&m.resourceList, &m.selectedRes)
			}
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
	case stepResourceDecision:
		m.resourceDecisionList, cmd = m.resourceDecisionList.Update(msg)
	case stepResource:
		m.resourceList, cmd = m.resourceList.Update(msg)
	case stepBackupName:
		m.backupNameInput, cmd = m.backupNameInput.Update(msg)
	}
	return m, cmd
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
		errView = errorStyle(fmt.Sprintf("\nError: %v\n", m.err))
	}

	var selectedView string
	switch m.step {
	case stepOperation:
		operationView := listStyle.Render(m.operationList.View())
		return operationView + fmt.Sprintf("\n\n%s\n%s", helpStyle("Press Enter to confirm or 'ctrl+c' to quit"), errView)
	case stepBackupSelection:
		backupView := listStyle.Render(m.backupList.View())
		return backupView + fmt.Sprintf("\n\n%s\n%s", helpStyle("Press Enter to confirm or 'ctrl+c' to quit"), errView)
	case stepContext:
		contextView := listStyle.Render(m.contextList.View())
		return contextView + fmt.Sprintf("\n\n%s\n%s", helpStyle("Press Enter to confirm or 'ctrl+c' to quit"), errView)
	case stepNamespace:
		selectedView = selectedListStyle.Render(fmt.Sprintf("Selected Namespaces:\n%s", renderSelectedItems(m.selectedNS)))
		namespaceView := listStyle.Render(m.namespaceList.View())
		return lipgloss.JoinHorizontal(lipgloss.Top, namespaceView, selectedView) + fmt.Sprintf("\n\n%s\n%s", helpStyle("Press Enter to confirm, Space to select/deselect, or 'ctrl+c' to quit"), errView)
	case stepResourceDecision:
		resourceDecisionView := listStyle.Render(m.resourceDecisionList.View())
		return resourceDecisionView + fmt.Sprintf("\n\n%s\n%s", helpStyle("Press Enter to confirm or 'ctrl+c' to quit"), errView)
	case stepResource:
		selectedView = selectedListStyle.Render(fmt.Sprintf("Selected Resources:\n%s", renderSelectedItems(m.selectedRes)))
		resourceView := listStyle.Render(m.resourceList.View())
		return lipgloss.JoinHorizontal(lipgloss.Top, resourceView, selectedView) + fmt.Sprintf("\n\n%s\n%s", helpStyle("Press Enter to confirm, Space to select/deselect, or 'ctrl+c' to quit"), errView)
	case stepBackupName:
		backupNameView := listStyle.Render(m.backupNameInput.View())
		return backupNameView + fmt.Sprintf("\n\n%s\n%s", helpStyle("Press Enter to confirm or 'ctrl+c' to quit"), errView)
	}
	return ""
}

func main() {
	defer logFile.Close()
	logger.Println("Program started")
	p := tea.NewProgram(initialModel())
	if err := func() error {
		_, err := p.Run()
		return err
	}(); err != nil {
		logger.Printf("Error: %v\n", err)
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	logger.Println("Program finished successfully")
}
