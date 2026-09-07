import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const progressVariants = cva(
  "relative h-4 w-full overflow-hidden rounded-full bg-secondary",
  {
    variants: {
      variant: {
        default: "bg-slate-700",
        gradient: "bg-gradient-to-r from-red-600 to-orange-600",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

export interface ProgressProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof progressVariants> {
  value?: number
}

function Progress({ className, value, variant, ...props }: ProgressProps) {
  return (
    <div
      className={cn(progressVariants({ variant, className }))}
      role="progressbar"
      aria-valuenow={value}
      {...props}
    >
      <div
        className="h-full w-full flex-1 bg-white transition-all"
        style={{ transform: `translateX(-${100 - (value ?? 0)}%)` }}
      />
    </div>
  )
}

export { Progress, progressVariants }
