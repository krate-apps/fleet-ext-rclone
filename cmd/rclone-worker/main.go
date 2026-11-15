package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      any             `json:"id"`
}

type settings struct {
	Enabled                 bool                `json:"enabled"`
	IntervalSec             int                 `json:"intervalSec"`
	RcloneBin               string              `json:"rcloneBin"`
	RemotesAllow            map[string]struct{} `json:"-"`
	RemotesAllowText        string              `json:"remotesAllowText"`
	Rules                   []rule              `json:"rules"`
	RulesText               string              `json:"rulesText"`
	DeleteLocalAfterVerify  bool                `json:"deleteLocalAfterVerify"`
}

type snapRow struct {
	Hash      string `json:"hash"`
	Name      string `json:"name"`
	Label     string `json:"label"`
	Complete  bool   `json:"complete"`
	Directory string `json:"directory"`
}

type journalEntry struct {
	Time    string `json:"time"`
	Title   string `json:"title"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
	JobID   string `json:"jobId,omitempty"`
}

type hostReply struct {
	result json.RawMessage
	err    string
}

type runningJob struct {
	cmd *exec.Cmd
}

var (
	mu       sync.Mutex
	nextHost = int64(1)
	pending  = map[int64]chan hostReply{}
	outMu    sync.Mutex

	runMu  sync.Mutex
	runMap = map[string]*runningJob{}

	pctRe = regexp.MustCompile(`([0-9]+(\.[0-9]+)?)%`)
)

const journalLimit = 100

func main() {
	go loopScan()
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for in.Scan() {
		line := append([]byte(nil), in.Bytes()...)
		if len(line) == 0 {
			continue
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		if _, hasMethod := probe["method"]; !hasMethod {
			var resp struct {
				ID     any             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(line, &resp); err != nil {
				continue
			}
			idNum, ok := asInt64(resp.ID)
			if !ok {
				continue
			}
			mu.Lock()
			ch := pending[idNum]
			delete(pending, idNum)
			mu.Unlock()
			if ch == nil {
				continue
			}
			msg := ""
			if resp.Error != nil {
				msg = resp.Error.Message
			}
			ch <- hostReply{result: resp.Result, err: msg}
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		go handleRequest(req)
	}
}

func handleRequest(req request) {
	switch req.Method {
	case "fleet.handshake", "fleet.ping", "ext.activate", "ext.deactivate", "ext.health", "ext.upgrade":
		reply(req.ID, map[string]any{"ok": true})
	case "fleet.shutdown":
		killAllJobs()
		reply(req.ID, map[string]any{"ok": true})
		os.Exit(0)
	case "ext.rclone.getSettings":
		st, err := loadSettings()
		if err != nil {
			replyErr(req.ID, err.Error())
			return
		}
		reply(req.ID, publicSettings(st))
	case "ext.rclone.saveSettings":
		handleSave(req.ID, req.Params)
	case "ext.rclone.scan":
		res, err := scanOnce()
		if err != nil {
			replyErr(req.ID, err.Error())
			return
		}
		reply(req.ID, res)
	case "ext.rclone.listJournal":
		list, err := loadJournal()
		if err != nil {
			replyErr(req.ID, err.Error())
			return
		}
		reply(req.ID, map[string]any{"entries": list})
	case "ext.rclone.clearJournal":
		if err := saveJournal(nil); err != nil {
			replyErr(req.ID, err.Error())
			return
		}
		reply(req.ID, map[string]any{"ok": true})
	case "ext.rclone.listJobs":
		runMu.Lock()
		ids := make([]string, 0, len(runMap))
		for id := range runMap {
			ids = append(ids, id)
		}
		runMu.Unlock()
		reply(req.ID, map[string]any{"running": ids})
	case "ext.rclone.cancelJob":
		var p struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(req.Params, &p)
		id := strings.TrimSpace(p.ID)
		if id == "" {
			replyErr(req.ID, "id required")
			return
		}
		killJob(id)
		_, _ = hostCall("fleet.jobs.cancel", map[string]any{"id": id})
		reply(req.ID, map[string]any{"ok": true, "id": id})
	default:
		replyErr(req.ID, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func handleSave(id any, params json.RawMessage) {
	var p struct {
		Enabled                bool   `json:"enabled"`
		IntervalSec            int    `json:"intervalSec"`
		RcloneBin              string `json:"rcloneBin"`
		RemotesAllowText       string `json:"remotesAllowText"`
		RulesText              string `json:"rulesText"`
		DeleteLocalAfterVerify bool   `json:"deleteLocalAfterVerify"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		replyErr(id, "invalid settings")
		return
	}
	rules, err := parseRulesText(p.RulesText)
	if err != nil {
		replyErr(id, err.Error())
		return
	}
	st := settings{
		Enabled:                p.Enabled,
		IntervalSec:            p.IntervalSec,
		RcloneBin:              strings.TrimSpace(p.RcloneBin),
		RemotesAllow:           parseRemotesAllow(p.RemotesAllowText),
		Rules:                  rules,
		DeleteLocalAfterVerify: p.DeleteLocalAfterVerify,
	}
	if st.IntervalSec < 5 || st.IntervalSec > 3600 {
		st.IntervalSec = 15
	}
	if err := saveSettings(&st); err != nil {
		replyErr(id, err.Error())
		return
	}
	reply(id, publicSettings(st))
}

func loopScan() {
	time.Sleep(3 * time.Second)
	for {
		st, err := loadSettings()
		interval := 15
		if err == nil && st.IntervalSec > 0 {
			interval = st.IntervalSec
		}
		if err == nil && st.Enabled {
			_, _ = scanOnce()
		}
		time.Sleep(time.Duration(interval) * time.Second)
	}
}

func scanOnce() (map[string]any, error) {
	st, err := loadSettings()
	if err != nil {
		return nil, err
	}
	raw, err := hostCall("fleet.torrents.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Torrents []snapRow `json:"torrents"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	prev, err := loadSnap()
	if err != nil {
		return nil, err
	}
	cur := map[string]snapRow{}
	started := 0
	first := len(prev) == 0
	for _, t := range payload.Torrents {
		hash := strings.ToUpper(strings.TrimSpace(t.Hash))
		if hash == "" {
			continue
		}
		t.Hash = hash
		cur[hash] = t
		if first {
			continue
		}
		old, ok := prev[hash]
		if !ok || old.Complete || !t.Complete {
			continue
		}
		if !st.Enabled {
			continue
		}
		r, ok := matchRule(st.Rules, t.Label)
		if !ok {
			continue
		}
		if err := startTransfer(st, t, r); err != nil {
			_ = appendJournal(journalEntry{
				Time: time.Now().UTC().Format(time.RFC3339), Title: t.Name,
				Outcome: "error", Reason: err.Error(),
			})
			continue
		}
		started++
	}
	if err := saveSnap(cur); err != nil {
		return nil, err
	}
	return map[string]any{"started": started, "tracked": len(cur), "prime": first}, nil
}

func startTransfer(st settings, t snapRow, r rule) error {
	bin, err := resolveRclone(st.RcloneBin)
	if err != nil {
		return err
	}
	if !remoteAllowed(st.RemotesAllow, r.Remote) {
		return fmt.Errorf("remote %q not allowlisted", r.Remote)
	}
	src := strings.TrimSpace(t.Directory)
	if src == "" {
		return fmt.Errorf("torrent directory empty")
	}
	src, err = filepath.Abs(src)
	if err != nil {
		return err
	}
	roots, err := loadRoots()
	if err != nil {
		return err
	}
	if !underRoots(src, roots) {
		return fmt.Errorf("path outside download roots")
	}
	destPath := renderDest(r.Dest, t.Label, t.Name)
	remote := strings.TrimSuffix(strings.TrimSpace(r.Remote), ":")
	dest := remote + ":" + destPath

	jobRaw, err := hostCall("fleet.jobs.create", map[string]any{
		"kind": "rclone.transfer",
		"payload": map[string]any{
			"hash":   t.Hash,
			"name":   t.Name,
			"action": r.Action,
			"src":    src,
			"dest":   dest,
		},
	})
	if err != nil {
		return err
	}
	var job struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(jobRaw, &job); err != nil || job.ID == "" {
		return fmt.Errorf("invalid job create response")
	}
	_, _ = hostCall("fleet.jobs.update", map[string]any{
		"id": job.ID, "status": "running", "message": r.Action + " " + dest, "progress": 0.0,
	})

	go runRcloneJob(bin, job.ID, r.Action, src, dest, st.DeleteLocalAfterVerify && r.Action == "copy", roots, t.Name)
	_ = appendJournal(journalEntry{
		Time: time.Now().UTC().Format(time.RFC3339), Title: t.Name,
		Outcome: "ok", Reason: r.Action + " → " + dest, JobID: job.ID,
	})
	return nil
}

func runRcloneJob(bin, jobID, action, src, dest string, deleteAfter bool, roots []string, title string) {
	args := []string{action, src, dest, "--stats", "1s", "--stats-one-line", "-v"}
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_, _ = hostCall("fleet.jobs.update", map[string]any{"id": jobID, "status": "failed", "error": err.Error()})
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		_, _ = hostCall("fleet.jobs.update", map[string]any{"id": jobID, "status": "failed", "error": err.Error()})
		return
	}
	runMu.Lock()
	runMap[jobID] = &runningJob{cmd: cmd}
	runMu.Unlock()

	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			prog := parseProgress(line)
			_, _ = hostCall("fleet.jobs.update", map[string]any{
				"id": jobID, "status": "running", "message": truncate(line, 200), "progress": prog,
			})
		}
	}()

	waitErr := cmd.Wait()
	runMu.Lock()
	delete(runMap, jobID)
	runMu.Unlock()

	if waitErr != nil {
		msg := waitErr.Error()
		if isCancelled(jobID) {
			msg = "cancelled"
			_, _ = hostCall("fleet.jobs.update", map[string]any{"id": jobID, "status": "cancelled", "message": msg})
			return
		}
		_, _ = hostCall("fleet.jobs.update", map[string]any{"id": jobID, "status": "failed", "error": msg})
		_ = appendJournal(journalEntry{
			Time: time.Now().UTC().Format(time.RFC3339), Title: title,
			Outcome: "error", Reason: msg, JobID: jobID,
		})
		return
	}

	if deleteAfter {
		if err := verifyAndDelete(bin, src, dest, roots); err != nil {
			_, _ = hostCall("fleet.jobs.update", map[string]any{
				"id": jobID, "status": "failed", "error": "verify/delete: " + err.Error(),
			})
			return
		}
	}
	_, _ = hostCall("fleet.jobs.update", map[string]any{
		"id": jobID, "status": "succeeded", "progress": 1.0, "result": map[string]any{"dest": dest},
	})
}

func verifyAndDelete(bin, src, dest string, roots []string) error {
	check := exec.Command(bin, "check", src, dest)
	if out, err := check.CombinedOutput(); err != nil {
		return fmt.Errorf("rclone check: %w (%s)", err, truncate(string(out), 200))
	}
	if !underRoots(src, roots) {
		return fmt.Errorf("refuse delete outside roots")
	}
	_, err := hostCall("fleet.files.remove", map[string]any{"path": src, "recursive": true})
	return err
}

func parseProgress(line string) float64 {
	m := pctRe.FindStringSubmatch(line)
	if len(m) < 2 {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	if v > 100 {
		v = 100
	}
	return v / 100
}

func resolveRclone(override string) (string, error) {
	if o := strings.TrimSpace(override); o != "" {
		if _, err := os.Stat(o); err != nil {
			return "", fmt.Errorf("rclone binary not found: %s", o)
		}
		return o, nil
	}
	if env := strings.TrimSpace(os.Getenv("FLEET_TOOL_RCLONE")); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
		if p, err := exec.LookPath(env); err == nil {
			return p, nil
		}
	}
	p, err := exec.LookPath("rclone")
	if err != nil {
		return "", fmt.Errorf("rclone not found (set tools.rclone or install on PATH)")
	}
	return p, nil
}

func loadRoots() ([]string, error) {
	raw, err := hostCall("fleet.files.roots", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Roots []string `json:"roots"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Roots, nil
}

func killJob(id string) {
	runMu.Lock()
	rj := runMap[id]
	runMu.Unlock()
	if rj == nil || rj.cmd == nil || rj.cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(rj.cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = rj.cmd.Process.Kill()
	}
}

func killAllJobs() {
	runMu.Lock()
	ids := make([]string, 0, len(runMap))
	for id := range runMap {
		ids = append(ids, id)
	}
	runMu.Unlock()
	for _, id := range ids {
		killJob(id)
	}
}

func isCancelled(jobID string) bool {
	raw, err := hostCall("fleet.jobs.get", map[string]any{"id": jobID})
	if err != nil {
		return false
	}
	var j struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(raw, &j)
	return j.Status == "cancelled"
}

func defaultSettings() settings {
	return settings{
		Enabled:      false,
		IntervalSec:  15,
		RemotesAllow: map[string]struct{}{},
		Rules:        nil,
	}
}

func publicSettings(st settings) map[string]any {
	return map[string]any{
		"enabled":                st.Enabled,
		"intervalSec":            st.IntervalSec,
		"rcloneBin":              st.RcloneBin,
		"remotesAllowText":       remotesAllowText(st.RemotesAllow),
		"rulesText":              rulesText(st.Rules),
		"deleteLocalAfterVerify": st.DeleteLocalAfterVerify,
		"rcloneResolved":         mustResolve(st.RcloneBin),
	}
}

func mustResolve(override string) string {
	p, err := resolveRclone(override)
	if err != nil {
		return ""
	}
	return p
}

func loadSettings() (settings, error) {
	raw, err := hostCall("fleet.storage.get", map[string]any{"key": "settings"})
	if err != nil {
		return settings{}, err
	}
	if string(raw) == "null" || len(raw) == 0 {
		return defaultSettings(), nil
	}
	var wire struct {
		Enabled                bool   `json:"enabled"`
		IntervalSec            int    `json:"intervalSec"`
		RcloneBin              string `json:"rcloneBin"`
		RemotesAllowText       string `json:"remotesAllowText"`
		RulesText              string `json:"rulesText"`
		DeleteLocalAfterVerify bool   `json:"deleteLocalAfterVerify"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return defaultSettings(), nil
	}
	rules, _ := parseRulesText(wire.RulesText)
	st := settings{
		Enabled:                wire.Enabled,
		IntervalSec:            wire.IntervalSec,
		RcloneBin:              wire.RcloneBin,
		RemotesAllow:           parseRemotesAllow(wire.RemotesAllowText),
		Rules:                  rules,
		DeleteLocalAfterVerify: wire.DeleteLocalAfterVerify,
	}
	if st.IntervalSec == 0 {
		st.IntervalSec = 15
	}
	return st, nil
}

func saveSettings(st *settings) error {
	wire := map[string]any{
		"enabled":                st.Enabled,
		"intervalSec":            st.IntervalSec,
		"rcloneBin":              st.RcloneBin,
		"remotesAllowText":       remotesAllowText(st.RemotesAllow),
		"rulesText":              rulesText(st.Rules),
		"deleteLocalAfterVerify": st.DeleteLocalAfterVerify,
	}
	_, err := hostCall("fleet.storage.put", map[string]any{"key": "settings", "value": wire})
	return err
}

func loadSnap() (map[string]snapRow, error) {
	raw, err := hostCall("fleet.storage.get", map[string]any{"key": "snapshot"})
	if err != nil {
		return nil, err
	}
	out := map[string]snapRow{}
	if string(raw) == "null" || len(raw) == 0 {
		return out, nil
	}
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

func saveSnap(snap map[string]snapRow) error {
	_, err := hostCall("fleet.storage.put", map[string]any{"key": "snapshot", "value": snap})
	return err
}

func loadJournal() ([]journalEntry, error) {
	raw, err := hostCall("fleet.storage.get", map[string]any{"key": "journal"})
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" || len(raw) == 0 {
		return []journalEntry{}, nil
	}
	var list []journalEntry
	_ = json.Unmarshal(raw, &list)
	return list, nil
}

func saveJournal(list []journalEntry) error {
	if list == nil {
		list = []journalEntry{}
	}
	_, err := hostCall("fleet.storage.put", map[string]any{"key": "journal", "value": list})
	return err
}

func appendJournal(e journalEntry) error {
	list, err := loadJournal()
	if err != nil {
		return err
	}
	list = append([]journalEntry{e}, list...)
	if len(list) > journalLimit {
		list = list[:journalLimit]
	}
	return saveJournal(list)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func hostCall(method string, params any) (json.RawMessage, error) {
	ch := make(chan hostReply, 1)
	mu.Lock()
	id := nextHost
	nextHost++
	pending[id] = ch
	mu.Unlock()
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params, "id": id})
	if err != nil {
		mu.Lock()
		delete(pending, id)
		mu.Unlock()
		return nil, err
	}
	outMu.Lock()
	fmt.Fprintf(os.Stdout, "%s\n", b)
	outMu.Unlock()
	select {
	case reply := <-ch:
		if reply.err != "" {
			return nil, fmt.Errorf("%s", reply.err)
		}
		if len(reply.result) == 0 {
			return json.RawMessage("null"), nil
		}
		return reply.result, nil
	case <-time.After(60 * time.Second):
		mu.Lock()
		delete(pending, id)
		mu.Unlock()
		return nil, fmt.Errorf("host call timeout")
	}
}

func reply(id any, result any) {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "result": result, "id": id})
	outMu.Lock()
	fmt.Fprintf(os.Stdout, "%s\n", b)
	outMu.Unlock()
}

func replyErr(id any, msg string) {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"error":   map[string]any{"code": -32000, "message": msg},
		"id":      id,
	})
	outMu.Lock()
	fmt.Fprintf(os.Stdout, "%s\n", b)
	outMu.Unlock()
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}
