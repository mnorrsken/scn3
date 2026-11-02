// Line icons on a 24 px grid, drawn with currentColor so they follow the theme.

const paths: Record<string, string> = {
  up: '<path d="M12 19V5M5.5 11.5 12 5l6.5 6.5"/>',
  back: '<path d="M19 12H5M11.5 5.5 5 12l6.5 6.5"/>',
  rescan: '<path d="M20 12a8 8 0 1 1-2.34-5.66"/><path d="M20 4v5h-5"/>',
  cancel: '<rect x="6.5" y="6.5" width="11" height="11" rx="2"/>',
  folder: '<path d="M3.5 7.5a2 2 0 0 1 2-2h3.6l2 2h7.4a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z"/>',
  // Themes
  system: '<rect x="3" y="4.5" width="18" height="12" rx="2"/><path d="M9 20h6M12 16.5V20"/>',
  light: '<circle cx="12" cy="12" r="4"/><path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M5.3 18.7l1.4-1.4M17.3 6.7l1.4-1.4"/>',
  dark: '<path d="M19.5 14.5A8 8 0 0 1 9.5 4.5a8 8 0 1 0 10 10z"/>',
  contrast: '<circle cx="12" cy="12" r="8.5"/><path d="M12 3.5a8.5 8.5 0 0 1 0 17z" fill="currentColor"/>',
  // Drives
  "drive-system": '<rect x="3" y="4.5" width="18" height="12" rx="2"/><path d="M9 20h6M12 16.5V20"/><circle cx="12" cy="10.5" r="2.2"/>',
  "drive-internal": '<rect x="3" y="6.5" width="18" height="11" rx="2.5"/><path d="M3 13h18"/><circle cx="7" cy="15.3" r=".6" fill="currentColor"/>',
  "drive-external": '<rect x="7" y="9" width="10" height="12" rx="2"/><path d="M9.5 9V3.5h5V9M11 5.5h.01M13 5.5h.01"/>',
  "drive-network": '<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17M12 3.5c2.6 2.3 3.8 5.1 3.8 8.5s-1.2 6.2-3.8 8.5c-2.6-2.3-3.8-5.1-3.8-8.5S9.4 5.8 12 3.5z"/>',
  "drive-optical": '<circle cx="12" cy="12" r="8.5"/><circle cx="12" cy="12" r="2.2"/>',
  "drive-image": '<path d="M12 3 20 7.5v9L12 21l-8-4.5v-9z"/><path d="M4 7.5 12 12l8-4.5M12 12v9"/>',
};

export function icon(name: string): string {
  return `<svg class="icon" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${paths[name] ?? paths["drive-internal"]}</svg>`;
}
