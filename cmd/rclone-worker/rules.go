package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

type rule struct {
	Label  string
	Action string // copy | move
	Remote string
	Dest   string // template with {label}/{name}
}

func parseRulesText(text string) ([]rule, error) {
	out := make([]rule, 0)
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := rule{Action: "copy", Dest: "{name}"}
		for _, part := range strings.Fields(line) {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				return nil, fmt.Errorf("line %d: expected key=value tokens", i+1)
			}
			k := strings.ToLower(strings.TrimSpace(kv[0]))
			v := strings.TrimSpace(kv[1])
			switch k {
			case "label":
				r.Label = v
			case "action":
				r.Action = strings.ToLower(v)
			case "remote":
				r.Remote = v
			case "dest":
				r.Dest = v
			default:
				return nil, fmt.Errorf("line %d: unknown key %q", i+1, k)
			}
		}
		if r.Remote == "" {
			return nil, fmt.Errorf("line %d: remote required", i+1)
		}
		if r.Action != "copy" && r.Action != "move" {
			return nil, fmt.Errorf("line %d: action must be copy or move", i+1)
		}
		if strings.TrimSpace(r.Dest) == "" {
			r.Dest = "{name}"
		}
		out = append(out, r)
	}
	return out, nil
}

func rulesText(list []rule) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range list {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "action=%s remote=%s dest=%s", r.Action, r.Remote, r.Dest)
		if r.Label != "" {
			fmt.Fprintf(&b, " label=%s", r.Label)
		}
	}
	return b.String()
}

func matchRule(rules []rule, label string) (rule, bool) {
	for _, r := range rules {
		if r.Label == "" || strings.EqualFold(r.Label, label) {
			return r, true
		}
	}
	return rule{}, false
}

func parseRemotesAllow(text string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Accept "lab" or "lab:" forms.
		line = strings.TrimSuffix(line, ":")
		if line == "" {
			continue
		}
		out[strings.ToLower(line)] = struct{}{}
	}
	return out
}

func remotesAllowText(m map[string]struct{}) string {
	if len(m) == 0 {
		return ""
	}
	var b strings.Builder
	first := true
	for k := range m {
		if !first {
			b.WriteByte('\n')
		}
		first = false
		b.WriteString(k)
	}
	return b.String()
}

func remoteAllowed(allow map[string]struct{}, remote string) bool {
	key := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(remote, ":")))
	if key == "" {
		return false
	}
	_, ok := allow[key]
	return ok
}

func renderDest(tmpl, label, name string) string {
	s := tmpl
	s = strings.ReplaceAll(s, "{label}", sanitizePathPart(label))
	s = strings.ReplaceAll(s, "{name}", sanitizePathPart(name))
	s = strings.ReplaceAll(s, "\\", "/")
	s = strings.Trim(s, "/")
	return s
}

func sanitizePathPart(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "..", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, ":", "_")
	if s == "" {
		return "_"
	}
	return s
}

func underRoots(abs string, roots []string) bool {
	clean := filepath.Clean(abs)
	for _, root := range roots {
		root = filepath.Clean(root)
		if root == "" {
			continue
		}
		if clean == root || strings.HasPrefix(clean, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
