import type { ReactNode } from "react";

type Props = {
  children: ReactNode;
  tone?: "error" | "info" | "warning";
};

const styles: Record<NonNullable<Props["tone"]>, string> = {
  error: "border-rose-500/30 bg-rose-500/10 text-rose-200",
  info: "border-slate-800 bg-slate-900/60 text-slate-400",
  warning: "border-amber-500/30 bg-amber-500/10 text-amber-200",
};

export default function Notice({ children, tone = "info" }: Props) {
  return <div className={`rounded-xl border px-4 py-3 text-sm ${styles[tone]}`} role={tone === "error" ? "alert" : undefined}>{children}</div>;
}
