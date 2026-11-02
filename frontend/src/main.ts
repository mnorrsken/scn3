import { api, dialogs, onProgress, SummaryRef, type Progress, type View, type Volume } from "./backend";
import {
  FULL, findArc, formatBytes, geometry, hitTest, layout, lerpDomain, MAX_DEPTH, minFraction, pathOf, zoomScene,
  type Arc, type Domain, type Geometry,
} from "./layout";
import { Nav } from "./nav";
import { drawArcs, drawCenter, drawHighlight, drawLabels, type Palette } from "./render";

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
let summaryMode = false;
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
    dark: document.documentElement.dataset.theme === "dark",
    bg: v("--bg"),
    text: v("--text"),
    quiet: v("--quiet"),
    outline: v("--line"),
    accent: v("--accent"),
  };
}
let pal: Palette;

const systemDark = window.matchMedia("(prefers-color-scheme: dark)");
function applyTheme(): void {
  const dark = themeChoice === "dark" || (themeChoice === "system" && systemDark.matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
  pal = palette();
  dirty = true;
  schedule();
}
systemDark.addEventListener("change", applyTheme);

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
      drawLabels(baseCtx, rings, geom, pal);
      drawCenter(baseCtx, geom, pal, view.root.name, formatBytes(view.root.size));
    }
  }
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.drawImage(base, 0, 0);
  if (hover) {
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    drawHighlight(ctx, hover, geom, pal);
  }
}

function resize(): void {
  const box = $("stage").getBoundingClientRect();
  dpr = window.devicePixelRatio || 1;
  for (const c of [canvas, base]) {
    c.width = Math.round(box.width * dpr);
    c.height = Math.round(box.height * dpr);
  }
  const R = geom.R;
  geom = geometry(box.width, box.height);
  dirty = true;
  schedule();
  // The cull limit depends on the radius, so a bigger chart can show more.
  if (geom.R !== R && view) refetchSoon(150);
}
new ResizeObserver(resize).observe($("stage"));

// ---- view ----

const fetchView = (ref: number): Promise<View> => api().View(ref, MAX_DEPTH, minFraction(geom.R));

function show(v: View): void {
  view = v;
  rings = layout(v.root, geom.R);
  hover = null;
  dirty = true;
  $("empty").hidden = true;
  updateChrome();
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
  else if (summaryMode && nav.current !== SummaryRef) void navigate(SummaryRef);
}

function back(): void {
  const ref = nav.back();
  if (ref !== null) void navigate(ref, false);
}

async function scan(path: string): Promise<void> {
  summaryMode = false;
  $<HTMLInputElement>("summary").checked = false;
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
    if (ref === SummaryRef) {
      await startSummaryScans(true);
    } else {
      await api().Rescan(ref);
      progressScan = ref;
    }
    updateChrome();
    refetchSoon(50);
  } catch (err) {
    report(err);
  }
}

async function startSummaryScans(all: boolean): Promise<void> {
  if (all) active.clear();
  for (const v of volumes) {
    if (all || !scans.has(v.path)) {
      scans.set(v.path, await api().Scan(v.path));
    }
  }
}

async function enterSummary(): Promise<void> {
  summaryMode = true;
  try {
    await startSummaryScans(false);
    nav.reset(SummaryRef);
    show(await fetchView(SummaryRef));
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
    await api().Trash(arc.ref);
    await refresh();
    void loadVolumes();
  } catch (err) {
    report(err);
  }
}

async function hide(arc: Arc): Promise<void> {
  try {
    await api().Hide(arc.ref);
    await refresh();
  } catch (err) {
    report(err);
  }
}

const arcPath = (a: Arc): string => (view ? pathOf(a, view.root.path ?? "", view.sep) : a.name);

// ---- chrome ----

function updateChrome(): void {
  const v = view;
  $<HTMLButtonElement>("up").disabled = !v || (v.parent < 0 && !(summaryMode && nav.current !== SummaryRef));
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
  denied.hidden = !v?.denied;
  if (v?.denied) {
    $("denied-text").textContent = `${v.denied} folder${v.denied === 1 ? "" : "s"} could not be read, so totals are low.`;
    $("privacy").hidden = platform !== "darwin";
  }
  showInfo();
  renderVolumes();
}

function showInfo(): void {
  const info = $("info");
  const v = view;
  if (!v) {
    info.textContent = "";
    return;
  }
  const a = hover;
  if (!a) {
    info.textContent = `${v.root.path || v.root.name}  ·  ${formatBytes(v.root.size)}  ·  ${v.root.files.toLocaleString()} files`;
    return;
  }
  if (a.kind === "free") {
    info.textContent = `Free on ${v.root.name}: ${formatBytes(a.size)}`;
    return;
  }
  if (a.kind === "unscanned") {
    info.textContent = `Used on ${v.root.name} but not found by the scan: ${formatBytes(a.size)}`;
    return;
  }
  const count = (n: number) => `${n.toLocaleString()} ${n === 1 ? "file" : "files"}`;
  const parentSize = a.parent?.size ?? v.root.size;
  const pct = (part: number, whole: number) => (whole > 0 ? `${((100 * part) / whole).toFixed(1)}%` : "–");
  const where = a.kind === "other" ? `Small items in ${arcPath(a)}` : a.kind === "files" ? `Files directly in ${arcPath(a)}` : arcPath(a);
  info.textContent = `${where}  ·  ${formatBytes(a.size)}  ·  ${count(a.files)}  ·  ${pct(a.size, parentSize)} of parent  ·  ${pct(a.size, v.volumeBytes)} of volume`;
}

function renderVolumes(): void {
  const list = $("volumes");
  list.replaceChildren();
  for (const vol of volumes) {
    const li = document.createElement("li");
    const b = document.createElement("button");
    const ref = scans.get(vol.path);
    b.className = !summaryMode && ref !== undefined && view?.crumbs[0]?.ref === ref ? "active" : "";
    const used = vol.capacity - vol.free;
    const name = document.createElement("span");
    name.textContent = vol.name;
    const small = document.createElement("small");
    small.textContent = `${formatBytes(vol.free)} free of ${formatBytes(vol.capacity)}`;
    const meter = document.createElement("div");
    meter.className = "meter";
    const fill = document.createElement("div");
    fill.style.width = `${(100 * used) / vol.capacity}%`;
    meter.append(fill);
    b.append(name, small, meter);
    b.onclick = () => {
      if (ref !== undefined && !summaryMode) {
        nav.go(ref);
        void refresh();
      } else if (ref !== undefined) {
        summaryMode = false;
        $<HTMLInputElement>("summary").checked = false;
        nav.reset(ref);
        void refresh();
      } else {
        void scan(vol.path);
      }
    };
    li.append(b);
    list.append(li);
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
  // In the Summary several volumes scan at once, so show their sum.
  const shown = summaryMode ? [...active.values()] : [p];
  const files = shown.reduce((n, x) => n + x.files, 0);
  const bytes = shown.reduce((n, x) => n + x.bytes, 0);
  const finished = shown.every((x) => x.done);
  $("progress-text").textContent = `${finished ? "Finished" : "Scanning…"} ${files.toLocaleString()} files, ${formatBytes(bytes)}`;
  const bar = $("progress-bar");
  const target = summaryMode ? volumes : volumes.filter((v) => scans.get(v.path) === p.scan);
  const used = target.reduce((n, v) => n + v.capacity - v.free, 0);
  if (used > 0) {
    bar.classList.remove("unknown");
    bar.style.width = `${Math.min(100, (100 * bytes) / used)}%`;
  } else {
    bar.style.width = "";
    bar.classList.add("unknown");
  }
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

canvas.addEventListener("mousemove", (e) => {
  if (tween) return;
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
  showInfo();
  schedule();
});

canvas.addEventListener("click", (e) => {
  closeMenu();
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
  const isDir = arc.kind === "dir";
  const busy = !!view?.scanning;
  const items: [string, () => void, boolean, string?][] = [
    ["Zoom", () => void navigate(arc.ref), isDir],
    ["Open in file manager", () => void api().Reveal(arc.ref).catch(report), true],
    ["Rescan", () => void rescan(arc.ref), !busy],
    ["Hide", () => void hide(arc), isDir && !busy],
    ["Move to Trash…", () => void trash(arc), isDir && !busy, "danger"],
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
  if (e.key === "Escape") closeMenu();
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
  if (summaryMode) {
    for (const ref of scans.values()) void api().Cancel(ref);
    return;
  }
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
$<HTMLInputElement>("summary").onchange = (e) => {
  if ((e.target as HTMLInputElement).checked) void enterSummary();
  else {
    summaryMode = false;
    const first = volumes.map((v) => scans.get(v.path)).find((r) => r !== undefined);
    if (first !== undefined) {
      nav.reset(first);
      void refresh();
    }
  }
};
$<HTMLSelectElement>("theme").onchange = (e) => {
  themeChoice = (e.target as HTMLSelectElement).value;
  applyTheme();
  void api().SetTheme(themeChoice).catch(report);
};

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
    $<HTMLSelectElement>("theme").value = themeChoice;
    applyTheme();
  } catch (err) {
    report(err);
  }
  await loadVolumes();
}

void start();
