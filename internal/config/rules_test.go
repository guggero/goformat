package config_test

import (
	"reflect"
	"testing"

	"github.com/guggero/goformat/internal/config"
	"github.com/guggero/goformat/internal/format"
)

func TestSelectableRulesCoverRegistry(t *testing.T) {
	var ids []string
	for _, rule := range format.Rules() {
		if rule.ID != "noformat" {
			ids = append(ids, rule.ID)
		}
	}
	cfg := config.Default()
	cfg.LineLength = 100
	cfg.TabWidth = 4
	cfg.Optimize = true
	if err := cfg.SelectRules(ids); err != nil {
		t.Fatal(err)
	}

	// Every config toggle must participate in explicit selection, including
	// new rules added to either the registry or configuration in the
	// future.
	fields := reflect.ValueOf(cfg.Rules)
	for i := 0; i < fields.NumField(); i++ {
		if fields.Field(i).IsNil() || !fields.Field(i).Elem().Bool() {
			t.Errorf("rule toggle %s not selected", fields.Type().Field(i).Name)
		}
	}
	if err := cfg.SelectRules([]string{" r15 ", "R15"}); err != nil {
		t.Fatal(err)
	}
	fields = reflect.ValueOf(cfg.Rules)
	for i := 0; i < fields.NumField(); i++ {
		field := fields.Field(i)
		want := fields.Type().Field(i).Name == "CommentReflow"
		if field.IsNil() || field.Elem().Bool() != want {
			t.Errorf("unexpected toggle %s", fields.Type().Field(
				i,
			).Name)
		}
	}
	if cfg.LineLength != 100 || cfg.TabWidth != 4 || !cfg.Optimize {
		t.Fatal("non-rule config settings changed")
	}
	if !reflect.DeepEqual(cfg.SelectedRules, []string{"R15"}) {
		t.Fatalf("selection: %v", cfg.SelectedRules)
	}
}
