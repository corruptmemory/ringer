package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFromBytesValid(t *testing.T) {
	m, err := FromBytes([]byte(`{
		"run_name":"demo","workdir":"/tmp/x","max_parallel":3,
		"tasks":[{"key":"alpha","spec":"do it","check":"test -f alpha.txt","expect_files":["alpha.txt"]}]
	}`))
	if err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	if m.RunName != "demo" || len(m.Tasks) != 1 || m.Tasks[0].Key != "alpha" {
		t.Fatalf("parsed wrong: %+v", m)
	}
}

func TestFromBytesValidation(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"no run_name", `{"workdir":"/x","tasks":[{"key":"a","spec":"s","check":"c"}]}`, "run_name"},
		{"no workdir", `{"run_name":"r","tasks":[{"key":"a","spec":"s","check":"c"}]}`, "workdir"},
		{"no tasks", `{"run_name":"r","workdir":"/x","tasks":[]}`, "at least one task"},
		{"dup key", `{"run_name":"r","workdir":"/x","tasks":[{"key":"a","spec":"s","check":"c"},{"key":"a","spec":"s","check":"c"}]}`, "duplicate"},
		{"empty key", `{"run_name":"r","workdir":"/x","tasks":[{"key":"","spec":"s","check":"c"}]}`, "key"},
		{"no spec", `{"run_name":"r","workdir":"/x","tasks":[{"key":"a","check":"c"}]}`, "spec"},
		{"no check", `{"run_name":"r","workdir":"/x","tasks":[{"key":"a","spec":"s"}]}`, "check"},
		{"bad json", `{not json`, "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FromBytes([]byte(tc.body))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q missing %q", err, tc.want)
			}
		})
	}
}

func TestWorktreesValidation(t *testing.T) {
	base := func(extra string) []byte {
		return []byte(`{
			"run_name": "wt", "workdir": "/tmp/wt-work", ` + extra + `
			"tasks": [{"key": "t1", "spec": "do the thing", "check": "true"}]
		}`)
	}
	// worktrees + repo: valid now (Plan 3).
	if _, err := FromBytes(base(`"worktrees": true, "repo": "/tmp/parent-repo",`)); err != nil {
		t.Fatalf("worktrees+repo must validate in Plan 3: %v", err)
	}
	// worktrees without repo: loud failure, not Python's silent downgrade.
	if _, err := FromBytes(base(`"worktrees": true,`)); err == nil || !strings.Contains(err.Error(), "repo") {
		t.Fatalf("worktrees without repo: err = %v, want repo requirement", err)
	}
}

func TestTaskKeyEscapesWorkdir(t *testing.T) {
	body := []byte(`{
		"run_name": "esc", "workdir": "/tmp/esc-work",
		"tasks": [{"key": "../evil", "spec": "do the thing", "check": "true"}]
	}`)
	if _, err := FromBytes(body); err == nil || !strings.Contains(err.Error(), "escapes workdir") {
		t.Fatalf("err = %v, want escape rejection", err)
	}
}

// TestPathsNormalizedAtLoad guards against the taskDir-resolved-twice bug:
// `git -C <repo> worktree add <taskdir>` resolves a relative taskdir against
// <repo> (git chdirs first), while Go's os.MkdirAll/os.Stat/cmd.Dir resolve
// it against the process CWD. ringer.py:489,494 prevents the split by
// normalizing workdir/repo with Path(...).expanduser().resolve() at manifest
// load time; FromBytes must mirror that.
func TestPathsNormalizedAtLoad(t *testing.T) {
	body := []byte(`{
		"run_name": "norm", "workdir": "rel/work",
		"worktrees": true, "repo": "~/some-repo",
		"tasks": [{"key": "t1", "spec": "do the thing", "check": "true"}]
	}`)
	m, err := FromBytes(body)
	if err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	if !filepath.IsAbs(m.Workdir) {
		t.Errorf("workdir not made absolute: %q", m.Workdir)
	}
	if !strings.HasSuffix(m.Workdir, filepath.Join("rel", "work")) {
		t.Errorf("workdir = %q, want suffix %q", m.Workdir, filepath.Join("rel", "work"))
	}
	if !filepath.IsAbs(m.Repo) {
		t.Errorf("repo not made absolute: %q", m.Repo)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	if !strings.HasPrefix(m.Repo, home) {
		t.Errorf("repo = %q, want ~ expanded under home %q", m.Repo, home)
	}
}

func TestTaskKeyReservedLogsDir(t *testing.T) {
	for _, key := range []string{"logs", "logs/nested"} {
		body := []byte(`{
			"run_name": "logs-clash", "workdir": "/tmp/lg-work",
			"tasks": [{"key": "` + key + `", "spec": "do the thing", "check": "true"}]
		}`)
		if _, err := FromBytes(body); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("key %q: err = %v, want reserved-logs rejection", key, err)
		}
	}
	// "logs-report" merely shares the prefix — must stay valid.
	body := []byte(`{
		"run_name": "ok", "workdir": "/tmp/ok-work",
		"tasks": [{"key": "logs-report", "spec": "do the thing", "check": "true"}]
	}`)
	if _, err := FromBytes(body); err != nil {
		t.Fatalf("logs-report is not reserved: %v", err)
	}
}
