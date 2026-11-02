// Radial partition, hit-testing and zoom scenes. No DOM here, so it is testable.

import type { Kind, ViewNode } from "./backend";

export const TAU = Math.PI * 2;
export const MAX_DEPTH = 8;
/** A segment thinner than this at the outer radius is not drawn. */
export const MIN_ARC_PX = 1.5;

export interface Arc {
  ref: number;
  name: string;
  size: number;
  files: number;
  kind: Kind;
  denied: boolean;
  /** Angles in radians, clockwise from 12 o'clock. */
  a0: number;
  a1: number;
  ring: number;
  hue: number;
  /** Newest modification inside, Unix seconds; 0 when unknown. */
  mtime: number;
  /** Age among its siblings: 0 = oldest (cold) to 1 = newest (hot); -1 for none. */
  heat: number;
  parent: Arc | null;
  node: ViewNode | null;
}

export interface Geometry {
  cx: number;
  cy: number;
  R: number;
  r0: number;
  w: number;
}

export function geometry(width: number, height: number, maxDepth = MAX_DEPTH): Geometry {
  const R = Math.max(40, Math.min(width, height) / 2 - 8);
  const r0 = Math.max(44, R * 0.16);
  return { cx: width / 2, cy: height / 2, R, r0, w: (R - r0) / maxDepth };
}

/** The share of a full circle below which a segment is too thin to draw. */
export const minFraction = (R: number): number => MIN_ARC_PX / (TAU * R);

const pinned = (k: Kind): boolean => k === "free" || k === "unscanned";

/**
 * Lays out the kids of root into rings, each sorted by start angle. Loose files
 * below the center have no segment, so kids may add up to less than their
 * parent: that part of the parent's angle stays empty.
 */
export function layout(root: ViewNode, R: number, maxDepth = MAX_DEPTH): Arc[][] {
  const rings: Arc[][] = Array.from({ length: maxDepth }, () => []);
  const minAngle = MIN_ARC_PX / R;

  const place = (node: ViewNode, a0: number, a1: number, ring: number, parent: Arc | null, hue: number) => {
    if (ring >= maxDepth || !node.kids?.length) return;
    const total = Math.max(node.size, node.kids.reduce((s, k) => s + k.size, 0));
    if (total <= 0) return;
    const span = a1 - a0;
    let at = a0;
    let topHue = 0;
    let merged: { size: number; files: number } | null = null;
    // Children are placed after the whole group, so that the group's heat is
    // known when theirs is worked out.
    const group: Arc[] = [];
    const later: [ViewNode, Arc][] = [];

    const push = (k: ViewNode | null, name: string, kind: Kind, size: number, files: number) => {
      const end = at + (span * size) / total;
      const h = ring === 0 ? topHue++ : hue;
      const arc: Arc = {
        ref: k ? k.ref : -1,
        name,
        size,
        files,
        kind,
        denied: !!k?.denied,
        a0: at,
        a1: end,
        ring,
        hue: h,
        mtime: k?.mtime ?? 0,
        heat: -1,
        parent,
        node: k,
      };
      rings[ring].push(arc);
      group.push(arc);
      if (k && kind === "dir") later.push([k, arc]);
      at = end;
    };

    for (const k of node.kids) {
      const angle = (span * k.size) / total;
      if (k.kind === "other" || (angle < minAngle && !pinned(k.kind))) {
        merged = merged ?? { size: 0, files: 0 };
        merged.size += k.size;
        merged.files += k.files;
      } else if (!pinned(k.kind)) {
        push(k, k.name, k.kind, k.size, k.files);
      }
    }
    if (merged) push(null, "other", "other", merged.size, merged.files);
    // Free space and the unaccounted rest always come last.
    for (const k of node.kids) {
      if (pinned(k.kind)) push(k, k.name, k.kind, k.size, k.files);
    }
    setHeat(group, parent?.heat ?? -1);
    for (const [k, arc] of later) place(k, arc.a0, arc.a1, ring + 1, arc, arc.hue);
  };

  place(root, 0, TAU, 0, null, 0);
  return rings;
}

/**
 * Gives siblings a heat from their modification times, relative to each other:
 * 1 for the newest, down to 0 for the oldest. Age is counted in days from the
 * newest, on a log scale, so one ancient item does not make the rest look new.
 * With nothing to compare (one item, or all the same age), an item takes its
 * parent's heat, or the middle at the top.
 */
export function setHeat(siblings: Arc[], parentHeat = -1): void {
  const dated = siblings.filter((a) => (a.kind === "dir" || a.kind === "file") && a.mtime > 0);
  if (!dated.length) return;
  // A file dated ahead counts as changed now, so it does not push the rest cold.
  const now = Date.now() / 1000;
  const time = (a: Arc) => Math.min(a.mtime, now);
  const newest = Math.max(...dated.map(time));
  const oldest = Math.min(...dated.map(time));
  // Days, not seconds: in seconds the log squeezes "ten days old" next to "three years old".
  const days = (s: number) => Math.log1p(s / 86400);
  const span = days(newest - oldest);
  const fallback = parentHeat >= 0 ? parentHeat : 0.5;
  for (const a of dated) a.heat = span > 0 ? 1 - days(newest - time(a)) / span : fallback;
}

/** The "scanned only" band sits inside the center circle, between these shares of r0. */
export const WEDGE_IN = 0.74;
export const WEDGE_OUT = 0.92;

/**
 * Where the scanned part of a volume ends: the start of the first free or
 * not-scanned arc around the center. Null when there is none.
 */
export function scannedEnd(rings: Arc[][]): number | null {
  const first = rings[0]?.find((a) => pinned(a.kind));
  return first ? first.a0 : null;
}

/** Whether a point is on the band from 12 o'clock to angle end. */
export function inWedge(g: Geometry, x: number, y: number, end: number): boolean {
  const dx = x - g.cx;
  const dy = y - g.cy;
  const r = Math.hypot(dx, dy);
  if (r < g.r0 * WEDGE_IN || r > g.r0 * WEDGE_OUT) return false;
  let theta = Math.atan2(dx, -dy);
  if (theta < 0) theta += TAU;
  return theta <= end;
}

export type Hit = { center: true } | { center: false; arc: Arc } | null;

/** Finds what is under a point. Polar lookup, so it works at any DPI. */
export function hitTest(rings: Arc[][], g: Geometry, x: number, y: number): Hit {
  const dx = x - g.cx;
  const dy = y - g.cy;
  const r = Math.hypot(dx, dy);
  if (r < g.r0) return { center: true };
  if (r >= g.R) return null;
  const ring = rings[Math.floor((r - g.r0) / g.w)];
  if (!ring?.length) return null;
  // atan2 from 12 o'clock, clockwise.
  let theta = Math.atan2(dx, -dy);
  if (theta < 0) theta += TAU;
  let lo = 0;
  let hi = ring.length - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (ring[mid].a0 <= theta) lo = mid;
    else hi = mid - 1;
  }
  const arc = ring[lo];
  return arc.a0 <= theta && theta < arc.a1 ? { center: false, arc } : null;
}

export const findArc = (rings: Arc[][], ref: number): Arc | null => {
  for (const ring of rings) {
    for (const a of ring) if (a.ref === ref && a.kind === "dir") return a;
  }
  return null;
};

/** What the screen shows: an angle window and how many rings are scrolled away. */
export interface Domain {
  a0: number;
  a1: number;
  depth: number;
}

export const FULL: Domain = { a0: 0, a1: TAU, depth: 0 };

export const lerpDomain = (p: Domain, q: Domain, t: number): Domain => ({
  a0: p.a0 + (q.a0 - p.a0) * t,
  a1: p.a1 + (q.a1 - p.a1) * t,
  depth: p.depth + (q.depth - p.depth) * t,
});

/**
 * Builds one list of arcs for a zoom tween: the outer view, with the inner view
 * fitted inside its arc `at`. Showing FULL gives the outer view; showing the
 * returned domain gives the inner view.
 */
export function zoomScene(outer: Arc[][], inner: Arc[][], at: Arc): { arcs: Arc[]; domain: Domain } {
  const arcs: Arc[] = [];
  for (const ring of outer) {
    for (const a of ring) {
      const below = a.ring > at.ring && a.a0 >= at.a0 - 1e-9 && a.a1 <= at.a1 + 1e-9;
      if (!below) arcs.push(a);
    }
  }
  const scale = (at.a1 - at.a0) / TAU;
  for (const ring of inner) {
    for (const a of ring) {
      arcs.push({
        ...a,
        a0: at.a0 + a.a0 * scale,
        a1: at.a0 + a.a1 * scale,
        ring: at.ring + 1 + a.ring,
      });
    }
  }
  return { arcs, domain: { a0: at.a0, a1: at.a1, depth: at.ring + 1 } };
}

const HUES = [0, 120, 48, 212, 285, 28, 170, 330, 85, 245];

export type Mode = "light" | "dark" | "contrast";
export type Colors = "folders" | "age";

type RGB = [number, number, number];
const hex = (h: string): RGB => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16)) as RGB;

/** Matplotlib's plasma and viridis at nine even steps. */
const PLASMA = ["#0d0887", "#4b03a1", "#7d03a8", "#a82296", "#cb4679", "#e56b5d", "#f89441", "#fdc328", "#f0f921"].map(hex);
const VIRIDIS = ["#440154", "#482878", "#3e4989", "#31688e", "#26828e", "#1f9e89", "#35b779", "#6ece58", "#fde725"].map(hex);
/** Blue, yellow, red, with a yellow strong enough to stand out on a light background. */
const BLUE_YELLOW_RED = ["#2f5fa8", "#5b93c9", "#9cc3df", "#f2d14c", "#f39a3d", "#d2452e", "#a31f1f"].map(hex);

/** Age colours per theme: the scale, and where on it the oldest item starts. */
const HEAT: Record<Mode, { stops: RGB[]; from: number }> = {
  light: { stops: BLUE_YELLOW_RED, from: 0 },
  // The darkest end would vanish on a dark background, so start a bit in.
  dark: { stops: PLASMA, from: 0.18 },
  contrast: { stops: VIRIDIS, from: 0.22 },
};

export function heatColor(heat: number, mode: Mode): string {
  const { stops, from } = HEAT[mode];
  const t = from + (1 - from) * Math.min(Math.max(heat, 0), 1);
  const x = t * (stops.length - 1);
  const i = Math.min(Math.floor(x), stops.length - 2);
  const f = x - i;
  const c = stops[i].map((v, k) => Math.round(v + (stops[i + 1][k] - v) * f));
  return `rgb(${c[0]} ${c[1]} ${c[2]})`;
}

/** Black or white, whichever reads better on the given rgb() or hsl() colour. */
export function inkOn(color: string): string {
  const rgb = /^rgb\((\d+) (\d+) (\d+)\)$/.exec(color);
  let light: number;
  if (rgb) {
    // Relative luminance, with the sRGB curve.
    const lin = rgb.slice(1).map((v) => {
      const c = Number(v) / 255;
      return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
    });
    light = 0.2126 * lin[0] + 0.7152 * lin[1] + 0.0722 * lin[2];
    return light > 0.22 ? "#111" : "#fff";
  }
  const hsl = /(\d+(?:\.\d+)?)%\)$/.exec(color);
  light = hsl ? Number(hsl[1]) : 50;
  return light > 55 ? "#111" : "#fff";
}

/** Colour by top-level branch, lighter with depth; or by age among siblings. */
export function arcColor(a: Pick<Arc, "hue" | "ring" | "kind" | "denied" | "heat">, mode: Mode, colors: Colors = "folders"): string {
  if (a.kind === "other" || a.kind === "unscanned" || a.denied || (colors === "age" && a.heat < 0)) {
    return { light: "hsl(0 0% 72%)", dark: "hsl(0 0% 34%)", contrast: "hsl(0 0% 62%)" }[mode];
  }
  if (colors === "age") return heatColor(a.heat, mode);
  const hue = HUES[a.hue % HUES.length];
  if (mode === "contrast") {
    // Bright, fully saturated colours on black, light enough for black labels.
    const light = Math.min(52 + a.ring * 4, 80);
    return `hsl(${hue} ${a.kind === "file" ? 60 : 100}% ${light}%)`;
  }
  const dark = mode === "dark";
  const light = Math.min((dark ? 36 : 46) + a.ring * (dark ? 5 : 6), 88);
  return `hsl(${hue} ${a.kind === "file" ? 30 : 72}% ${light}%)`;
}

export function pathOf(a: Arc, rootPath: string, sep: string): string {
  const parts: string[] = [];
  let base = rootPath;
  for (let p: Arc | null = a; p; p = p.parent) {
    if (p.node?.path) {
      base = p.node.path;
      break;
    }
    if (p.kind === "dir") parts.unshift(p.name);
  }
  if (!parts.length) return base;
  return (base.endsWith(sep) ? base : base + sep) + parts.join(sep);
}

/**
 * Shortens text to fit maxWidth by cutting out the middle, keeping about a
 * third from the start and two thirds from the end: for a path, the drive and
 * the file name matter most. measure returns the drawn width of a string.
 */
export function fitMiddle(text: string, maxWidth: number, measure: (s: string) => number): string {
  if (measure(text) <= maxWidth) return text;
  const cut = (n: number) => {
    const head = Math.floor(n / 3);
    return text.slice(0, head) + "…" + text.slice(text.length - (n - head));
  };
  // The most characters that still fit, found by halving.
  let lo = 0;
  let hi = text.length - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (measure(cut(mid)) <= maxWidth) lo = mid;
    else hi = mid - 1;
  }
  return cut(lo);
}

/** A local date as YYYY-MM-DD, which reads the same in every country. */
export function formatDate(unixSeconds: number): string {
  const d = new Date(unixSeconds * 1000);
  const two = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}`;
}

export function formatBytes(n: number): string {
  const units = ["bytes", "KB", "MB", "GB", "TB", "PB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? v : v >= 100 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`;
}
