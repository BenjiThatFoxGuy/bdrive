import { Toaster as HotToaster, toast, resolveValue } from "react-hot-toast";
import IconMaterialSymbolsClose from "~icons/material-symbols/close";
import IconMaterialSymbolsCheckCircleRounded from "~icons/material-symbols/check-circle-rounded";
import IconMaterialSymbolsErrorRounded from "~icons/material-symbols/error-rounded";
import { Spinner } from "@heroui/react";
import { Button } from "@heroui/react";
import clsx from "clsx";

export function Toaster() {
  return (
    <HotToaster position="bottom-right">
      {(t) => (
        <div
          className={clsx(
            "flex items-center gap-3 pl-4 pr-2 py-2 min-h-[48px] rounded-lg shadow-lg transition-all duration-300 ease-in-out",
            "bg-foreground text-background",
            t.visible ? "opacity-100 translate-y-0 scale-100" : "opacity-0 translate-y-2 scale-95",
          )}
        >
          <div className="flex-shrink-0 flex items-center justify-center size-6">
            {t.type === "success" && (
              <IconMaterialSymbolsCheckCircleRounded className="size-5 text-accent" />
            )}
            {t.type === "error" && (
              <IconMaterialSymbolsErrorRounded className="size-5 text-danger" />
            )}
            {t.type === "loading" && <Spinner />}
          </div>
          <div className="text-sm font-medium flex-grow pr-2 min-w-[200px]">
            {resolveValue(t.message, t)}
          </div>
          {t.type !== "loading" && (
            <Button
              isIconOnly
              variant="ghost"
              className="min-w-10"
              onPress={() => toast.dismiss(t.id)}
            >
              <IconMaterialSymbolsClose className="size-5" />
            </Button>
          )}
        </div>
      )}
    </HotToaster>
  );
}
