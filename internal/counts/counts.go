package counts

import (
	"encoding/json"
	"fmt"
	"io"
	"text/template"

	"github.com/mattgmak/agent-sesh/internal/registry"
	"github.com/mattgmak/agent-sesh/internal/tmux"
)

// Nerd Font icons — same codepoints as the picker status display.
const (
	iconAttention = "\U000F0377" // 󰍷
	iconActive    = "\U000F09D1" // 󰧑
	iconIdle      = "\U000F04B2" // 󰒲
)

// DefaultFormat is used when --format is omitted.
const DefaultFormat = "{{.AttentionIcon}} {{.Attention}} {{.ActiveIcon}} {{.Active}} {{.IdleIcon}} {{.Idle}}"

// Options configures counts output.
type Options struct {
	JSON   bool
	Format string
}

// TemplateData exposes count and icon fields to --format templates.
type TemplateData struct {
	Attention     int
	Active        int
	Idle          int
	AttentionIcon string
	ActiveIcon    string
	IdleIcon      string
}

// LoadAndCount reads the registry and returns category totals for live pi sessions.
func LoadAndCount() (registry.CategoryCounts, error) {
	path, err := registry.DefaultPath()
	if err != nil {
		return registry.CategoryCounts{}, err
	}
	sessions, err := registry.Load(path)
	if err != nil {
		return registry.CategoryCounts{}, err
	}
	sanitized, _ := registry.Sanitize(sessions, tmux.RegistrySanitizeOptions(nil))
	return registry.CountByCategory(sanitized), nil
}

func templateData(counts registry.CategoryCounts) TemplateData {
	return TemplateData{
		Attention:     counts.Attention,
		Active:        counts.Active,
		Idle:          counts.Idle,
		AttentionIcon: iconAttention,
		ActiveIcon:    iconActive,
		IdleIcon:      iconIdle,
	}
}

// Write formats category totals to w.
func Write(w io.Writer, opts Options, counts registry.CategoryCounts) error {
	if opts.JSON {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		return enc.Encode(counts)
	}

	format := opts.Format
	if format == "" {
		format = DefaultFormat
	}

	tmpl, err := template.New("counts").Parse(format)
	if err != nil {
		return fmt.Errorf("parse format template: %w", err)
	}
	return tmpl.Execute(w, templateData(counts))
}

// Run loads the registry and writes formatted counts to w.
func Run(w io.Writer, opts Options) error {
	counts, err := LoadAndCount()
	if err != nil {
		return err
	}
	return Write(w, opts, counts)
}
