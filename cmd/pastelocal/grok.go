package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

// canonicalPasteSkillHeader is used by hasGrokSkill to validate that a probed
// source tree contains *our* canonical skill (defense against malicious trees
// in probed locations like cwd or ~/src).
var canonicalPasteSkillHeader = []byte("# PasteLocal /paste — Grok Skill")

// ── grok (agent integration) ─────────────────────────────────────────────────

var grokCmd = &cobra.Command{
	Use:     "grok",
	Short:   "Grok / agentic coding tool integration helpers",
	GroupID: "grok",
}

var grokInstallCmd = &cobra.Command{
	Use:   "install-skills",
	Short: "Install or update PasteLocal Grok skills (paste, history, snippet, send, recall) into ~/.grok/skills/",
	Args:  cobra.NoArgs,
	RunE:  runGrokInstallSkills,
}

func init() {
	grokCmd.AddCommand(grokInstallCmd)
	rootCmd.AddCommand(grokCmd)
}

func runGrokInstallSkills(cmd *cobra.Command, args []string) error {
	srcDir, err := findGrokSourceDir()
	if err != nil {
		return fail("%v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fail("could not determine home directory: %v", err)
	}
	dstBase := filepath.Join(home, ".grok", "skills")
	// 0755 is intentional for a user-discoverable skills directory (Grok/AI tools must read SKILL.md).
	// Contrast with 0700/0600 used only for private tokens/keys (see SECURITY.md and auth/store).
	if err := os.MkdirAll(dstBase, 0755); err != nil {
		return fail("failed to create ~/.grok/skills: %v", err)
	}

	skills := []string{"paste", "paste-send", "recall", "paste-history", "paste-snippet"}
	copied := 0
	for _, name := range skills {
		src := filepath.Join(srcDir, name)
		if _, err := os.Stat(filepath.Join(src, "SKILL.md")); err != nil {
			continue // skip missing (future-proof)
		}
		dst := filepath.Join(dstBase, "pastelocal-"+name)
		_ = os.RemoveAll(dst) // clean previous
		cpBin := "/bin/cp"
		if _, err := os.Stat(cpBin); err != nil {
			cpBin = "/usr/bin/cp"
		}
		cp := exec.Command(cpBin, "-r", src, dst)
		if output, err := cp.CombinedOutput(); err != nil {
			return fail("failed to install skill %s: %v\n%s", name, err, string(output))
		}
		copied++
	}

	if copied == 0 {
		return fail("no skills were copied from %s (run from PasteLocal source tree or provide correct layout)", srcDir)
	}
	fmt.Printf("PasteLocal Grok skills installed successfully (%d skills).\n", copied)
	fmt.Println("  Discoverable as: /paste, /paste-send, /paste-history, /paste-snippet, /recall")
	fmt.Println("\nUpdate anytime with: pastelocal grok install-skills  (after git pull)")
	return nil
}

// findGrokSourceDir locates the skill/grok tree relative to the running binary,
// cwd, or common install locations. Prefers source tree clones.
func findGrokSourceDir() (string, error) {
	// Try relative to the executable (works when running from a build next to the repo tree).
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, rel := range []string{"../skill/grok", "skill/grok", "../../skill/grok"} {
			cand := filepath.Join(dir, rel)
			if hasGrokSkill(cand) {
				return filepath.Clean(cand), nil
			}
		}
	}

	// CWD or parent (user is inside the cloned repo).
	if cwd, err := os.Getwd(); err == nil {
		for _, rel := range []string{"skill/grok", "../skill/grok", "PasteLocal/skill/grok"} {
			cand := filepath.Join(cwd, rel)
			if hasGrokSkill(cand) {
				return filepath.Clean(cand), nil
			}
		}
	}

	// Fallback locations for packaged or user installs.
	home, homeErr := os.UserHomeDir()
	for _, cand := range []string{
		filepath.Join(home, ".local", "share", "pastelocal", "skill", "grok"),
		filepath.Join(home, "src", "PasteLocal", "skill", "grok"),
		"/usr/local/share/pastelocal/skill/grok",
	} {
		if hasGrokSkill(cand) {
			return cand, nil
		}
	}

	if homeErr != nil {
		return "", fmt.Errorf("PasteLocal grok skills source not found (home probe err: %v). Run from a clone of the official repo (git instructions below)", homeErr)
	}
	return "", fmt.Errorf("PasteLocal grok skills source not found. Run from a clone of the official repo: git clone https://github.com/Zen-Open-Source/PasteLocal && cd PasteLocal && ./pastelocal grok install-skills (or go run ./cmd/pastelocal grok install-skills)")
}

func hasGrokSkill(dir string) bool {
	p := filepath.Join(dir, "paste", "SKILL.md")
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return bytes.Contains(data, canonicalPasteSkillHeader)
}
