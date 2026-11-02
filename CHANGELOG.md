# Changelog

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
