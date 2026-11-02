# Changelog

## [0.9.2] - 2026-11-03

### Changed

- A long path in the info bar is shortened in the middle, so both the drive and the file or folder name stay visible. The full path shows when you hover over it.

## [0.9.1] - 2026-11-02

### Changed

- The app is called "scn3 Disk Sunburst" in its window title and on Linux menus. Commands, files and packages are still named scn3.

### Added

- Color by age: the clock button colors each file and folder by its newest change, compared within each folder: blue, yellow, red in the light theme, plasma in the dark theme and viridis in high contrast, old to new. A folder counts as new as the newest file or folder inside it. The hover text shows the date.
- The `scan --json` output has a `modified` time for each folder.
- The notice about folders that could not be read has a close button. It comes back for a new scan or when the count changes.
- Dates show as YYYY-MM-DD. A file dated in the future (some caches store an expiry date there) is marked so, and counts as changed now for its folder and the age colors.
- A button to hide the folder and file names in the chart; the center keeps its name and size.
- At a drive's root, a band inside the center circle shows the scanned part. Click it to let the scanned folders fill the whole circle without free and not-scanned space; click again to bring them back.

## [0.9.0] - 2026-11-02

### Changed

- The hover text has its own bar above the chart instead of sitting on top of it.
- The drive list shows only real drives. Turn on "Show all drives" to also list disk images, extra system volumes (such as macOS Recovery) and boot, snap and container mounts on Linux. The choice is saved.

### Removed

- "All local drives". With one real disk it was the same as clicking that disk, and it started scans of every drive at once.

### Added

- Copy path in the right-click menu, for folders and files.
- Releases list the downloads per OS with install notes, and include `THIRD_PARTY_NOTICES.txt` with the licenses of the third-party code.
- The app shows its real version in its file properties (Windows) and app info (macOS).

## [0.1.0-beta.4] - 2026-11-02

### Changed

- No more "files here" segments. Files now show one by one: always in the folder in the center (small ones merged into "other"), and further out in folders whose files take up a visible part of the chart. Elsewhere a folder's own files are an empty part of its arc. Only those folders are read again, at most 200 per view, so this does not slow the scan.

- New look: icon toolbar, drive cards with an icon per drive type, and a theme menu.
- The Summary checkbox is now an "All local drives" card. It leaves out network shares and optical discs.
- Linux: a scan no longer enters bind mounts or other mounts below the scanned folder, even on the same device.

### Added

- Right-click a file: Open in file manager, or Move to Trash.
- High-contrast theme. The System theme picks it when the OS asks for more contrast.

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
