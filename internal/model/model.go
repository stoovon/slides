package model

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/maaslalani/slides/internal/code"
	"github.com/maaslalani/slides/internal/file"
	"github.com/maaslalani/slides/internal/meta"
	"github.com/maaslalani/slides/internal/process"
	"github.com/maaslalani/slides/styles"
)

const (
	delimiter = "\n---\n"
)

type Model struct {
	Slides   []string
	Page     int
	Author   string
	Date     string
	Theme    glamour.TermRendererOption
	Paging   string
	FileName string
	viewport viewport.Model
	// VirtualText is used for additional information that is not part of the
	// original slides, it will be displayed on a slide and reset on page change
	VirtualText string
}

type fileWatchMsg struct{}

var fileInfo os.FileInfo

func (m Model) Init() tea.Cmd {
	if m.FileName == "" {
		return nil
	}
	fileInfo, _ = os.Stat(m.FileName)
	return fileWatchCmd()
}

func fileWatchCmd() tea.Cmd {
	return tea.Every(time.Second, func(t time.Time) tea.Msg {
		return fileWatchMsg{}
	})
}

func (m *Model) Load() error {
	var content string
	var err error

	if m.FileName != "" {
		content, err = readFile(m.FileName)
	} else {
		content, err = readStdin()
	}

	if err != nil {
		return err
	}

	content = strings.TrimPrefix(content, strings.TrimPrefix(delimiter, "\n"))
	slides := strings.Split(content, delimiter)

	metaData, exists := meta.New().Parse(slides[0])
	// If the user specifies a custom configuration options
	// skip the first "slide" since this is all configuration
	if exists && len(slides) > 1 {
		slides = slides[1:]
	}

	m.Slides = slides
	m.Author = metaData.Author
	m.Date = time.Now().Format(metaData.Date)
	m.Paging = metaData.Paging
	if m.Theme == nil {
		m.Theme = styles.SelectTheme(metaData.Theme)
	}

	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case " ", "down", "j", "right", "l", "enter", "n":
			if m.Page < len(m.Slides)-1 {
				m.Page++
			}
			m.VirtualText = ""
		case "up", "k", "left", "h", "p":
			if m.Page > 0 {
				m.Page--
			}
			m.VirtualText = ""
		case "ctrl+e":
			// Run code blocks
			blocks, err := code.Parse(m.Slides[m.Page])
			if err != nil {
				// We couldn't parse the code block on the screen
				m.VirtualText = "\n" + err.Error()
				return m, nil
			}
			var outs []string
			for _, block := range blocks {
				res := code.Execute(block)
				outs = append(outs, res.Out)
			}
			m.VirtualText = strings.Join(outs, "\n")
		}

	case fileWatchMsg:
		newFileInfo, err := os.Stat(m.FileName)
		if err == nil && newFileInfo.ModTime() != fileInfo.ModTime() {
			fileInfo = newFileInfo
			_ = m.Load()
			if m.Page >= len(m.Slides) {
				m.Page = len(m.Slides) - 1
			}
		}
		return m, fileWatchCmd()
	}
	return m, nil
}

func (m Model) View() string {
	r, _ := glamour.NewTermRenderer(m.Theme, glamour.WithWordWrap(m.viewport.Width))
	slide := m.Slides[m.Page]
	slide, err := r.Render(slide)
	slide += m.VirtualText
	if err != nil {
		slide = fmt.Sprintf("Error: Could not render markdown! (%v)", err)
	}
	slide = styles.Slide.Render(slide)

	left := styles.Author.Render(m.Author) + styles.Date.Render(m.Date)
	right := styles.Page.Render(m.paging())
	status := styles.Status.Render(styles.JoinHorizontal(left, right, m.viewport.Width))
	return styles.JoinVertical(slide, status, m.viewport.Height)
}

func (m *Model) paging() string {
	switch strings.Count(m.Paging, "%d") {
	case 2:
		return fmt.Sprintf(m.Paging, m.Page+1, len(m.Slides))
	case 1:
		return fmt.Sprintf(m.Paging, m.Page+1)
	default:
		return m.Paging
	}
}

func readFile(path string) (string, error) {
	s, err := os.Stat(path)
	if err != nil {
		return "", errors.New("could not read file")
	}
	if s.IsDir() {
		return "", errors.New("can not read directory")
	}
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(b)

	// Pre-process slides if the file is executable to avoid
	// unintentional code execution when presenting slides
	if file.IsExecutable(s) {
		// Remove shebang if file has one
		if strings.HasPrefix(content, "#!") {
			content = strings.Join(strings.SplitN(content, "\n", 2)[1:], "\n")
		}

		content = process.Pre(content)
	}

	return content, err
}

func readStdin() (string, error) {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}

	if stat.Mode()&os.ModeNamedPipe == 0 && stat.Size() == 0 {
		return "", errors.New("no slides provided")
	}

	reader := bufio.NewReader(os.Stdin)
	var b strings.Builder

	for {
		r, _, err := reader.ReadRune()
		if err != nil && err == io.EOF {
			break
		}
		_, err = b.WriteRune(r)
		if err != nil {
			return "", err
		}
	}

	return b.String(), nil
}
