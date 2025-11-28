# macOS PATH handling for installer

## Initial Prompt
The scripts/install.sh script doesn't seem to properly cover macos PATH update. Investigate why and refactor to add proper support for macos terminal.

## Implementation Summary
Implementation Summary: Added login-shell-aware PATH updates in the POSIX installer so macOS zsh defaults use ~/.zprofile (with fallbacks for zshrc/bash_profile/bashrc/fish) and creates rc directories before appending the binary path.

## Documentation Overview
- Installer now chooses the shell rc file based on the user's login shell, preferring macOS-specific defaults (`~/.zprofile` for zsh, `~/.bash_profile` for bash) while preserving fish and generic fallbacks.
- When the PATH entry is missing, the script creates the rc directory if needed and appends the export statement only once to avoid duplicates.
- Keeps existing behaviour for Linux shells but fixes macOS scenarios where running via bash previously ignored zsh rc files.

## Implementation Examples
- `cli-init/scripts/install.sh:72-88` resolves the appropriate rc file per OS/shell and appends the PATH export only when absent, creating parent directories first.
- `cli-init/scripts/install.sh:93-121` adds `resolve_shell_rc` to map login shells to rc files, prioritising macOS zsh `.zprofile`.
