# Changelog

## [v0.0.2](https://github.com/yteraoka/ccr/compare/v0.0.1...v0.0.2) - 2026-07-25

- Rename homebrew tap repository to homebrew-cask by @yteraoka in https://github.com/yteraoka/ccr/pull/24
- Update README.ja.md and sync English translation by @yteraoka in https://github.com/yteraoka/ccr/pull/29
- Run go test on pull requests by @yteraoka in https://github.com/yteraoka/ccr/pull/30
- Increase test coverage (54% -> 84%) by @yteraoka in https://github.com/yteraoka/ccr/pull/31
- Update module github.com/mattn/go-runewidth to v0.0.27 by @renovate[bot] in https://github.com/yteraoka/ccr/pull/16
- Update module github.com/charmbracelet/bubbletea to v2 by @renovate[bot] in https://github.com/yteraoka/ccr/pull/17
- Update actions/checkout action to v7.0.1 by @renovate[bot] in https://github.com/yteraoka/ccr/pull/22
- Update Songmu/tagpr action to v1.20.1 by @renovate[bot] in https://github.com/yteraoka/ccr/pull/23
- Update golangci/golangci-lint-action digest to ba0d7d2 by @renovate[bot] in https://github.com/yteraoka/ccr/pull/25
- Update actions/setup-go action to v7 by @renovate[bot] in https://github.com/yteraoka/ccr/pull/26

## [v0.0.1](https://github.com/yteraoka/ccr/commits/v0.0.1) - 2026-07-24

- Rename command to ccr and add an interactive session picker by @yteraoka in https://github.com/yteraoka/ccr/pull/1
- Ignore .envrc by @yteraoka in https://github.com/yteraoka/ccr/pull/2
- Remove the list and info subcommands by @yteraoka in https://github.com/yteraoka/ccr/pull/3
- Use a Latin-1-representable middle dot for the prompt bullet by @yteraoka in https://github.com/yteraoka/ccr/pull/4
- Tighten up the preview pane by @yteraoka in https://github.com/yteraoka/ccr/pull/5
- Update module path to match renamed repository by @yteraoka in https://github.com/yteraoka/ccr/pull/6
- Show PID of currently running sessions in the picker by @yteraoka in https://github.com/yteraoka/ccr/pull/7
- Pin go 1.26.5 via mise by @yteraoka in https://github.com/yteraoka/ccr/pull/8
- Restructure into cmd/ and internal/ layout by @yteraoka in https://github.com/yteraoka/ccr/pull/9
- Show session's last jsonl timestamp instead of file mtime by @yteraoka in https://github.com/yteraoka/ccr/pull/10
- Add 'v' key to view a session's full transcript as HTML by @yteraoka in https://github.com/yteraoka/ccr/pull/11
- Show a key legend at the bottom of the session list pane by @yteraoka in https://github.com/yteraoka/ccr/pull/12
- Add README.md (English) and README.ja.md (Japanese) by @yteraoka in https://github.com/yteraoka/ccr/pull/13
- Add -v/--version flag by @yteraoka in https://github.com/yteraoka/ccr/pull/15
- Configure Renovate by @renovate[bot] in https://github.com/yteraoka/ccr/pull/14
- Add release automation (tagpr + GoReleaser) by @yteraoka in https://github.com/yteraoka/ccr/pull/19
