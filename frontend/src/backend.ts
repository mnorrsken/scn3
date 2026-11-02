// Typed access to the Go API that Wails binds on window.go.

export type Kind = "dir" | "file" | "other" | "free" | "unscanned";

export interface ViewNode {
  ref: number;
  name: string;
  size: number;
  files: number;
  kind: Kind;
  denied?: boolean;
  mtime?: number; // newest modification inside, Unix seconds
  path?: string;
  kids?: ViewNode[];
}

export interface Crumb {
  ref: number;
  name: string;
}

export interface View {
  root: ViewNode;
  crumbs: Crumb[];
  parent: number;
  sep: string;
  volumeBytes: number;
  scanning: boolean;
  denied: number;
}

export interface Volume {
  path: string;
  name: string;
  fs: string;
  kind: "system" | "internal" | "external" | "network" | "optical" | "image";
  hidden: boolean;
  capacity: number;
  free: number;
}

export interface Progress {
  scan: number;
  dirs: number;
  files: number;
  bytes: number;
  errors: number;
  elapsed: number;
  done: boolean;
}

export interface Settings {
  theme: string;
  showAllDrives: boolean;
  colorMode: string;
  hideLabels: boolean;
  width: number;
  height: number;
}

interface Api {
  Volumes(): Promise<Volume[]>;
  Scan(path: string): Promise<number>;
  Cancel(ref: number): Promise<void>;
  Rescan(ref: number): Promise<void>;
  Trash(ref: number): Promise<void>;
  Hide(ref: number): Promise<void>;
  Reveal(ref: number): Promise<void>;
  TrashFile(dirRef: number, name: string): Promise<void>;
  RevealFile(dirRef: number, name: string): Promise<void>;
  View(ref: number, maxDepth: number, minFrac: number): Promise<View>;
  Settings(): Promise<Settings>;
  SetTheme(theme: string): Promise<void>;
  SetShowAllDrives(show: boolean): Promise<void>;
  SetColorMode(mode: string): Promise<void>;
  SetHideLabels(hide: boolean): Promise<void>;
  Platform(): Promise<string>;
}

interface DialogsApi {
  ChooseFolder(): Promise<string>;
  OpenPrivacySettings(): Promise<void>;
}

declare global {
  interface Window {
    go: { desktop: { App: Api }; main: { Dialogs: DialogsApi } };
    runtime: {
      EventsOn(name: string, cb: (data: never) => void): void;
      ClipboardSetText(text: string): Promise<boolean>;
    };
  }
}

export const api = (): Api => window.go.desktop.App;
export const dialogs = (): DialogsApi => window.go.main.Dialogs;
export const copyText = (text: string): Promise<boolean> => window.runtime.ClipboardSetText(text);
export const onProgress = (cb: (p: Progress) => void): void =>
  window.runtime.EventsOn("scan:progress", cb as (data: never) => void);
