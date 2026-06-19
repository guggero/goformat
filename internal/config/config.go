// Package config loads goformat configuration from TOML and applies the
// lnd-style defaults documented in code-ingest/development_guidelines.md.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const FileName = "goformat.toml"

type Config struct {
	LineLength   int  `toml:"line_length"`
	TabWidth     int  `toml:"tab_width"`
	Canonicalize bool `toml:"canonicalize"`

	FormattingFuncs     []string `toml:"formatting_funcs"`
	FormattingFuncsDeny []string `toml:"formatting_funcs_deny"`

	// StructuredLogMethods lists method names that, when called as
	// pkg.Method(ctx, "msg", ...), are treated as structured-log calls
	// subject to R8. Default matches btclog conventions: TraceS, DebugS,
	// InfoS, WarnS, ErrorS, CriticalS.
	StructuredLogMethods []string `toml:"structured_log_methods"`

	Rules Rules `toml:"rules"`
}

// Rules holds per-pass enable toggles. Each field is a pointer so that "absent
// in TOML" is distinguishable from "explicitly false". An unset rule defaults
// to enabled.
type Rules struct {
	SwitchCaseSpacing      *bool `toml:"switch_case_spacing"`
	FuncSignatureBodyBlank *bool `toml:"func_signature_body_blank"`
	FuncDefWrap            *bool `toml:"func_def_wrap"`
	FuncCallWrap           *bool `toml:"func_call_wrap"`
	FormattingFnCompact    *bool `toml:"formatting_fn_compact"`
	StructuredLogWrap      *bool `toml:"structured_log_wrap"`
	InlineCompositeLit     *bool `toml:"inline_composite_lit"`
	StringLitWrap          *bool `toml:"string_lit_wrap"`
	StanzaSpacing          *bool `toml:"stanza_spacing"`
	BodySplit              *bool `toml:"body_split"`
	VarBlockWrap           *bool `toml:"var_block_wrap"`
	CommentReflow          *bool `toml:"comment_reflow"`
	BinaryOpWrap           *bool `toml:"binary_op_wrap"`
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func (r Rules) SwitchCaseSpacingOn() bool {
	return boolOr(r.SwitchCaseSpacing, true)
}
func (r Rules) FuncSignatureBodyBlankOn() bool {
	return boolOr(r.FuncSignatureBodyBlank, true)
}
func (r Rules) FuncDefWrapOn() bool {
	return boolOr(r.FuncDefWrap, true)
}
func (r Rules) FuncCallWrapOn() bool {
	return boolOr(r.FuncCallWrap, true)
}
func (r Rules) FormattingFnCompactOn() bool {
	return boolOr(r.FormattingFnCompact, true)
}
func (r Rules) StructuredLogWrapOn() bool {
	return boolOr(r.StructuredLogWrap, true)
}
func (r Rules) InlineCompositeLitOn() bool {
	return boolOr(r.InlineCompositeLit, true)
}
func (r Rules) StringLitWrapOn() bool {
	return boolOr(r.StringLitWrap, true)
}
func (r Rules) StanzaSpacingOn() bool {
	return boolOr(r.StanzaSpacing, true)
}
func (r Rules) BodySplitOn() bool {
	return boolOr(r.BodySplit, true)
}
func (r Rules) VarBlockWrapOn() bool {
	return boolOr(r.VarBlockWrap, true)
}
func (r Rules) CommentReflowOn() bool {
	return boolOr(r.CommentReflow, true)
}
func (r Rules) BinaryOpWrapOn() bool {
	return boolOr(r.BinaryOpWrap, true)
}

func Default() *Config {
	return &Config{
		LineLength:   80,
		TabWidth:     8,
		Canonicalize: false,
		FormattingFuncs: []string{
			"fmt.Errorf",
			"fmt.Printf",
			"fmt.Sprintf",
			"log.Tracef",
			"log.Debugf",
			"log.Infof",
			"log.Warnf",
			"log.Errorf",
			"log.Criticalf",
			"t.Errorf",
			"t.Fatalf",
			"require.NoErrorf",
			"require.Errorf",
			"assert.Errorf",
			"t.Logf",
		},
		FormattingFuncsDeny: []string{},
		StructuredLogMethods: []string{
			"TraceS", "DebugS", "InfoS", "WarnS", "ErrorS",
			"CriticalS",
		},
	}
}

// Load overlays the TOML file at path on Default(). A missing file is not an
// error; defaults are returned unchanged.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	_, err := toml.DecodeFile(path, cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// Find walks upward from start looking for goformat.toml. Returns "" if none is
// found before the filesystem root.
func Find(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, FileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
