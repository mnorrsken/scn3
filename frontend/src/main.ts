import { api, copyText, dialogs, onProgress, type HiddenItem, type Progress, type View, type Volume } from "./backend";
import {
  FULL, findArc, fitMiddle, formatBytes, formatDate, geometry, hitTest, layout, lerpDomain, MAX_DEPTH, minFraction, pathOf, zoomScene,
  heatColor, inWedge, scannedEnd, TAU, type Arc, type Colors, type Domain, type Geometry, type Mode,
} from "./layout";
import { icon } from "./icons";
import { Nav } from "./nav";
import { drawArcs, drawCenter, drawHighlight, drawLabels, drawWedge, type Palette } from "./render";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const canvas = $<HTMLCanvasElement>("chart");
const ctx = canvas.getContext("2d")!;
const base = document.createElement("canvas");
const baseCtx = base.getContext("2d")!;

const TWEEN_MS = 250;

const nav = new Nav();
const scans = new Map<string, number>(); // volume or folder path -> root ref
let volumes: Volume[] = [];
let view: View | null = null;
let rings: Arc[][] = [];
let geom: Geometry = geometry(800, 600);
let hover: Arc | null = null;
let dpr = 1;
let dirty = true;
let tween: { arcs: Arc[]; from: Domain; to: Domain; start: number; done: () => void } | null = null;
let showAllDrives = false;
let colorMode: Colors = "folders";
let hideLabels = false;
// "Scanned only": free and not-scanned space hidden, the scanned part filling the circle.
let scannedOnly = false;
let scannedShare = 1; // scanned part of the full circle, to scale the cull limit
let wedgeEnd: number | null = null; // where the band ends, null when there is no band
let hoverWedge = false;
// The unreadable-folders notice stays closed for this scan and count, and comes back if either changes.
let deniedDismissed = "";
let platform = "";
let themeChoice = "system";
let refetchTimer = 0;
let progressScan: number | null = null;
const active = new Map<number, Progress>(); // latest progress per scan

// ---- errors ----

let toastTimer = 0;
function toast(message: string): void {
  const el = $("toast");
  el.textContent = message;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => (el.hidden = true), 5000);
}
const report = (err: unknown) => toast(String(err));

// ---- theme ----

function palette(): Palette {
  const css = getComputedStyle(document.documentElement);
  const v = (name: string) => css.getPropertyValue(name).trim();
  return {
    mode: document.documentElement.dataset.theme as Mode,
    colors: colorMode,
    bg: v("--bg"),
    text: v("--text"),
    quiet: v("--quiet"),
    outline: v("--line"),
    accent: v("--accent"),
  };
}
let pal: Palette;

const THEMES = ["system", "light", "dark", "contrast"];
const systemDark = window.matchMedia("(prefers-color-scheme: dark)");
const systemContrast = window.matchMedia("(prefers-contrast: more)");

function applyTheme(): void {
  if (!THEMES.includes(themeChoice)) themeChoice = "system";
  let mode: Mode;
  if (themeChoice !== "system") mode = themeChoice as Mode;
  else if (systemContrast.matches) mode = "contrast";
  else mode = systemDark.matches ? "dark" : "light";
  document.documentElement.dataset.theme = mode;
  const btn = $("theme");
  btn.innerHTML = icon(themeChoice);
  btn.title = `Theme: ${themeChoice === "contrast" ? "high contrast" : themeChoice}`;
  for (const item of $("theme-menu").querySelectorAll<HTMLButtonElement>("button")) {
    item.setAttribute("aria-checked", String(item.dataset.theme === themeChoice));
  }
  pal = palette();
  updateLegend();
  dirty = true;
  schedule();
}
systemDark.addEventListener("change", applyTheme);

function updateLegend(): void {
  const on = colorMode === "age";
  $("legend").hidden = !on || !view;
  $("colors").setAttribute("aria-pressed", String(on));
  const mode = document.documentElement.dataset.theme as Mode;
  const steps = Array.from({ length: 11 }, (_, i) => heatColor(i / 10, mode));
  $("legend").querySelector<HTMLElement>(".ramp")!.style.background = `linear-gradient(to right, ${steps.join(", ")})`;
}

function setColorMode(next: Colors): void {
  colorMode = next;
  applyTheme();
}
systemContrast.addEventListener("change", applyTheme);

// ---- drawing ----

let scheduled = false;
function schedule(): void {
  if (!scheduled) {
    scheduled = true;
    requestAnimationFrame(frame);
  }
}

function clear(c: CanvasRenderingContext2D): void {
  c.setTransform(dpr, 0, 0, dpr, 0, 0);
  c.clearRect(0, 0, canvas.width / dpr, canvas.height / dpr);
}

function frame(now: number): void {
  scheduled = false;
  if (tween) {
    const raw = Math.min(1, (now - tween.start) / TWEEN_MS);
    const t = 1 - (1 - raw) ** 3;
    clear(ctx);
    drawArcs(ctx, tween.arcs, geom, pal, lerpDomain(tween.from, tween.to, t));
    if (raw < 1) {
      schedule();
      return;
    }
    const done = tween.done;
    tween = null;
    done();
    dirty = true;
  }
  if (dirty) {
    dirty = false;
    clear(baseCtx);
    if (view) {
      drawArcs(baseCtx, rings.flat(), geom, pal);
      if (!hideLabels) drawLabels(baseCtx, rings, geom, pal);
      drawCenter(baseCtx, geom, pal, view.root.name, formatBytes(view.root.size));
      if (wedgeEnd !== null) drawWedge(baseCtx, geom, pal, wedgeEnd, scannedOnly, false);
    }
  }
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.drawImage(base, 0, 0);
  if (hover) {
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    drawHighlight(ctx, hover, geom, pal);
  }
  if (hoverWedge && wedgeEnd !== null) {
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    drawWedge(ctx, geom, pal, wedgeEnd, scannedOnly, true);
  }
}

function resize(): void {
  const box = $("area").getBoundingClientRect();
  dpr = window.devicePixelRatio || 1;
  for (const c of [canvas, base]) {
    c.width = Math.round(box.width * dpr);
    c.height = Math.round(box.height * dpr);
  }
  const R = geom.R;
  geom = geometry(box.width, box.height);
  if (lastInfo[0]) setInfo(...lastInfo); // the info bar changed width too
  dirty = true;
  schedule();
  // The cull limit depends on the radius, so a bigger chart can show more.
  if (geom.R !== R && view) refetchSoon(150);
}
new ResizeObserver(resize).observe($("area"));

// ---- view ----

// In the scanned-only view the scanned part is stretched to the full circle,
// so segments can be smaller and still be drawn.
const fetchView = (ref: number): Promise<View> =>
  api().View(ref, MAX_DEPTH, minFraction(geom.R) * (scannedOnly ? scannedShare : 1));

const isPinned = (k: { kind: string }) => k.kind === "free" || k.kind === "unscanned";

function show(v: View): void {
  const kids = v.root.kids ?? [];
  const hidden = kids.some(isPinned);
  if (!scannedOnly || !hidden) {
    const total = kids.reduce((n, k) => n + k.size, 0);
    if (total > 0) scannedShare = kids.filter((k) => !isPinned(k)).reduce((n, k) => n + k.size, 0) / total;
  }
  if (scannedOnly && hidden) v = { ...v, root: { ...v.root, kids: kids.filter((k) => !isPinned(k)) } };
  view = v;
  rings = layout(v.root, geom.R);
  wedgeEnd = !hidden ? null : scannedOnly ? TAU : scannedEnd(rings);
  hoverWedge = false;
  hover = null;
  dirty = true;
  $("empty").hidden = true;
  updateLegend();
  updateChrome();
  void updateHidden();
  schedule();
}

function refetchSoon(ms: number): void {
  clearTimeout(refetchTimer);
  refetchTimer = window.setTimeout(() => {
    refetchTimer = 0;
    void refresh();
  }, ms);
}

/** Fetches the current root again without animating. */
async function refresh(): Promise<void> {
  if (nav.current === null || tween) return;
  try {
    show(await fetchView(nav.current));
  } catch {
    // The folder is gone, for example after a rescan removed it. Go somewhere that exists.
    await recover();
  }
}

async function recover(): Promise<void> {
  while (nav.back() !== null) {
    try {
      show(await fetchView(nav.current!));
      return;
    } catch {
      // Keep walking back.
    }
  }
  view = null;
  rings = [];
  nav.current = null;
  $("empty").hidden = false;
  dirty = true;
  updateChrome();
  schedule();
}

/** Moves to ref: as a zoom if one view sits inside the other, otherwise directly. */
async function navigate(ref: number, push = true): Promise<void> {
  if (tween) return;
  let next: View;
  try {
    next = await fetchView(ref);
  } catch (err) {
    report(err);
    return;
  }
  if (push) nav.go(ref);
  const old = view;
  const oldRings = rings;
  const nextRings = layout(next.root, geom.R);
  const finish = () => show(next);
  hover = null;

  const inward = old && findArc(oldRings, ref);
  const outward = old && !inward && findArc(nextRings, old.root.ref);
  if (inward) {
    const s = zoomScene(oldRings, nextRings, inward);
    tween = { arcs: s.arcs, from: FULL, to: s.domain, start: performance.now(), done: finish };
  } else if (outward) {
    const s = zoomScene(nextRings, oldRings, outward);
    tween = { arcs: s.arcs, from: s.domain, to: FULL, start: performance.now(), done: finish };
  } else {
    finish();
    return;
  }
  schedule();
}

// ---- actions ----

function up(): void {
  if (!view) return;
  if (view.parent >= 0) void navigate(view.parent);
}

function back(): void {
  const ref = nav.back();
  if (ref !== null) void navigate(ref, false);
}

async function scan(path: string): Promise<void> {
  try {
    const ref = await api().Scan(path);
    scans.set(path, ref);
    progressScan = ref;
    nav.reset(ref);
    show(await fetchView(ref));
    renderVolumes();
  } catch (err) {
    report(err);
  }
}

async function rescan(ref: number): Promise<void> {
  try {
    await api().Rescan(ref);
    progressScan = ref;
    updateChrome();
    refetchSoon(50);
  } catch (err) {
    report(err);
  }
}

function confirmTrash(path: string, size: string): Promise<boolean> {
  const dlg = $<HTMLDialogElement>("confirm");
  $("confirm-text").textContent = `Move "${path}" (${size}) to the Trash?`;
  dlg.returnValue = "cancel";
  dlg.showModal();
  return new Promise((resolve) => {
    dlg.addEventListener("close", () => resolve(dlg.returnValue === "ok"), { once: true });
  });
}

async function trash(arc: Arc): Promise<void> {
  const path = arcPath(arc);
  if (!(await confirmTrash(path, formatBytes(arc.size)))) return;
  try {
    if (arc.kind === "file") await api().TrashFile(arc.ref, arc.name);
    else await api().Trash(arc.ref);
    await refresh();
    void loadVolumes();
  } catch (err) {
    report(err);
  }
}

// ---- hidden folders ----

let hiddenItems: HiddenItem[] = [];

/** Shows how many folders are hidden in the scan on screen, as a chip that opens a list. */
async function updateHidden(): Promise<void> {
  const chip = $<HTMLButtonElement>("hidden-chip");
  const scanRef = view?.crumbs[0]?.ref;
  hiddenItems = scanRef === undefined ? [] : await api().Hidden(scanRef).catch(() => []);
  chip.hidden = hiddenItems.length === 0;
  if (chip.hidden) setHiddenMenu(false);
  const size = hiddenItems.reduce((n, h) => n + h.size, 0);
  chip.textContent = `${hiddenItems.length} hidden  ·  ${formatBytes(size)}`;
  chip.title = "Show hidden folders again";
  if (!$("hidden-menu").hidden) renderHiddenMenu();
}

function renderHiddenMenu(): void {
  const menu = $("hidden-menu");
  const unhide = (action: () => Promise<void>) => async () => {
    try {
      await action();
      await refresh();
    } catch (err) {
      report(err);
    }
  };
  const items = hiddenItems.map((h) => {
    const b = document.createElement("button");
    b.setAttribute("role", "menuitem");
    b.title = `Show ${h.path} again`;
    b.append(
      Object.assign(document.createElement("span"), { className: "name", textContent: h.name }),
      Object.assign(document.createElement("span"), { className: "size", textContent: formatBytes(h.size) }),
    );
    b.onclick = unhide(() => api().Unhide(h.ref));
    return b;
  });
  const all = Object.assign(document.createElement("button"), { className: "all", textContent: "Show all again" });
  all.setAttribute("role", "menuitem");
  all.onclick = unhide(() => api().UnhideAll(hiddenItems[0].ref));
  menu.replaceChildren(...items, ...(hiddenItems.length > 1 ? [all] : []));
}

function setHiddenMenu(open: boolean): void {
  if (open) renderHiddenMenu();
  $("hidden-menu").hidden = !open;
  $("hidden-chip").setAttribute("aria-expanded", String(open));
}

async function hide(arc: Arc): Promise<void> {
  try {
    await api().Hide(arc.ref);
    await refresh();
  } catch (err) {
    report(err);
  }
}

async function copyPath(arc: Arc): Promise<void> {
  try {
    if (await copyText(arcPath(arc))) toast("Path copied");
    else report("Could not copy the path");
  } catch (err) {
    report(err);
  }
}

const arcPath = (a: Arc): string => (view ? pathOf(a, view.root.path ?? "", view.sep) : a.name);

// ---- chrome ----

function updateChrome(): void {
  const v = view;
  $<HTMLButtonElement>("up").disabled = !v || v.parent < 0;
  $<HTMLButtonElement>("back").disabled = !nav.canGoBack;
  $<HTMLButtonElement>("rescan").disabled = !v || v.scanning;
  $("cancel").hidden = !v?.scanning;
  $("progress").hidden = !v?.scanning;

  const crumbs = $("crumbs");
  crumbs.replaceChildren();
  (v?.crumbs ?? []).forEach((c, i) => {
    if (i > 0) crumbs.append(Object.assign(document.createElement("span"), { textContent: "›" }));
    const b = document.createElement("button");
    b.textContent = c.name;
    b.onclick = () => void navigate(c.ref);
    crumbs.append(b);
  });

  const denied = $("denied");
  const deniedKey = v ? `${v.crumbs[0]?.ref}:${v.denied}` : "";
  denied.hidden = !v?.denied || deniedKey === deniedDismissed;
  if (v?.denied) {
    $("denied-text").textContent = `${v.denied} folder${v.denied === 1 ? "" : "s"} could not be read, so totals are low.`;
    $("privacy").hidden = platform !== "darwin";
  }
  showInfo();
  renderVolumes();
}

let lastInfo: [string, string] = ["", ""];
const measureCtx = document.createElement("canvas").getContext("2d")!;

/** Shows a path and its facts; a long path is shortened in the middle to fit. */
function setInfo(where: string, facts: string): void {
  lastInfo = [where, facts];
  const info = $("info");
  const strong = Object.assign(document.createElement("span"), { className: "where", textContent: where, title: where });
  const rest = Object.assign(document.createElement("span"), { className: "facts", textContent: facts });
  info.replaceChildren(strong, rest);
  const css = getComputedStyle(info);
  const room = info.clientWidth - parseFloat(css.paddingLeft) - parseFloat(css.paddingRight) - rest.offsetWidth - parseFloat(css.columnGap || "0");
  measureCtx.font = getComputedStyle(strong).font;
  strong.textContent = fitMiddle(where, room - 2, (s) => measureCtx.measureText(s).width);
}

function showInfo(): void {
  const v = view;
  if (!v) {
    $("info").replaceChildren();
    return;
  }
  const count = (n: number) => `${n.toLocaleString()} ${n === 1 ? "file" : "files"}`;
  const ahead = (t: number) => (t > Date.now() / 1000 + 3600 ? " (in the future)" : "");
  const changed = (t?: number) => (t ? `  ·  changed ${formatDate(t)}${ahead(t)}` : "");
  const a = hover;
  if (!a) {
    setInfo(v.root.path || v.root.name, `${formatBytes(v.root.size)}  ·  ${count(v.root.files)}${changed(v.root.mtime)}`);
    return;
  }
  if (a.kind === "free") {
    setInfo(`Free on ${v.root.name}`, formatBytes(a.size));
    return;
  }
  if (a.kind === "unscanned") {
    setInfo(`Used on ${v.root.name} but not found by the scan`, formatBytes(a.size));
    return;
  }
  const parentSize = a.parent?.size ?? v.root.size;
  const pct = (part: number, whole: number) => (whole > 0 ? `${((100 * part) / whole).toFixed(1)}%` : "–");
  const where = a.kind === "other" ? `Small items in ${arcPath(a.parent ?? a)}` : arcPath(a);
  setInfo(where, `${formatBytes(a.size)}  ·  ${count(a.files)}  ·  ${pct(a.size, parentSize)} of parent  ·  ${pct(a.size, v.volumeBytes)} of volume${changed(a.mtime)}`);
}

/** Status line of a drive card: scan progress while scanning, free space otherwise. */
function driveStatus(ref: number | undefined, free: number, capacity: number): [string, boolean] {
  const p = ref === undefined ? undefined : active.get(ref);
  if (p && !p.done) return [`Scanning… ${p.files.toLocaleString()} files`, true];
  // A cloud sync folder has no free space of its own.
  if (capacity <= 0) return [p ? `${formatBytes(p.bytes)} on this disk` : "Cloud folder", false];
  return [`${formatBytes(free)} free of ${formatBytes(capacity)}`, false];
}

function driveCard(iconName: string, name: string, status: [string, boolean], used: number, capacity: number, isActive: boolean, onClick: () => void): HTMLLIElement {
  const li = document.createElement("li");
  const b = document.createElement("button");
  b.className = `drive${isActive ? " active" : ""}${status[1] ? " scanning" : ""}`;
  b.innerHTML = icon(iconName);
  const label = Object.assign(document.createElement("span"), { className: "name", textContent: name });
  const small = Object.assign(document.createElement("small"), { textContent: status[0] });
  b.append(label, small);
  if (capacity > 0) {
    const meter = Object.assign(document.createElement("div"), { className: "meter" });
    const fill = document.createElement("div");
    fill.style.width = `${(100 * used) / capacity}%`;
    meter.append(fill);
    b.append(meter);
  }
  b.onclick = onClick;
  li.append(b);
  return li;
}

function renderVolumes(): void {
  const list = $("volumes");
  list.replaceChildren();
  for (const vol of volumes) {
    const ref = scans.get(vol.path);
    const isActive = ref !== undefined && view?.crumbs[0]?.ref === ref;
    // A hidden drive stays listed while it is the one on screen.
    if (vol.hidden && !showAllDrives && !isActive) continue;
    list.append(driveCard(`drive-${vol.kind}`, vol.name, driveStatus(ref, vol.free, vol.capacity), vol.capacity - vol.free, vol.capacity, isActive, () => {
      if (ref === undefined) {
        void scan(vol.path);
        return;
      }
      nav.go(ref);
      void refresh();
    }));
  }
}

async function loadVolumes(): Promise<void> {
  try {
    volumes = await api().Volumes();
    renderVolumes();
  } catch (err) {
    report(err);
  }
}

function handleProgress(p: Progress): void {
  active.set(p.scan, p);
  const { files, bytes, done: finished } = p;
  $("progress-text").textContent = `${finished ? "Finished" : "Scanning…"} ${files.toLocaleString()} files, ${formatBytes(bytes)}`;
  const bar = $("progress-bar");
  const target = volumes.filter((v) => scans.get(v.path) === p.scan);
  const used = target.reduce((n, v) => n + v.capacity - v.free, 0);
  if (used > 0) {
    bar.classList.remove("unknown");
    bar.style.width = `${Math.min(100, (100 * bytes) / used)}%`;
  } else {
    bar.style.width = "";
    bar.classList.add("unknown");
  }
  renderVolumes();
  if (p.done) {
    void loadVolumes();
    refetchSoon(0);
  } else if (!refetchTimer) {
    refetchSoon(300);
  }
}

// ---- pointer, menu, keys ----

function pointer(e: MouseEvent) {
  const box = canvas.getBoundingClientRect();
  return hitTest(rings, geom, e.clientX - box.left, e.clientY - box.top);
}

function onWedge(e: MouseEvent): boolean {
  if (wedgeEnd === null) return false;
  const box = canvas.getBoundingClientRect();
  return inWedge(geom, e.clientX - box.left, e.clientY - box.top, wedgeEnd);
}

/** Switches the scanned-only view, stretching or shrinking the scanned part. */
async function toggleScannedOnly(): Promise<void> {
  if (!view || wedgeEnd === null || tween || nav.current === null) return;
  const turnOn = !scannedOnly;
  const end = wedgeEnd;
  scannedOnly = turnOn;
  let next: View;
  try {
    next = await fetchView(nav.current);
  } catch (err) {
    scannedOnly = !turnOn;
    report(err);
    return;
  }
  hover = null;
  hoverWedge = false;
  if (turnOn) {
    tween = { arcs: rings.flat(), from: FULL, to: { a0: 0, a1: end, depth: 0 }, start: performance.now(), done: () => show(next) };
  } else {
    const nextRings = layout(next.root, geom.R);
    const nextEnd = scannedEnd(nextRings) ?? TAU;
    tween = { arcs: nextRings.flat(), from: { a0: 0, a1: nextEnd, depth: 0 }, to: FULL, start: performance.now(), done: () => show(next) };
  }
  schedule();
}

canvas.addEventListener("mousemove", (e) => {
  if (tween) return;
  const wedge = onWedge(e);
  if (wedge !== hoverWedge) {
    hoverWedge = wedge;
    hover = null;
    canvas.style.cursor = wedge ? "pointer" : "default";
    if (wedge) {
      setInfo(
        scannedOnly ? "Showing only scanned space" : "Show only scanned space",
        scannedOnly ? "Click to show free and not-scanned space again" : "Click to let the scanned folders fill the circle",
      );
    } else {
      showInfo();
    }
    schedule();
  }
  if (wedge) return;
  const hit = pointer(e);
  const arc = hit && !hit.center ? hit.arc : null;
  if (arc !== hover) {
    hover = arc;
    canvas.style.cursor = arc?.kind === "dir" || hit?.center ? "pointer" : "default";
    showInfo();
    schedule();
  }
});

canvas.addEventListener("mouseleave", () => {
  hover = null;
  hoverWedge = false;
  showInfo();
  schedule();
});

canvas.addEventListener("click", (e) => {
  closeMenu();
  if (onWedge(e)) {
    void toggleScannedOnly();
    return;
  }
  const hit = pointer(e);
  if (!hit) return;
  if (hit.center) up();
  else if (hit.arc.kind === "dir") void navigate(hit.arc.ref);
});

function closeMenu(): void {
  $("menu").hidden = true;
}

canvas.addEventListener("contextmenu", (e) => {
  e.preventDefault();
  closeMenu();
  const hit = pointer(e);
  if (!hit || hit.center) return;
  const arc = hit.arc;
  if (arc.ref < 0) return;
  const busy = !!view?.scanning;
  const items: [string, () => void, boolean, string?][] =
    arc.kind === "file"
      ? [
          ["Open in file manager", () => void api().RevealFile(arc.ref, arc.name).catch(report), true],
          ["Copy path", () => void copyPath(arc), true],
          ["Move to Trash…", () => void trash(arc), !busy, "danger"],
        ]
      : [
          ["Zoom", () => void navigate(arc.ref), true],
          ["Open in file manager", () => void api().Reveal(arc.ref).catch(report), true],
          ["Copy path", () => void copyPath(arc), true],
          ["Rescan", () => void rescan(arc.ref), !busy],
          ["Hide", () => void hide(arc), !busy],
          ["Move to Trash…", () => void trash(arc), !busy, "danger"],
        ];
  const menu = $("menu");
  menu.replaceChildren(
    ...items.map(([label, action, enabled, cls]) => {
      const b = document.createElement("button");
      b.textContent = label;
      b.disabled = !enabled;
      b.className = cls ?? "";
      b.setAttribute("role", "menuitem");
      b.onclick = () => {
        closeMenu();
        action();
      };
      return b;
    }),
  );
  menu.hidden = false;
  const { offsetWidth: w, offsetHeight: h } = menu;
  menu.style.left = `${Math.min(e.clientX, window.innerWidth - w - 4)}px`;
  menu.style.top = `${Math.min(e.clientY, window.innerHeight - h - 4)}px`;
});

document.addEventListener("mousedown", (e) => {
  if (!$("menu").contains(e.target as Node)) closeMenu();
});

window.addEventListener("keydown", (e) => {
  if ($<HTMLDialogElement>("confirm").open) return;
  if (e.key === "Escape") {
    closeMenu();
    setThemeMenu(false);
    setHiddenMenu(false);
  }
  else if (e.key === "Backspace") {
    e.preventDefault();
    up();
  } else if (e.key === "ArrowLeft" && e.altKey) {
    e.preventDefault();
    back();
  } else if (e.key === "F5") {
    e.preventDefault();
    if (nav.current !== null && !view?.scanning) void rescan(nav.current);
  } else if (e.key === "Enter" && hover?.kind === "dir") {
    void navigate(hover.ref);
  }
});

$("up").onclick = up;
$("back").onclick = back;
$("rescan").onclick = () => {
  if (nav.current !== null) void rescan(nav.current);
};
$("cancel").onclick = () => {
  const target = progressScan ?? nav.current;
  if (target !== null) void api().Cancel(target);
};
$("folder").onclick = async () => {
  try {
    const path = await dialogs().ChooseFolder();
    if (path) await scan(path);
  } catch (err) {
    report(err);
  }
};
$("privacy").onclick = () => void dialogs().OpenPrivacySettings();
$("hidden-chip").onclick = () => setHiddenMenu($("hidden-menu").hidden !== false);
document.addEventListener("mousedown", (e) => {
  const t = e.target as Node;
  if (!$("hidden-menu").contains(t) && !$("hidden-chip").contains(t)) setHiddenMenu(false);
});
$("denied-close").innerHTML = icon("close");
$("denied-close").onclick = () => {
  if (view) deniedDismissed = `${view.crumbs[0]?.ref}:${view.denied}`;
  $("denied").hidden = true;
};
$<HTMLInputElement>("show-all").onchange = (e) => {
  showAllDrives = (e.target as HTMLInputElement).checked;
  renderVolumes();
  void api().SetShowAllDrives(showAllDrives).catch(report);
};
const themeMenu = $<HTMLDivElement>("theme-menu");
function setThemeMenu(open: boolean): void {
  themeMenu.hidden = !open;
  $("theme").setAttribute("aria-expanded", String(open));
}
$("theme").onclick = () => setThemeMenu(themeMenu.hidden !== false);
for (const item of themeMenu.querySelectorAll<HTMLButtonElement>("button")) {
  item.innerHTML = icon(item.dataset.theme!) + item.innerHTML;
  item.onclick = () => {
    themeChoice = item.dataset.theme!;
    setThemeMenu(false);
    applyTheme();
    void api().SetTheme(themeChoice).catch(report);
  };
}
document.addEventListener("mousedown", (e) => {
  if (!themeMenu.contains(e.target as Node) && !$("theme").contains(e.target as Node)) setThemeMenu(false);
});
for (const [id, name] of [["up", "up"], ["back", "back"], ["rescan", "rescan"], ["cancel", "cancel"], ["colors", "age"], ["labels", "labels"]]) $(id).innerHTML = icon(name);
function setHideLabels(hide: boolean): void {
  hideLabels = hide;
  $("labels").setAttribute("aria-pressed", String(!hide));
  $("labels").title = hide ? "Show names in the chart" : "Hide names in the chart";
  dirty = true;
  schedule();
}
$("labels").onclick = () => {
  setHideLabels(!hideLabels);
  void api().SetHideLabels(hideLabels).catch(report);
};
$("colors").onclick = () => {
  setColorMode(colorMode === "age" ? "folders" : "age");
  void api().SetColorMode(colorMode).catch(report);
};
$("folder").innerHTML = `${icon("folder")}<span>Scan a folder…</span>`;

// ---- start ----

async function start(): Promise<void> {
  applyTheme();
  resize();
  updateChrome();
  onProgress(handleProgress);
  try {
    const [settings, os] = await Promise.all([api().Settings(), api().Platform()]);
    platform = os;
    themeChoice = settings.theme;
    showAllDrives = settings.showAllDrives;
    colorMode = settings.colorMode === "age" ? "age" : "folders";
    setHideLabels(settings.hideLabels);
    $<HTMLInputElement>("show-all").checked = showAllDrives;
    applyTheme();
  } catch (err) {
    report(err);
  }
  await loadVolumes();
}

void start();
