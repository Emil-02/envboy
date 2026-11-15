package check_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Emil-02/envboy/internal/check"
	"github.com/Emil-02/envboy/internal/config"
	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/testutil"
)

func TestCheck(t *testing.T) {
	root := t.TempDir()
	testutil.Write(t, filepath.Join(root, "app/.env.example"), "A=\nB=\nC=default\n")
	testutil.Write(t, filepath.Join(root, "app/.env"), "A=1\nB=\nB=set-later\nEXTRA=1\nE=\n")
	testutil.Write(t, filepath.Join(root, "app/.env.test"), "A=1\nB=2\nC=3\n")
	testutil.Write(t, filepath.Join(root, "app/.env.local.example"), "L=\n")
	testutil.Write(t, filepath.Join(root, "app/.env.local"), "L=x\n")
	testutil.Write(t, filepath.Join(root, "svc/.env.sample"), "S=\n")
	testutil.Write(t, filepath.Join(root, "lonely/.env"), "Z=1\n")

	repos, err := discover.Scan([]string{root}, discover.FromConfig(config.Default()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]check.Finding{}
	for _, f := range check.All(repos) {
		got[f.Repo+"/"+f.File+"<"+f.Template] = f
	}
	if len(got) != 4 {
		t.Fatalf("want 4 findings, got %d: %+v", len(got), got)
	}

	main := got["app/.env<.env.example"]
	if !reflect.DeepEqual(main.Missing, []string{"C"}) || !reflect.DeepEqual(main.Extra, []string{"E", "EXTRA"}) ||
		!reflect.DeepEqual(main.Empty, []string{"E"}) {
		t.Errorf(".env finding = %+v", main)
	}
	if !main.Failed(false) {
		t.Error("missing keys must fail")
	}

	if f := got["app/.env.test<.env.example"]; !f.OK() || f.Failed(true) {
		t.Errorf(".env.test should be clean: %+v", f)
	}
	if f := got["app/.env.local<.env.local.example"]; !f.OK() {
		t.Errorf(".env.local should use its own template: %+v", f)
	}
	f := got["svc/<.env.sample"]
	if !f.NoEnvFile || f.Failed(false) || !f.Failed(true) {
		t.Errorf("template without env: %+v", f)
	}
}
