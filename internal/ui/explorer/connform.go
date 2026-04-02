package explorer

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/robertn/dbx/internal/config"
	"github.com/robertn/dbx/internal/ui/theme"
	"github.com/robertn/dbx/internal/util"
)

// ConnFormSubmitMsg is sent when the form is submitted.
type ConnFormSubmitMsg struct {
	Conn   config.Connection
	IsEdit bool
}

// ConnFormCancelMsg is sent when the form is cancelled.
type ConnFormCancelMsg struct{}

// ConnTestRequestMsg asks the app to test the connection built from current form fields.
type ConnTestRequestMsg struct {
	Conn config.Connection
}

// ConnTestResultMsg carries the result of a connection test back to the form.
type ConnTestResultMsg struct {
	Err error
}

// field indices
const (
	fieldName = iota
	fieldDriver
	fieldHost
	fieldPort
	fieldUser
	fieldPassword
	fieldDatabase
	fieldSSLMode
	fieldFilePath
	fieldCount
)

var fieldLabels = []string{
	"Name", "Driver", "Host", "Port", "User", "Password", "Database", "SSL Mode", "File Path (sqlite)",
}

var drivers = []string{"postgres", "mysql", "sqlite", "mssql"}

func driverIndex(name string) int {
	for i, d := range drivers {
		if d == name {
			return i
		}
	}
	return 0
}

// ConnForm is a simple form for adding/editing a connection.
type ConnForm struct {
	theme     theme.Theme
	fields    [fieldCount]string
	driverIdx int // index into drivers slice
	cursor    int
	isEdit    bool
	origID    string
	width     int
	height    int
	testMsg   string // result of last test (success/error)
	testing   bool   // true while a test is in progress
}

// NewConnForm creates a blank add-connection form.
func NewConnForm(t theme.Theme) ConnForm {
	f := ConnForm{theme: t, driverIdx: 0}
	f.syncDriver()
	f.fields[fieldHost] = "localhost"
	f.fields[fieldSSLMode] = "prefer"
	return f
}

// NewEditConnForm creates a pre-filled edit form.
func NewEditConnForm(t theme.Theme, conn config.Connection) ConnForm {
	f := ConnForm{theme: t, isEdit: true, origID: conn.ID}
	f.fields[fieldName] = conn.Name
	f.driverIdx = driverIndex(conn.Driver)
	f.syncDriver()
	f.fields[fieldHost] = conn.Host
	f.fields[fieldPort] = strconv.Itoa(conn.Port)
	f.fields[fieldUser] = conn.User
	f.fields[fieldPassword] = conn.Password
	f.fields[fieldDatabase] = conn.Database
	f.fields[fieldSSLMode] = conn.SSLMode
	f.fields[fieldFilePath] = conn.FilePath
	return f
}

// syncDriver updates fields[fieldDriver], fieldPort defaults when driver changes.
func (f *ConnForm) syncDriver() {
	f.fields[fieldDriver] = drivers[f.driverIdx]
	// Only overwrite port if it's empty or was a known default
	knownDefaults := map[string]bool{"5432": true, "3306": true, "1433": true, "0": true, "": true}
	if knownDefaults[f.fields[fieldPort]] {
		p := defaultPort(f.fields[fieldDriver])
		if p > 0 {
			f.fields[fieldPort] = strconv.Itoa(p)
		} else {
			f.fields[fieldPort] = ""
		}
	}
}

func (f *ConnForm) SetSize(w, h int) {
	f.width = w
	f.height = h
}

// nextVisible returns the next visible field index in direction (+1 or -1).
func (f ConnForm) nextVisible(from, dir int) int {
	cur := from
	for i := 0; i < fieldCount; i++ {
		cur = (cur + dir + fieldCount) % fieldCount
		if f.fieldVisible(cur) {
			return cur
		}
	}
	return from
}

// lastVisible returns the index of the last visible field.
func (f ConnForm) lastVisible() int {
	for i := fieldCount - 1; i >= 0; i-- {
		if f.fieldVisible(i) {
			return i
		}
	}
	return fieldCount - 1
}

func (f ConnForm) Update(msg tea.Msg) (ConnForm, tea.Cmd) {
	switch msg := msg.(type) {
	case ConnTestResultMsg:
		f.testing = false
		if msg.Err != nil {
			f.testMsg = "✗ " + msg.Err.Error()
		} else {
			f.testMsg = "✓ Connection successful"
		}
		return f, nil
	case tea.KeyMsg:
		f.testMsg = ""
		switch msg.String() {
		case "esc":
			return f, func() tea.Msg { return ConnFormCancelMsg{} }
		case "tab", "down":
			f.cursor = f.nextVisible(f.cursor, 1)
		case "shift+tab", "up":
			f.cursor = f.nextVisible(f.cursor, -1)
		case "enter":
			if f.cursor == fieldDriver {
				f.driverIdx = (f.driverIdx + 1) % len(drivers)
				f.syncDriver()
			} else if f.cursor == f.lastVisible() {
				return f, f.submitCmd()
			} else {
				f.cursor = f.nextVisible(f.cursor, 1)
			}
		case "left":
			if f.cursor == fieldDriver {
				f.driverIdx = (f.driverIdx - 1 + len(drivers)) % len(drivers)
				f.syncDriver()
			}
		case "right":
			if f.cursor == fieldDriver {
				f.driverIdx = (f.driverIdx + 1) % len(drivers)
				f.syncDriver()
			}
		case "ctrl+s":
			return f, f.submitCmd()
		case "ctrl+t":
			if f.testing {
				return f, nil
			}
			f.testing = true
			f.testMsg = "Testing connection…"
			conn := f.toConnection()
			return f, func() tea.Msg { return ConnTestRequestMsg{Conn: conn} }
		case "ctrl+v":
			if f.cursor != fieldDriver {
				if text, err := util.Paste(); err == nil {
					text = strings.NewReplacer("\n", "", "\r", "").Replace(text)
					f.fields[f.cursor] += text
				}
			}
		case "backspace":
			if f.cursor != fieldDriver && len(f.fields[f.cursor]) > 0 {
				runes := []rune(f.fields[f.cursor])
				f.fields[f.cursor] = string(runes[:len(runes)-1])
			}
		default:
			if f.cursor != fieldDriver && len(msg.Runes) == 1 {
				f.fields[f.cursor] += string(msg.Runes[0])
			}
		}
	}
	return f, nil
}

func (f ConnForm) submitCmd() tea.Cmd {
	conn := f.toConnection()
	isEdit := f.isEdit
	return func() tea.Msg {
		return ConnFormSubmitMsg{Conn: conn, IsEdit: isEdit}
	}
}

func (f ConnForm) toConnection() config.Connection {
	port, _ := strconv.Atoi(f.fields[fieldPort])
	if port == 0 {
		port = defaultPort(f.fields[fieldDriver])
	}
	id := f.origID
	if id == "" {
		id = fmt.Sprintf("%s-%s", f.fields[fieldDriver], f.fields[fieldName])
		id = strings.ReplaceAll(id, " ", "-")
	}
	return config.Connection{
		ID:       id,
		Name:     f.fields[fieldName],
		Driver:   f.fields[fieldDriver],
		Host:     f.fields[fieldHost],
		Port:     port,
		User:     f.fields[fieldUser],
		Password: f.fields[fieldPassword],
		Database: f.fields[fieldDatabase],
		SSLMode:  f.fields[fieldSSLMode],
		FilePath: f.fields[fieldFilePath],
	}
}

func defaultPort(driver string) int {
	switch driver {
	case "postgres", "postgresql":
		return 5432
	case "mysql":
		return 3306
	case "mssql", "sqlserver":
		return 1433
	}
	return 0
}

// isSQLite returns true when the currently selected driver is sqlite.
func (f ConnForm) isSQLite() bool {
	return drivers[f.driverIdx] == "sqlite"
}

// fieldVisible returns false for fields that are not relevant to the current driver.
func (f ConnForm) fieldVisible(i int) bool {
	switch i {
	case fieldHost, fieldPort, fieldUser, fieldPassword, fieldSSLMode:
		return !f.isSQLite()
	case fieldFilePath:
		return f.isSQLite()
	}
	return true
}

func (f ConnForm) View() string {
	title := "Add Connection"
	if f.isEdit {
		title = "Edit Connection"
	}

	var sb strings.Builder
	sb.WriteString(f.theme.Bold.Render(title) + "\n\n")

	for i, label := range fieldLabels {
		if !f.fieldVisible(i) {
			continue
		}

		labelStr := fmt.Sprintf("%-20s", label+":")
		focused := i == f.cursor

		var valueStr string
		if i == fieldDriver {
			// Render as a ← driver → selector
			prev := drivers[(f.driverIdx-1+len(drivers))%len(drivers)]
			next := drivers[(f.driverIdx+1)%len(drivers)]
			cur := drivers[f.driverIdx]
			if focused {
				valueStr = f.theme.Dimmed.Render("‹ "+prev+" ") +
					f.theme.TreeSelected.Render(" "+cur+" ") +
					f.theme.Dimmed.Render(" "+next+" ›")
			} else {
				valueStr = f.theme.Bold.Render(cur)
			}
		} else {
			val := f.fields[i]
			if i == fieldPassword {
				val = strings.Repeat("•", len(val))
			}
			if focused {
				valueStr = val + "█"
			} else {
				valueStr = val
			}
		}

		if focused {
			// TreeConnection matches PaletteKey accent but stays fg-only (no palette panel fill).
			labelStr = f.theme.TreeConnection.Render(labelStr)
		} else {
			labelStr = f.theme.Dimmed.Render(labelStr)
		}
		sb.WriteString(labelStr + " " + valueStr + "\n")
	}

	if f.testMsg != "" {
		sb.WriteString("\n")
		switch {
		case strings.HasPrefix(f.testMsg, "✓"):
			sb.WriteString(f.theme.Success.Render(f.testMsg))
		case strings.HasPrefix(f.testMsg, "✗"):
			sb.WriteString(f.theme.Error.Render(f.testMsg))
		default:
			sb.WriteString(f.theme.Dimmed.Render(f.testMsg))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	hint := "tab/↑↓: next field  ←/→: change driver  ctrl+t: test  ctrl+s: save  esc: cancel"
	sb.WriteString(f.theme.Dimmed.Render(hint))

	return lipgloss.NewStyle().Width(f.width).Height(f.height).Render(sb.String())
}
