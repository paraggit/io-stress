package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/spf13/pflag"
)

func TestOptionsCommandMatchesFlagsAndGuide(t *testing.T) {
	root := newRootCmd()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"options"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	var doc config.OptionsDocument
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("options JSON: %v\n%s", err, stdout.Bytes())
	}
	if doc.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d", doc.SchemaVersion)
	}
	wantRecipes := []string{"smoke", "write-verify", "app-vm", "app-database", "full"}
	if len(doc.Recipes) != len(wantRecipes) {
		t.Fatalf("recipes = %d, want %d", len(doc.Recipes), len(wantRecipes))
	}
	for i, name := range wantRecipes {
		r := doc.Recipes[i]
		if r.Name != name || len(r.Argv) == 0 || r.DurationSeconds <= 0 || !r.DeletesNamespace {
			t.Fatalf("recipe[%d] = %+v", i, r)
		}
	}
	if doc.Recipes[0].DurationSeconds >= doc.Recipes[len(doc.Recipes)-1].DurationSeconds {
		t.Fatalf("smoke duration %d should be below full %d", doc.Recipes[0].DurationSeconds, doc.Recipes[len(doc.Recipes)-1].DurationSeconds)
	}
	for _, want := range []string{"nfs", "vm", "database", "messaging", "ai"} {
		if !contains(doc.AppTypes, want) {
			t.Errorf("appTypes missing %s: %v", want, doc.AppTypes)
		}
	}

	guide, err := os.ReadFile("../../AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range doc.Rules {
		if !bytes.Contains(guide, []byte(rule)) {
			t.Errorf("AGENTS.md missing rule: %s", rule)
		}
	}
	if len(doc.Rules) == 0 {
		t.Fatal("options JSON has no rules")
	}

	runCmd, _, err := root.Find([]string{"run"})
	if err != nil {
		t.Fatal(err)
	}
	genCmd, _, err := root.Find([]string{"generate-config"})
	if err != nil {
		t.Fatal(err)
	}
	assertFlags(t, guide, doc.Run, runCmd.Flags())
	assertFlags(t, guide, doc.GenerateConfig, genCmd.Flags())

	shorts := map[string]string{}
	for _, f := range append(append([]config.FlagInfo{}, doc.Run...), doc.GenerateConfig...) {
		if f.Shorthand != "" {
			shorts[f.Name] = f.Shorthand
		}
	}
	for name, short := range map[string]string{
		"num-pvc": "n", "namespace": "N", "image": "i", "runtime": "r",
		"bs": "b", "prefix": "p", "timeout": "t", "format": "f", "output": "o",
	} {
		if shorts[name] != short {
			t.Errorf("shorthand %s = %q, want %q", name, shorts[name], short)
		}
	}
}

func assertFlags(t *testing.T, guide []byte, listed []config.FlagInfo, fs *pflag.FlagSet) {
	t.Helper()
	got := map[string]config.FlagInfo{}
	for _, f := range listed {
		got[f.Name] = f
	}
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		info, ok := got[f.Name]
		if !ok {
			t.Errorf("options JSON missing --%s", f.Name)
			return
		}
		if info.Usage != f.Usage || info.Default != f.DefValue {
			t.Errorf("--%s help/default = %q %q, flag has %q %q", f.Name, info.Usage, info.Default, f.Usage, f.DefValue)
		}
		if !bytes.Contains(guide, []byte("--"+f.Name)) {
			t.Errorf("AGENTS.md missing --%s", f.Name)
		}
	})
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
