package config

import (
	"fmt"
	"strings"
)

// SelectRules replaces all rule toggles with the requested set. Other config
// settings are preserved. IDs are case-insensitive; duplicates are harmless.
func (c *Config) SelectRules(ids []string) error {
	var rules Rules
	fields := map[string]**bool{
		"R1":  &rules.SwitchCaseSpacing,
		"R2":  &rules.FuncSignatureBodyBlank,
		"R3":  &rules.FuncDefWrap,
		"R4":  &rules.FuncCallWrap,
		"R5":  &rules.FormattingFnCompact,
		"R6":  &rules.IndentationSymmetry,
		"R7":  &rules.InlineCompositeLit,
		"R8":  &rules.StructuredLogWrap,
		"R9":  &rules.StringLitWrap,
		"R10": &rules.LineLengthCheck,
		"R11": &rules.StanzaSpacing,
		"R12": &rules.BodySplit,
		"R13": &rules.VarBlockWrap,
		"R14": &rules.DeclarationGrouping,
		"R15": &rules.CommentReflow,
		"R16": &rules.BinaryOpWrap,
	}
	for _, field := range fields {
		*field = new(bool)
	}
	if len(ids) == 0 {
		return fmt.Errorf("--rule requires at least one rule ID")
	}
	var selected []string
	for _, id := range ids {
		id = strings.ToUpper(strings.TrimSpace(id))
		field, ok := fields[id]
		if !ok {
			return fmt.Errorf("unknown selectable rule %q (use R1–R16; "+
				"try --rules to list)", id)
		}
		if !**field {
			**field = true
			selected = append(selected, id)
		}
	}
	c.Rules = rules
	c.SelectedRules = selected
	return nil
}
