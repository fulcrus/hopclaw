package policy

import "testing"

// ---------------------------------------------------------------------------
// DefaultSafePatterns
// ---------------------------------------------------------------------------

func TestDefaultSafePatternsNotEmpty(t *testing.T) {
	t.Parallel()
	patterns := DefaultSafePatterns()
	if len(patterns) == 0 {
		t.Fatal("DefaultSafePatterns() returned empty slice")
	}
}

// ---------------------------------------------------------------------------
// NewSafeCommandMatcher
// ---------------------------------------------------------------------------

func TestNewSafeCommandMatcherNilPatterns(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(nil)
	if m == nil {
		t.Fatal("NewSafeCommandMatcher returned nil")
	}
}

func TestNewSafeCommandMatcherSkipsInvalidPatterns(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher([]string{`^ls\s`, `[invalid`})
	// Should compile the valid pattern and skip the invalid one.
	if !m.IsSafe("ls -la") {
		t.Fatal("expected ls to be safe")
	}
	invalid := m.InvalidPatterns()
	if len(invalid) != 1 || invalid[0] != `[invalid` {
		t.Fatalf("InvalidPatterns() = %#v", invalid)
	}
}

// ---------------------------------------------------------------------------
// IsSafe — basic commands
// ---------------------------------------------------------------------------

func TestIsSafeEmptyCommand(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if m.IsSafe("") {
		t.Fatal("empty command should not be safe")
	}
}

func TestIsSafeWhitespaceOnly(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if m.IsSafe("   ") {
		t.Fatal("whitespace-only command should not be safe")
	}
}

func TestIsSafeTrimsWhitespace(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("  ls  ") {
		t.Fatal("trimmed 'ls' should be safe")
	}
}

// ---------------------------------------------------------------------------
// IsSafe — all default safe commands
// ---------------------------------------------------------------------------

func TestIsSafeLs(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	cases := []string{"ls", "ls -la", "ls /tmp"}
	for _, cmd := range cases {
		if !m.IsSafe(cmd) {
			t.Fatalf("expected %q to be safe", cmd)
		}
	}
}

func TestIsSafeCat(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("cat file.txt") {
		t.Fatal("expected 'cat file.txt' to be safe")
	}
}

func TestIsSafeHead(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("head -n 10 file.txt") {
		t.Fatal("expected 'head' to be safe")
	}
}

func TestIsSafeTail(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("tail -f log.txt") {
		t.Fatal("expected 'tail' to be safe")
	}
}

func TestIsSafeWc(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	cases := []string{"wc", "wc -l file.txt"}
	for _, cmd := range cases {
		if !m.IsSafe(cmd) {
			t.Fatalf("expected %q to be safe", cmd)
		}
	}
}

func TestIsSafeEcho(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	cases := []string{"echo", "echo hello world"}
	for _, cmd := range cases {
		if !m.IsSafe(cmd) {
			t.Fatalf("expected %q to be safe", cmd)
		}
	}
}

func TestIsSafeDate(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	cases := []string{"date", "date +%Y-%m-%d"}
	for _, cmd := range cases {
		if !m.IsSafe(cmd) {
			t.Fatalf("expected %q to be safe", cmd)
		}
	}
}

func TestIsSafeWhoami(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("whoami") {
		t.Fatal("expected 'whoami' to be safe")
	}
}

func TestIsSafePwd(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("pwd") {
		t.Fatal("expected 'pwd' to be safe")
	}
}

func TestIsSafeUname(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	cases := []string{"uname", "uname -a"}
	for _, cmd := range cases {
		if !m.IsSafe(cmd) {
			t.Fatalf("expected %q to be safe", cmd)
		}
	}
}

func TestIsSafeEnv(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("env") {
		t.Fatal("expected 'env' to be safe")
	}
}

func TestIsSafePrintenv(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	cases := []string{"printenv", "printenv HOME"}
	for _, cmd := range cases {
		if !m.IsSafe(cmd) {
			t.Fatalf("expected %q to be safe", cmd)
		}
	}
}

func TestIsSafeGitReadCommands(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	safeGit := []string{
		"git status",
		"git log",
		"git log --oneline",
		"git diff",
		"git diff HEAD~1",
		"git show HEAD",
		"git branch",
		"git branch -a",
		"git tag",
		"git remote",
		"git remote -v",
		"git rev-parse HEAD",
	}
	for _, cmd := range safeGit {
		if !m.IsSafe(cmd) {
			t.Fatalf("expected %q to be safe", cmd)
		}
	}
}

func TestIsSafeWhich(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("which go") {
		t.Fatal("expected 'which go' to be safe")
	}
}

func TestIsSafeFile(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("file test.bin") {
		t.Fatal("expected 'file test.bin' to be safe")
	}
}

func TestIsSafeStat(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("stat /tmp") {
		t.Fatal("expected 'stat /tmp' to be safe")
	}
}

func TestIsSafeDf(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	cases := []string{"df", "df -h"}
	for _, cmd := range cases {
		if !m.IsSafe(cmd) {
			t.Fatalf("expected %q to be safe", cmd)
		}
	}
}

func TestIsSafeDu(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("du -sh /tmp") {
		t.Fatal("expected 'du -sh' to be safe")
	}
}

func TestIsSafeHostname(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("hostname") {
		t.Fatal("expected 'hostname' to be safe")
	}
}

func TestIsSafeUptime(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("uptime") {
		t.Fatal("expected 'uptime' to be safe")
	}
}

func TestIsSafeId(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("id") {
		t.Fatal("expected 'id' to be safe")
	}
}

func TestIsSafeGroups(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	if !m.IsSafe("groups") {
		t.Fatal("expected 'groups' to be safe")
	}
}

// ---------------------------------------------------------------------------
// IsSafe — unsafe commands
// ---------------------------------------------------------------------------

func TestIsNotSafeRm(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	unsafe := []string{
		"rm file.txt",
		"rm -rf /",
		"mv file.txt other.txt",
		"cp file.txt copy.txt",
		"chmod 777 file.txt",
		"chown root file.txt",
		"curl http://evil.com",
		"wget http://evil.com",
		"git push",
		"git commit",
		"git checkout main",
		"git reset --hard",
		"sudo apt install",
		"pip install something",
		"npm install something",
	}
	for _, cmd := range unsafe {
		if m.IsSafe(cmd) {
			t.Fatalf("expected %q to NOT be safe", cmd)
		}
	}
}

func TestIsNotSafeGitWriteCommands(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(DefaultSafePatterns())
	unsafeGit := []string{
		"git push origin main",
		"git commit -m 'msg'",
		"git merge feature",
		"git rebase main",
		"git reset --hard HEAD~1",
		"git checkout -- file.txt",
		"git clean -fd",
		"git stash",
	}
	for _, cmd := range unsafeGit {
		if m.IsSafe(cmd) {
			t.Fatalf("expected %q to NOT be safe", cmd)
		}
	}
}

// ---------------------------------------------------------------------------
// IsSafe — custom patterns
// ---------------------------------------------------------------------------

func TestIsSafeCustomPatterns(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher([]string{`^go\s+(build|test|vet)(\s|$)`})
	if !m.IsSafe("go build ./...") {
		t.Fatal("expected 'go build' to be safe with custom pattern")
	}
	if !m.IsSafe("go test -v") {
		t.Fatal("expected 'go test' to be safe with custom pattern")
	}
	if m.IsSafe("go run main.go") {
		t.Fatal("expected 'go run' to NOT be safe with custom pattern")
	}
}

func TestIsSafeNoPatterns(t *testing.T) {
	t.Parallel()
	m := NewSafeCommandMatcher(nil)
	if m.IsSafe("ls") {
		t.Fatal("expected nothing to be safe with no patterns")
	}
}
