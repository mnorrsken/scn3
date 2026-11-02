import { expect, it } from "vitest";
import { Nav } from "./nav";

it("goes back through history, skipping repeats", () => {
  const n = new Nav();
  n.go(1);
  n.go(2);
  n.go(2);
  n.go(3);
  expect(n.back()).toBe(2);
  expect(n.current).toBe(2);
  expect(n.back()).toBe(1);
  expect(n.canGoBack).toBe(false);
  expect(n.back()).toBeNull();
  expect(n.current).toBe(1);
});

it("forgets history on reset", () => {
  const n = new Nav();
  n.go(1);
  n.go(2);
  n.reset(9);
  expect(n.canGoBack).toBe(false);
  expect(n.current).toBe(9);
});
