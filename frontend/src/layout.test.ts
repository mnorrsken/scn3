import { describe, expect, it } from "vitest";
import type { Kind, ViewNode } from "./backend";
import { arcColor, findArc, formatBytes, FULL, geometry, hitTest, layout, lerpDomain, pathOf, TAU, zoomScene } from "./layout";

const n = (name: string, size: number, kids?: ViewNode[], kind: Kind = "dir", ref = 1): ViewNode => ({
  ref,
  name,
  size,
  files: 1,
  kind,
  kids,
});

const tree = n("root", 100, [
  n("a", 60, [n("a1", 40, undefined, "dir", 11), n("files here", 20, undefined, "files", 2)], "dir", 2),
  n("b", 40, undefined, "dir", 3),
]);

describe("layout", () => {
  it("gives angles in proportion to size, clockwise from zero", () => {
    const r = layout(tree, 300);
    expect(r[0].map((a) => a.name)).toEqual(["a", "b"]);
    expect(r[0][0].a0).toBe(0);
    expect(r[0][0].a1).toBeCloseTo(TAU * 0.6);
    expect(r[0][1].a1).toBeCloseTo(TAU);
    expect(r[1][1].a1).toBeCloseTo(TAU * 0.6);
  });

  it("puts free space last, even if it is the largest", () => {
    const r = layout(n("vol", 100, [n("used", 10), n("not scanned", 5, undefined, "unscanned", -1), n("free", 85, undefined, "free", -1)]), 300);
    expect(r[0].map((a) => a.kind)).toEqual(["dir", "unscanned", "free"]);
    expect(r[0][2].a1).toBeCloseTo(TAU);
    expect(r[0][0].a1).toBeCloseTo(TAU * 0.1);
  });

  it("merges segments that are too thin into one other, keeping the sum", () => {
    const kids = [n("big", 1000)];
    for (let i = 0; i < 50; i++) kids.push(n(`t${i}`, 0.01));
    const r = layout(n("r", 1000.5, kids), 300);
    expect(r[0].map((a) => a.name)).toEqual(["big", "other"]);
    expect(r[0][1].a1).toBeCloseTo(TAU);
    expect(r[0][1].size).toBeCloseTo(0.5);
  });

  it("stops at the depth limit", () => {
    let node = n("leaf", 1);
    for (let i = 0; i < 12; i++) node = n(`d${i}`, 1, [node]);
    const r = layout(node, 300, 8);
    expect(r.length).toBe(8);
    expect(r[7].length).toBe(1);
  });

  it("handles an empty folder", () => {
    expect(layout(n("r", 0, []), 300).flat()).toEqual([]);
  });

  it("colours by branch and gets lighter with depth", () => {
    const r = layout(tree, 300);
    expect(r[1][0].hue).toBe(r[0][0].hue);
    expect(r[0][1].hue).not.toBe(r[0][0].hue);
    const light = (s: string) => Number(/(\d+)%\)$/.exec(s)![1]);
    expect(light(arcColor(r[1][0], false))).toBeGreaterThan(light(arcColor(r[0][0], false)));
  });
});

describe("hitTest", () => {
  const g = geometry(800, 800);
  const rings = layout(tree, g.R);
  const at = (angle: number, ring: number) => {
    const r = g.r0 + (ring + 0.5) * g.w;
    return [g.cx + r * Math.sin(angle), g.cy - r * Math.cos(angle)] as const;
  };

  it("finds the segment under each point of every quadrant", () => {
    for (const [angle, name] of [[0.1, "a"], [TAU * 0.3, "a"], [TAU * 0.59, "a"], [TAU * 0.61, "b"], [TAU * 0.99, "b"]] as const) {
      const hit = hitTest(rings, g, ...at(angle, 0));
      expect(hit && !hit.center && hit.arc.name).toBe(name);
    }
  });

  it("tells center, outside and empty gaps apart", () => {
    expect(hitTest(rings, g, g.cx, g.cy)).toEqual({ center: true });
    expect(hitTest(rings, g, g.cx + g.R + 5, g.cy)).toBeNull();
    // b has no children, so its next ring is empty.
    expect(hitTest(rings, g, ...at(TAU * 0.8, 1))).toBeNull();
  });
});

describe("zoomScene", () => {
  it("fits the inner view exactly into the zoomed arc", () => {
    const outer = layout(tree, 300);
    const a = findArc(outer, 2)!;
    const inner = layout(tree.kids![0], 300);
    const { arcs, domain } = zoomScene(outer, inner, a);
    expect(domain).toEqual({ a0: a.a0, a1: a.a1, depth: 1 });
    const mapped = arcs.filter((x) => x.name === "a1");
    // The old copy of a's child is replaced, not doubled.
    expect(mapped.length).toBe(1);
    expect(mapped[0].ring).toBe(1);
    expect(mapped[0].a1).toBeCloseTo(a.a1 * (40 / 60));
  });

  it("interpolates between domains", () => {
    const d = lerpDomain(FULL, { a0: 1, a1: 2, depth: 2 }, 0.5);
    expect(d.depth).toBe(1);
    expect(d.a0).toBe(0.5);
  });
});

describe("text", () => {
  it("builds paths", () => {
    const r = layout(tree, 300);
    expect(pathOf(r[1][0], "/x", "/")).toBe("/x/a/a1");
    expect(pathOf(r[1][1], "/", "/")).toBe("/a");
    expect(pathOf(r[0][0], "C:\\", "\\")).toBe("C:\\a");
  });

  it("uses a node's own path for a volume in the summary", () => {
    const r = layout(n("all", 1, [{ ...n("vol", 1, [n("sub", 1)]), path: "/mnt/x" }]), 300);
    expect(pathOf(r[1][0], "", "/")).toBe("/mnt/x/sub");
  });

  it("formats sizes", () => {
    expect(formatBytes(512)).toBe("512 bytes");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(1281 * 1024 ** 3)).toBe("1.3 TB");
  });
});
