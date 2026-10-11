import { cn } from "@/utils/cn";

interface Props {
  className?: string;
}

export default function Logo({ className }: Props) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 1000 1000"
      className={cn("shrink-0", className)}
      fill="none"
    >
      <rect width="1000" height="1000" rx="180" className="fill-foreground" />
      <g
        fill="none"
        className="stroke-background"
        strokeWidth="60"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M 224 700 L 224 300 L 384 700 L 384 300" />
        <path d="M 800 354 A 190 190 0 1 0 800 646" />
        <path d="M 678 500 L 800 500" />
      </g>
    </svg>
  );
}
