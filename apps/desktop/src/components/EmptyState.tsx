import type { ReactNode } from "react";

export function EmptyState({ title, description, children }: { title: string; description: string; children?: ReactNode }) {
  return (
    <section className="empty-state" aria-labelledby={`${title}-title`}>
      <h2 id={`${title}-title`}>{title}</h2>
      <p>{description}</p>
      {children}
    </section>
  );
}
