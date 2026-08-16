import type { RuntimeState } from "../lib/desktop-api";

export function StatusDot({ state }: { state: RuntimeState | "loading" }) {
  return <span className={`status-dot status-${state}`} aria-hidden="true" />;
}
