// Draws arcs on a canvas. Coordinates are CSS pixels; the caller scales for DPR.

import { arcColor, FULL, TAU, type Arc, type Domain, type Geometry } from "./layout";

export interface Palette {
  dark: boolean;
  bg: string;
  text: string;
  quiet: string;
  outline: string;
  accent: string;
}

const LABEL_FONT = "11px system-ui, -apple-system, 'Segoe UI', sans-serif";

/** Screen angles and radii of an arc under a domain, or null if off-screen. */
function project(a: Arc, g: Geometry, d: Domain) {
  const k = TAU / (d.a1 - d.a0);
  const s = Math.max(0, (a.a0 - d.a0) * k);
  const e = Math.min(TAU, (a.a1 - d.a0) * k);
  if (e <= s) return null;
  const rIn = g.r0 + (a.ring - d.depth) * g.w;
  const rOut = rIn + g.w;
  if (rOut <= g.r0 || rIn >= g.R) return null;
  return { s, e, rIn: Math.max(g.r0, rIn), rOut: Math.min(g.R, rOut) };
}

function addSegment(p: Path2D, g: Geometry, s: number, e: number, rIn: number, rOut: number) {
  const a = s - Math.PI / 2;
  const b = e - Math.PI / 2;
  p.moveTo(g.cx + rOut * Math.cos(a), g.cy + rOut * Math.sin(a));
  p.arc(g.cx, g.cy, rOut, a, b);
  p.arc(g.cx, g.cy, rIn, b, a, true);
  p.closePath();
}

/** Fills arcs. Same-colour arcs share one path, which is far cheaper than one fill each. */
export function drawArcs(ctx: CanvasRenderingContext2D, arcs: Arc[], g: Geometry, pal: Palette, d: Domain = FULL): void {
  const fills = new Map<string, Path2D>();
  const seams = new Path2D();
  const outlines = new Path2D();
  for (const a of arcs) {
    const q = project(a, g, d);
    if (!q) continue;
    if (a.kind === "free") {
      addSegment(outlines, g, q.s, q.e, q.rIn, q.rOut);
      continue;
    }
    const color = arcColor(a, pal.dark);
    let path = fills.get(color);
    if (!path) fills.set(color, (path = new Path2D()));
    addSegment(path, g, q.s, q.e, q.rIn, q.rOut);
    if ((q.e - q.s) * q.rOut > 3) addSegment(seams, g, q.s, q.e, q.rIn, q.rOut);
  }
  for (const [color, path] of fills) {
    ctx.fillStyle = color;
    ctx.fill(path);
  }
  ctx.lineWidth = 1;
  ctx.strokeStyle = pal.bg;
  ctx.stroke(seams);
  ctx.strokeStyle = pal.outline;
  ctx.stroke(outlines);
}

/** Puts names on segments that have room, along the tangent. */
export function drawLabels(ctx: CanvasRenderingContext2D, rings: Arc[][], g: Geometry, pal: Palette): void {
  if (g.w < 16) return;
  ctx.font = LABEL_FONT;
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  for (const ring of rings) {
    for (const a of ring) {
      if (a.kind === "other") continue;
      const r = g.r0 + (a.ring + 0.5) * g.w;
      const room = (a.a1 - a.a0) * r - 8;
      if (room < 28) continue;
      const text = fit(ctx, a.name, room);
      if (!text) continue;
      const mid = (a.a0 + a.a1) / 2;
      const x = g.cx + r * Math.sin(mid);
      const y = g.cy - r * Math.cos(mid);
      const flip = mid > Math.PI / 2 && mid < (3 * Math.PI) / 2;
      ctx.save();
      ctx.translate(x, y);
      ctx.rotate(flip ? mid + Math.PI : mid);
      ctx.fillStyle = a.kind === "free" ? pal.quiet : pal.dark ? "#fff" : "#111";
      ctx.fillText(text, 0, 0);
      ctx.restore();
    }
  }
}

function fit(ctx: CanvasRenderingContext2D, name: string, room: number): string | null {
  if (ctx.measureText(name).width <= room) return name;
  // Average glyph width gives a good first cut, so this rarely loops long.
  let n = Math.floor((name.length * room) / ctx.measureText(name).width);
  for (; n >= 3; n--) {
    const t = name.slice(0, n) + "…";
    if (ctx.measureText(t).width <= room) return t;
  }
  return null;
}

export function drawCenter(ctx: CanvasRenderingContext2D, g: Geometry, pal: Palette, name: string, size: string): void {
  ctx.beginPath();
  ctx.arc(g.cx, g.cy, g.r0 - 1, 0, TAU);
  ctx.strokeStyle = pal.outline;
  ctx.lineWidth = 1;
  ctx.stroke();
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.fillStyle = pal.text;
  ctx.font = "600 13px system-ui, -apple-system, 'Segoe UI', sans-serif";
  const room = g.r0 * 1.7;
  ctx.fillText(fit(ctx, name, room) ?? "", g.cx, g.cy - 9);
  ctx.fillStyle = pal.quiet;
  ctx.font = "12px system-ui, -apple-system, 'Segoe UI', sans-serif";
  ctx.fillText(size, g.cx, g.cy + 9);
}

/** Outlines an arc and its ancestors. */
export function drawHighlight(ctx: CanvasRenderingContext2D, arc: Arc, g: Geometry, pal: Palette): void {
  const chain = new Path2D();
  for (let p = arc.parent; p; p = p.parent) {
    addSegment(chain, g, p.a0, p.a1, g.r0 + p.ring * g.w, g.r0 + (p.ring + 1) * g.w);
  }
  ctx.strokeStyle = pal.accent;
  ctx.lineWidth = 1;
  ctx.stroke(chain);
  const self = new Path2D();
  addSegment(self, g, arc.a0, arc.a1, g.r0 + arc.ring * g.w, g.r0 + (arc.ring + 1) * g.w);
  ctx.lineWidth = 2;
  ctx.stroke(self);
}
