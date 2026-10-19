package ccr

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// historySkillName is the Claude Code skill `ccr skill install` writes: it
// tells Claude how to look through the conversations ccr has recorded.
const historySkillName = "ccr-history"

// historySkillTemplate is the skill's SKILL.md, with {{ccr}} standing for
// the command that runs ccr. ${CLAUDE_SESSION_ID} is left for Claude Code
// to fill in with the session the skill is used from, which is kept out of
// the results: its own recent messages would match everything.
const historySkillTemplate = `---
name: ccr-history
description: Search and read the user's past Claude Code conversations (every prompt they typed and Claude's final answer to it, recorded by ccr). Use when the user refers to something discussed, decided or done in an earlier session, or when knowing how a problem was handled before on this project would help.
when_to_use: The user says things like "as we discussed before", "last time", "in a previous session", "what did we decide about X", "the fix we did for Y", or in Japanese "前に話した", "以前のセッションで", "前回", "あのとき", "前にやった".
allowed-tools: Bash({{ccr}} search *) Bash({{ccr}} show *)
---

# Past conversations, through ccr

ccr keeps, in a local database, each prompt the user typed in Claude Code and
Claude's final answer to it, across sessions and projects. Tool calls and the
messages in between are not kept.

## 1. Search

` + "```" + `bash
{{ccr}} search -json -exclude-session ${CLAUDE_SESSION_ID} <words>
` + "```" + `

- It searches the messages recorded in the current directory. Add ` + "`-g`" + ` to search
  every project: when nothing relevant turns up, or the user means another project.
- Give a few distinctive words; all of them must appear (Japanese is fine).
  "Double quotes" keep a phrase exact. Rephrase and search again rather than
  giving up after one try.
- ` + "`-role user`" + ` searches only what the user asked, ` + "`-role assistant`" + ` only what
  Claude answered; ` + "`-l 20`" + ` returns more results (default 10).
- ` + "`-mode keyword`" + ` matches the words themselves (best for identifiers, file
  names, error messages); ` + "`-mode vector`" + ` matches by meaning. Without -mode both
  are combined when an embedding model is configured, keyword only otherwise.
  If ` + "`-mode vector`" + ` fails, carry on with keyword search.

Each output line is one result, best first: ` + "`id`" + `, ` + "`session_id`" + `, ` + "`role`" + `
(` + "`user`" + ` = the person, ` + "`assistant`" + ` = Claude), ` + "`time`" + `, ` + "`cwd`" + `, a ` + "`snippet`" + ` around
the match, and the ` + "`length`" + ` of the whole text. "no matches" on stderr means
nothing was found.

## 2. Read

` + "```" + `bash
{{ccr}} show -around <id> <session_id>    # the result with the two exchanges before and after it
{{ccr}} show -around <id> -context 5 <session_id>
{{ccr}} show <session_id>                 # everything recorded of that session
` + "```" + `

## 3. Answer

- Say what you found and when (the date, and which project from ` + "`cwd`" + `), and
  quote or summarize the relevant part.
- Treat it as history: code and decisions may have changed since. Check the
  current files before acting on anything found this way.
- If nothing relevant turns up, say so plainly.
- If every search finds nothing at all, ccr may not be recording: suggest
  ` + "`ccr hook install`" + ` (records from now on) and ` + "`ccr import`" + ` (adds past sessions).
`

// historySkill returns the SKILL.md text for running ccr as command.
func historySkill(command string) string {
	return strings.ReplaceAll(historySkillTemplate, "{{ccr}}", command)
}

// historySkillPaths returns where a scope keeps the skill: Claude Code reads
// skills from skills/ in the config directory (user), from .claude/skills
// in the project, and from .claude/skills in the managed settings directory
// (system).
func historySkillPaths(scope string, all bool) ([]string, error) {
	if all && scope != scopeUser {
		return nil, errors.New("-A goes with -scope user only")
	}
	rel := filepath.Join(historySkillName, "SKILL.md")
	switch scope {
	case scopeUser:
		dirs := []string{configDir()}
		if all {
			var err error
			if dirs, err = configDirs(); err != nil {
				return nil, err
			}
		}
		paths := make([]string, len(dirs))
		for i, d := range dirs {
			paths[i] = filepath.Join(d, "skills", rel)
		}
		return paths, nil
	case scopeProject:
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		return []string{filepath.Join(cwd, ".claude", "skills", rel)}, nil
	case scopeSystem:
		return []string{filepath.Join(systemSettingsDir(), ".claude", "skills", rel)}, nil
	}
	return nil, fmt.Errorf("-scope must be user, project or system, not %q", scope)
}

// RunSkillCommand implements `ccr skill install|uninstall`.
func RunSkillCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ccr skill install|uninstall [-scope user|project|system]")
	}
	action := args[0]
	if action != "install" && action != "uninstall" {
		return fmt.Errorf("unknown skill command %q: want install or uninstall", action)
	}

	fs := flag.NewFlagSet("ccr skill "+action, flag.ExitOnError)
	scope := fs.String("scope", scopeUser, "where to write: user ($CLAUDE_CONFIG_DIR/skills), project (.claude/skills here) or system (managed settings, every user)")
	all := fs.Bool("A", false, "with -scope user, every config directory in the config file")
	command := fs.String("command", "", `how the skill runs ccr (default: "ccr", or this binary's path if ccr is not on PATH)`)
	printOnly := fs.Bool("print", false, "print the skill instead of writing it")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: ccr skill %s [-scope user|project|system] [-A] [-print]", action)
		if action == "install" {
			fmt.Fprint(os.Stderr, " [-command cmd]")
		}
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	paths, err := historySkillPaths(*scope, *all)
	if err != nil {
		return err
	}
	cmd := *command
	if cmd == "" {
		cmd = ccrCommand()
	}
	skill := historySkill(cmd)

	for _, path := range paths {
		if *printOnly {
			fmt.Fprintf(os.Stderr, "# %s\n", path)
			if action == "install" {
				if _, err := os.Stdout.WriteString(skill); err != nil {
					return err
				}
			}
			continue
		}
		old, err := os.ReadFile(path)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		if action == "uninstall" {
			if !exists || !isHistorySkill(old) {
				fmt.Fprintf(os.Stderr, "%s: no ccr skill to remove\n", path)
				continue
			}
			if err := os.Remove(path); err != nil {
				return permissionHint(err, *scope)
			}
			os.Remove(filepath.Dir(path)) //nolint:errcheck // only if left empty
			fmt.Fprintf(os.Stderr, "%s: removed\n", path)
			continue
		}

		switch {
		case exists && string(old) == skill:
			fmt.Fprintf(os.Stderr, "%s: already up to date\n", path)
			continue
		case exists && !isHistorySkill(old):
			return fmt.Errorf("%s: exists and is not ccr's skill; not overwriting it", path)
		}
		if err := writeFileAtomic(path, []byte(skill), exists); err != nil {
			return permissionHint(err, *scope)
		}
		verb := "installed"
		if exists {
			verb = "updated"
		}
		fmt.Fprintf(os.Stderr, "%s: %s (restart Claude Code if a running session does not see it)\n", path, verb)
	}
	return nil
}

// isHistorySkill reports whether a SKILL.md is the one ccr writes, so ccr
// never overwrites or deletes someone else's skill of the same name.
func isHistorySkill(data []byte) bool {
	return strings.HasPrefix(string(data), "---\nname: "+historySkillName+"\n")
}
