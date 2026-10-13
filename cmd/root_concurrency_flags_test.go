package cmd

import (
	"strings"
	"testing"
)

func TestConcurrencyFlagHelpAndDefaults(t *testing.T) {
	command, out, errOut := newTestRootCommand(t, newTestRootConfig(), newTestRootDeps())
	command.SetArgs([]string{"--help"})
	if err := command.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("help wrote stderr: %q", errOut.String())
	}
	for _, test := range []struct {
		name      string
		valueType string
		defValue  string
		shorthand string
	}{
		{name: "concurrency-targets", valueType: "int", defValue: "3", shorthand: "T"},
		{name: "concurrency-pages", valueType: "int", defValue: "1", shorthand: "P"},
		{name: "concurrency-http", valueType: "int", defValue: "0", shorthand: "H"},
		{name: "concurrency-adaptive", valueType: "bool", defValue: "false", shorthand: "A"},
	} {
		t.Run(test.name, func(t *testing.T) {
			flag := command.Flags().Lookup(test.name)
			if flag == nil {
				t.Fatalf("missing flag --%s", test.name)
			}
			if flag.Value.Type() != test.valueType || flag.DefValue != test.defValue || flag.Shorthand != test.shorthand {
				t.Fatalf("--%s: type=%q default=%q shorthand=%q, want %q %q %q", test.name, flag.Value.Type(), flag.DefValue, flag.Shorthand, test.valueType, test.defValue, test.shorthand)
			}
			if !strings.Contains(out.String(), "--"+test.name) {
				t.Fatalf("help does not list --%s: %s", test.name, out.String())
			}
			if !strings.Contains(out.String(), "-"+test.shorthand+", --"+test.name) {
				t.Fatalf("help does not pair -%s with --%s: %s", test.shorthand, test.name, out.String())
			}
		})
	}
	for _, token := range strings.Fields(out.String()) {
		switch token {
		case "-n,", "--concurrency", "--page-concurrency", "--http-concurrency", "--adaptive-concurrency", "--adaptive-http-concurrency":
			t.Errorf("help lists removed flag %s", token)
		}
	}
}
