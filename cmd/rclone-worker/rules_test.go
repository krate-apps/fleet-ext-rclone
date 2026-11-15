package main

import (
	"path/filepath"
	"testing"
)

func TestParseRulesText(t *testing.T) {
	list, err := parseRulesText(`action=copy remote=lab dest={label}/{name} label=tv
# comment
action=move remote=backup dest={name}
`)
	if err != nil || len(list) != 2 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	r, ok := matchRule(list, "tv")
	if !ok || r.Remote != "lab" || r.Action != "copy" {
		t.Fatalf("%+v", r)
	}
	r2, ok := matchRule(list, "other")
	if !ok || r2.Action != "move" {
		t.Fatalf("expected fallback move rule %+v ok=%v", r2, ok)
	}
}

func TestRemoteAllow(t *testing.T) {
	m := parseRemotesAllow("lab\nBackup:\n")
	if !remoteAllowed(m, "lab") || !remoteAllowed(m, "backup") {
		t.Fatal(m)
	}
	if remoteAllowed(m, "evil") {
		t.Fatal("evil should be rejected")
	}
}

func TestUnderRoots(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "data", "downloads")
	ok := underRoots(filepath.Join(root, "show"), []string{root})
	if !ok {
		t.Fatal("expected under")
	}
	if underRoots(filepath.Join(string(filepath.Separator), "tmp", "x"), []string{root}) {
		t.Fatal("expected outside")
	}
}

func TestRenderDest(t *testing.T) {
	got := renderDest("{label}/{name}", "tv", "Show/S01")
	if got != "tv/Show_S01" {
		t.Fatalf("got %q", got)
	}
}
