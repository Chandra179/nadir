import type { ReactNode } from "react";

type Props = {
  children: ReactNode;
  tone?: "error" | "info" | "warning";
};

const styles: Record<NonNullable<Props["tone"]>, string> = {
  error: "feedback feedback-err",
  info: "feedback feedback-ok",
  warning: "feedback feedback-err",
};

export default function Notice({ children, tone = "info" }: Props) {
  return <div className={styles[tone]} role={tone === "error" ? "alert" : undefined}>{children}</div>;
}
