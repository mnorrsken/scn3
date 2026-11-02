# Changelog

## [0.1.0-beta.3] - 2026-11-02

### Fixed

- Windows: scans found nothing on some PCs ("Invalid access to memory location"). The buffer for reading folders was not 8-byte aligned.
- Windows: drives without file IDs (such as FAT32 and exFAT) are listed without them, so files there are not wrongly treated as hard links of each other.
- The `scan` CLI prints the first errors it hits, not only how many folders could not be read.

## [0.1.0-beta.2] - 2026-11-02

### Fixed

- macOS: APFS clones of a file now count once, not once per copy.
- Windows: releases now include the installer.

## [0.1.0-beta.1] - 2026-11-02

Beta release. The Windows and Linux desktop apps are less tested than macOS, and builds are unsigned.

### Added

- Desktop app with a sunburst chart of disk usage, including an arc for free space.
- Parallel scan with progress and cancel.
- Hover info, animated click to zoom, Up, Back and Rescan, and a breadcrumb.
- Keyboard: Backspace (Up), Alt+Left (Back), F5 (Rescan), Enter (zoom hovered item).
- Context menu: Zoom, Open, Rescan, Hide, Move to Trash.
- Volume list and Summary.
- Light and dark theme, and a settings file.
- `scan` command line tool: `scan <path> [--json] [--depth N]`.
