import { describe, expect, it } from "vitest";
import type { Kind, ViewNode } from "./backend";
import { arcColor, findArc, formatBytes, formatDate, FULL, geometry, heatColor, hitTest, inkOn, inWedge, scannedEnd, WEDGE_IN, WEDGE_OUT, layout, lerpDomain, pathOf, TAU, zoomScene } from "./layout";

const n = (name: string, size: number, kids?: ViewNode[], kind: Kind = "dir", ref = 1): ViewNode => ({
  ref,
  name,
  size,
  files: 1,
  kind,
  kids,
});

const tree = n("root", 100, [
  n("a", 60, [n("a1", 40, undefined, "dir", 11), n("f.bin", 20, undefined, "file", 2)], "dir", 2),
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

  it("leaves room for loose files that have no segment", () => {
    const r = layout(n("r", 100, [n("a", 100, [n("a1", 40)])]), 300);
    expect(r[1][0].a0).toBe(0);
    expect(r[1][0].a1).toBeCloseTo(TAU * 0.4);
    expect(r[0][0].a1).toBeCloseTo(TAU);
  });

  it("handles an empty folder", () => {
    expect(layout(n("r", 0, []), 300).flat()).toEqual([]);
  });

  it("colours by branch and gets lighter with depth", () => {
    const r = layout(tree, 300);
    expect(r[1][0].hue).toBe(r[0][0].hue);
    expect(r[0][1].hue).not.toBe(r[0][0].hue);
    const light = (s: string) => Number(/(\d+)%\)$/.exec(s)![1]);
    expect(light(arcColor(r[1][0], "light"))).toBeGreaterThan(light(arcColor(r[0][0], "light")));
  });

  it("uses saturated, light colours in high contrast", () => {
    const r = layout(tree, 300);
    const c = arcColor(r[0][0], "contrast");
    expect(c).toMatch(/ 100% /);
    expect(Number(/(\d+)%\)$/.exec(c)![1])).toBeGreaterThanOrEqual(50);
  });
});

describe("age colours", () => {
  const dated = (name: string, mtime: number, kind: Kind = "dir"): ViewNode => ({ ...n(name, 10, undefined, kind), mtime });

  it("ranks siblings from cold to hot, relative to each other", () => {
    const day = 86400;
    const r = layout(n("r", 30, [dated("new", 1000 * day), dated("mid", 990 * day), dated("old", 10 * day, "file")]), 300);
    const heat = Object.fromEntries(r[0].map((a) => [a.name, a.heat]));
    expect(heat.new).toBe(1);
    expect(heat.old).toBe(0);
    expect(heat.mid).toBeGreaterThan(0.4); // ten days older than the newest, out of ~1000: still warm
    expect(heat.mid).toBeLessThan(1);
  });

  it("uses only siblings in the same folder", () => {
    const r = layout(n("r", 20, [
      { ...n("a", 10, [dated("a1", 100), dated("a2", 200)]), mtime: 200 },
      { ...n("b", 10, [dated("b1", 5000), dated("b2", 9000)]), mtime: 9000 },
    ]), 300);
    const heat = Object.fromEntries(r[1].map((x) => [x.name, x.heat]));
    expect(heat.a2).toBe(1); // newest in a, though older than everything in b
    expect(heat.b1).toBe(0);
  });

  it("gives a lone item its parent's heat", () => {
    const r = layout(n("r", 20, [
      { ...n("old", 10, [dated("only", 100)]), mtime: 100 },
      { ...n("new", 10, [dated("x", 9000)]), mtime: 9000 },
    ]), 300);
    const heat = Object.fromEntries(r[1].map((x) => [x.name, x.heat]));
    expect(heat.only).toBe(0);
    expect(heat.x).toBe(1);
  });

  it("puts a lone or undated item in the middle or leaves it grey", () => {
    const r = layout(n("r", 20, [dated("only", 500), n("undated", 10)]), 300);
    expect(r[0].find((a) => a.name === "only")!.heat).toBe(0.5);
    const undated = r[0].find((a) => a.name === "undated")!;
    expect(undated.heat).toBe(-1);
    expect(arcColor(undated, "light", "age")).toBe(arcColor({ ...undated, kind: "other" }, "light"));
  });

  it("goes blue, yellow, red in light mode and up plasma and viridis otherwise", () => {
    expect(heatColor(0, "light")).toBe("rgb(47 95 168)");
    expect(heatColor(0.5, "light")).toBe("rgb(242 209 76)");
    expect(heatColor(1, "light")).toBe("rgb(163 31 31)");
    expect(heatColor(1, "dark")).toBe("rgb(240 249 33)"); // plasma's yellow end
    expect(heatColor(1, "contrast")).toBe("rgb(253 231 37)"); // viridis's yellow end
    // The oldest end is trimmed so it stays visible on dark backgrounds.
    expect(heatColor(0, "dark")).not.toBe("rgb(13 8 135)");
  });

  it("picks readable label ink", () => {
    expect(inkOn("rgb(240 249 33)")).toBe("#111");
    expect(inkOn("rgb(75 3 161)")).toBe("#fff");
    expect(inkOn("hsl(0 72% 46%)")).toBe("#fff");
    expect(inkOn("hsl(120 72% 70%)")).toBe("#111");
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

describe("scanned-only wedge", () => {
  const vol = n("vol", 100, [n("used", 30), n("not scanned", 10, undefined, "unscanned", -1), n("free", 60, undefined, "free", -1)]);
  const g = geometry(800, 800);

  it("ends where free and not-scanned space start", () => {
    expect(scannedEnd(layout(vol, g.R))).toBeCloseTo(TAU * 0.3);
    expect(scannedEnd(layout(tree, g.R))).toBeNull();
  });

  it("is hit only on its band and within its angle", () => {
    const end = TAU * 0.3;
    const r = g.r0 * ((WEDGE_IN + WEDGE_OUT) / 2);
    const at = (angle: number, radius = r) => [g.cx + radius * Math.sin(angle), g.cy - radius * Math.cos(angle)] as const;
    expect(inWedge(g, ...at(0.5), end)).toBe(true);
    expect(inWedge(g, ...at(TAU * 0.5), end)).toBe(false);
    expect(inWedge(g, ...at(0.5, g.r0 * 0.3), end)).toBe(false);
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

  it("uses a node's own path when it has one", () => {
    const r = layout(n("all", 1, [{ ...n("vol", 1, [n("sub", 1)]), path: "/mnt/x" }]), 300);
    expect(pathOf(r[1][0], "", "/")).toBe("/mnt/x/sub");
  });

  it("formats dates the same way everywhere", () => {
    expect(formatDate(new Date(2026, 8, 11, 15, 30).getTime() / 1000)).toBe("2026-09-11");
    expect(formatDate(new Date(2026, 0, 2).getTime() / 1000)).toBe("2026-01-02");
  });

  it("formats sizes", () => {
    expect(formatBytes(512)).toBe("512 bytes");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(1281 * 1024 ** 3)).toBe("1.3 TB");
  });
});
