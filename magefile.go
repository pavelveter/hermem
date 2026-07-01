//go:build mage

// magefile.go — cross-platform Makefile alternative.
//
// Build tag `mage` excludes this file from `go build ./...` so it does not
// accidentally end up in the production binary. Run a target with:
//
//	go run -tags=mage magefile.go <target>
//
// Or install `mage` (`go install github.com/magefile/mage@latest`) and
// invoke targets with `mage <target>` (or just `mage` for the default).
//
// Targets mirror Makefile (the two are kept in lock-step intentionally).
// Some Make targets are intentionally omitted (sign, routes) where the
// behaviour is platform-specific or a no-op.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	pkg = "github.com/pavelveter/hermem"
	bin = "hermem"
)

// Default intentionally unset: bare `mage` prints available targets
// instead of mutating the workspace. Callers must pass explicit
// targets (`mage Build`, `mage Completions`, `mage Test`, etc.).
// Mirrors `make` behaviour (bare `make` is a no-op/help).

// ldflags returns the -ldflags string with build-time var overrides.
// Mirrors Makefile's LDFLAGS macro.
func ldflags() string {
	version := "dev"
	if out, err := sh.Output("git", "describe", "--tags", "--always", "--dirty"); err == nil {
		version = out
	}
	buildDate := "unknown"
	if out, err := sh.Output("date", "-u", "+%Y-%m-%dT%H:%M:%SZ"); err == nil {
		buildDate = strings.TrimSpace(out)
	}
	gitCommit := "unknown"
	if out, err := sh.Output("git", "rev-parse", "--short", "HEAD"); err == nil {
		gitCommit = out
	}
	return fmt.Sprintf(
		"-X '%s/api.BuildVersion=%s' -X 'main.version=%s' -X 'main.buildDate=%s' -X 'main.gitCommit=%s'",
		pkg, version, version, buildDate, gitCommit,
	)
}

// ensureBinDir creates the placeholder files for `go:embed` if missing.
// Idempotent — safe to run on every Build.
func ensureBinDir() error {
	if _, err := os.Stat("src/internal/ai/bin"); os.IsNotExist(err) {
		return sh.RunV("scripts/ensure-embed-placeholders.sh")
	}
	return nil
}

// Build compiles the hermem binary at the repo root.
func Build() error {
	if err := ensureBinDir(); err != nil {
		return err
	}
	return sh.RunV("go", "build", "-ldflags", ldflags(), "-o", bin, "./src")
}

// BuildLocal requires real llama-embedding binary in src/internal/ai/bin/.
func BuildLocal() error {
	binPath := "src/internal/ai/bin/llama-embedding"
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		return fmt.Errorf("place llama-embedding + dylibs in src/internal/ai/bin/ before building:\n  cp /path/to/llama-embedding %s\n  cp /path/to/lib*.dylib src/internal/ai/bin/llama-libs/", binPath)
	}
	return sh.RunV("go", "build", "-ldflags", ldflags(), "-o", bin, "./src")
}

// BuildNoLocal builds without the local CGO embedding dep.
func BuildNoLocal() error {
	if err := ensureBinDir(); err != nil {
		return err
	}
	return sh.RunV("go", "build", "-ldflags", ldflags(), "-tags", "no_local_embedding", "-o", bin, "./src")
}

// Test runs unit tests with race detection. Mirrors `make test`.
func Test() error {
	return sh.RunV("go", "test", "-race", "-count=1", "./src/...")
}

// TestE2E runs the e2e test suite. Mirrors `make test-e2e`.
func TestE2E() error {
	mg.Deps(Build)
	return sh.RunV("go", "test", "-p", "1", "./tests/e2e/...", "-v", "-timeout", "5m")
}

// Benchmarks runs the benchmark suite. Mirrors `make benchmarks`.
func Benchmarks() error {
	return sh.RunV("go", "test", "-bench=.", "-benchmem", "-count=3", "./src/...")
}

// Lint runs golangci-lint. Mirrors `make lint`.
func Lint() error {
	return sh.RunV("golangci-lint", "run", "./...")
}

// Fmt formats Go sources. Mirrors `make fmt`.
func Fmt() error {
	return sh.RunV("gofmt", "-s", "-w", ".")
}

// Vet runs go vet. Mirrors `make vet`.
func Vet() error {
	return sh.RunV("go", "vet", "./src/...")
}

// TestCoverage produces an HTML coverage report. Mirrors `make test-coverage`.
func TestCoverage() error {
	if err := sh.RunV("go", "test", "-coverprofile=coverage.out", "-covermode=atomic", "./src/..."); err != nil {
		return err
	}
	if err := sh.RunV("go", "tool", "cover", "-html=coverage.out", "-o", "coverage.html"); err != nil {
		return err
	}
	fmt.Println("Coverage report: coverage.html")
	return nil
}

// Clean removes build artifacts. Mirrors `make clean`.
func Clean() error {
	if err := os.Remove(bin); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := sh.Rm("src/internal/ai/bin"); err != nil {
		return err
	}
	fmt.Println("Cleaned.")
	return nil
}

// Install builds and installs hermem to INSTALL_DIR (default ~/.local/bin).
// On macOS, re-signs the installed binary with an ad-hoc signature to
// dodge the "Code Signature Invalid" gate on first launch.
func Install() error {
	mg.Deps(Build)
	installDir := os.Getenv("INSTALL_DIR")
	if installDir == "" {
		home, _ := os.UserHomeDir()
		installDir = filepath.Join(home, ".local", "bin")
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return err
	}
	if err := sh.Copy(bin, filepath.Join(installDir, bin)); err != nil {
		return err
	}
	if _, err := os.Stat("hermem.ini"); err == nil {
		if err := sh.Copy("hermem.ini", filepath.Join(installDir, "hermem.ini")); err != nil {
			return err
		}
	}
	if runtime.GOOS == "darwin" {
		installed := filepath.Join(installDir, bin)
		if err := sh.Run("codesign", "--force", "--sign", "-", installed); err == nil {
			fmt.Println("Signed:", installed)
		}
	}
	fmt.Println("Installed:", filepath.Join(installDir, bin))
	return nil
}

// Completions generates bash/zsh/fish completion scripts into ./completions/.
// Mirrors `make completions`. Uses the freshly-built binary to obtain
// generated output via the existing cobra-based completion subcommand.
func Completions() error {
	mg.Deps(Build)
	if err := os.MkdirAll("completions", 0o755); err != nil {
		return err
	}
	cases := []struct {
		shell string
		file  string
	}{
		{"bash", "completions/hermem.bash"},
		{"zsh", "completions/hermem.zsh"},
		{"fish", "completions/hermem.fish"},
	}
	binPath := filepath.Join(".", bin)
	for _, c := range cases {
		out, err := exec.Command(binPath, "completion", c.shell).Output()
		if err != nil {
			return fmt.Errorf("generate %s completion: %w", c.shell, err)
		}
		if err := os.WriteFile(c.file, out, 0o644); err != nil {
			return err
		}
	}
	fmt.Println("Generated: completions/hermem.{bash,zsh,fish}")
	return nil
}

// InstallCompletions copies the generated completion scripts into the
// user-local XDG paths. Override individually via COMPLETIONS_{BASH,ZSH,FISH}_DIR
// env vars. Mirrors `make install-completions`.
func InstallCompletions() error {
	if _, err := os.Stat("completions"); os.IsNotExist(err) {
		return fmt.Errorf("completions/ directory missing — run 'mage Completions' first")
	}
	home, _ := os.UserHomeDir()
	resolve := func(envVar, defaultPath string) string {
		if v := os.Getenv(envVar); v != "" {
			return v
		}
		return filepath.Join(home, filepath.FromSlash(defaultPath))
	}
	bashDir := resolve("COMPLETIONS_BASH_DIR", ".local/share/bash-completion/completions")
	zshDir := resolve("COMPLETIONS_ZSH_DIR", ".local/share/zsh/site-functions")
	fishDir := resolve("COMPLETIONS_FISH_DIR", ".config/fish/completions")
	pairs := []struct{ src, dst string }{
		{"completions/hermem.bash", filepath.Join(bashDir, "hermem")},
		{"completions/hermem.zsh", filepath.Join(zshDir, "_hermem")},
		{"completions/hermem.fish", filepath.Join(fishDir, "hermem.fish")},
	}
	dirs := map[string]bool{}
	for _, p := range pairs {
		dir := filepath.Dir(p.dst)
		if !dirs[dir] {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			dirs[dir] = true
		}
		if err := sh.Copy(p.src, p.dst); err != nil {
			return err
		}
	}
	fmt.Printf("Installed shell completions:\n  bash: %s\n  zsh : %s\n  fish: %s\n",
		filepath.Join(bashDir, "hermem"), filepath.Join(zshDir, "_hermem"), filepath.Join(fishDir, "hermem.fish"))
	return nil
}

// Dev runs air-based hot-reload. Mirrors `make dev`.
func Dev() error {
	if _, err := exec.LookPath("air"); err != nil {
		return fmt.Errorf("air (github.com/air-verse/air) not installed.\nInstall with:\n    go install github.com/air-verse/air@latest\nThen re-run: mage Dev")
	}
	return sh.RunV("air", "-c", ".air.toml")
}
