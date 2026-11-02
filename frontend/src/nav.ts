/** The current root and the Back history. */
export class Nav {
  current: number | null = null;
  private history: number[] = [];

  go(ref: number): void {
    if (this.current !== null && this.current !== ref) this.history.push(this.current);
    this.current = ref;
  }

  back(): number | null {
    const prev = this.history.pop();
    if (prev === undefined) return null;
    this.current = prev;
    return prev;
  }

  get canGoBack(): boolean {
    return this.history.length > 0;
  }

  /** Starts over at ref, for a new scan: old refs no longer exist. */
  reset(ref: number): void {
    this.history = [];
    this.current = ref;
  }
}
