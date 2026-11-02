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

/** Lays out the kids of root into rings, each sorted by start angle. */
export function layout(root: ViewNode, R: number, maxDepth = MAX_DEPTH): Arc[][] {
  const rings: Arc[][] = Array.from({ length: maxDepth }, () => []);
  const minAngle = MIN_ARC_PX / R;

  const place = (node: ViewNode, a0: number, a1: number, ring: number, parent: Arc | null, hue: number) => {
    if (ring >= maxDepth || !node.kids?.length) return;
    const total = node.kids.reduce((s, k) => s + k.size, 0);
    if (total <= 0) return;
    const span = a1 - a0;
    let at = a0;
    let topHue = 0;
    let merged: { size: number; files: number } | null = null;

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
        parent,
        node: k,
      };
      rings[ring].push(arc);
      if (k && kind === "dir") place(k, at, end, ring + 1, arc, h);
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
  };

  place(root, 0, TAU, 0, null, 0);
  return rings;
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

/** Colour by top-level branch; lighter with depth. */
export function arcColor(a: Pick<Arc, "hue" | "ring" | "kind" | "denied">, dark: boolean): string {
  if (a.kind === "other" || a.kind === "unscanned" || a.denied) {
    return dark ? "hsl(0 0% 34%)" : "hsl(0 0% 72%)";
  }
  const hue = HUES[a.hue % HUES.length];
  const base = dark ? 36 : 46;
  const light = Math.min(base + a.ring * (dark ? 5 : 6), 88);
  return `hsl(${hue} ${a.kind === "files" ? 30 : 72}% ${light}%)`;
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
