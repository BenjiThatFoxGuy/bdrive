import { memo, useCallback, useEffect, useState } from "react";
import { ColorSwatchPicker } from "@heroui/react";
import { Button } from "@heroui/react";
import clsx from "clsx";

import { defaultColorScheme, useTheme } from "@/components/theme-provider";
import { scrollbarClasses } from "@/utils/classes";

import IcOutlinePalette from "~icons/ic/outline-palette";
import IcOutlineLightMode from "~icons/ic/outline-light-mode";
import IcOutlineDarkMode from "~icons/ic/outline-dark-mode";
import IcOutlineSettingsBrightness from "~icons/ic/outline-settings-brightness";

const swatches = [
  "#ef4444",
  "#f97316",
  "#eab308",
  "#22c55e",
  "#14b8a6",
  "#06b6d4",
  "#3b82f6",
  "#6366f1",
  "#8b5cf6",
  "#a855f7",
  "#d946ef",
  "#ec4899",
  "#f43f5e",
  "#78716c",
  "#1e293b",
];

export const AppearanceTab = memo(() => {
  const { colorScheme, setColorScheme, theme, setTheme } = useTheme();

  // Local state for the color picker to be snappy
  const [localColor, setLocalColor] = useState(colorScheme.color);

  // Sync local color with global state when global state changes (e.g. on reset)
  useEffect(() => {
    setLocalColor(colorScheme.color);
  }, [colorScheme.color]);

  const handleSwatchPickerChange = useCallback(
    (color: any) => {
      const hex = color.toString("hex");
      setLocalColor(hex);
      setColorScheme({ color: hex });
    },
    [setColorScheme],
  );

  const handleReset = useCallback(() => {
    setColorScheme(defaultColorScheme);
  }, [setColorScheme]);

  return (
    <div className={clsx("flex flex-col gap-6 p-4 h-full overflow-y-auto", scrollbarClasses)}>
      <div className="bg-surface rounded-3xl p-6 border border-border/50 flex flex-col gap-6">
        <div className="flex items-start gap-4">
          <div className="p-3 rounded-2xl bg-accent-soft">
            <IcOutlineSettingsBrightness className="size-6 text-accent-soft-foreground" />
          </div>
          <div className="flex-1 min-w-0">
            <h3 className="text-xl font-semibold mb-1">Theme Mode</h3>
            <p className="text-sm text-muted">Choose how the app looks to you.</p>
          </div>
        </div>

        <div className="grid grid-cols-3 gap-3">
          {(["light", "dark", "system"] as const).map((mode) => {
            const Icon =
              mode === "light"
                ? IcOutlineLightMode
                : mode === "dark"
                  ? IcOutlineDarkMode
                  : IcOutlineSettingsBrightness;
            return (
              <button
                key={mode}
                type="button"
                onClick={() => setTheme(mode)}
                className={clsx(
                  "flex flex-col items-center gap-2 py-5 px-4 rounded-2xl cursor-pointer transition-all border-2",
                  theme === mode
                    ? "bg-accent-soft border-secondary text-accent-soft-foreground"
                    : "bg-surface border-transparent hover:bg-accent-soft/30",
                )}
              >
                <Icon className="size-6" />
                <span className="text-sm font-medium capitalize">{mode}</span>
              </button>
            );
          })}
        </div>
      </div>

      <div className="bg-surface rounded-3xl p-6 border border-border/50 flex flex-col gap-6">
        <div className="flex items-start gap-4">
          <div className="p-3 rounded-2xl bg-accent-soft">
            <IcOutlinePalette className="size-6 text-accent-soft-foreground" />
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center justify-between">
              <h3 className="text-xl font-semibold">Primary Color</h3>
              <Button
                variant="ghost"
                className="text-muted hover:text-foreground"
                onPress={handleReset}
              >
                Reset
              </Button>
            </div>
            <p className="text-sm text-muted">
              Personalize the app with your favorite color.
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <ColorSwatchPicker
            value={localColor}
            onChange={handleSwatchPickerChange}
            size="md"
            variant="circle"
            className="gap-2"
          >
            {swatches.map((swatchColor) => (
              <ColorSwatchPicker.Item key={swatchColor} color={swatchColor}>
                <ColorSwatchPicker.Swatch />
                <ColorSwatchPicker.Indicator />
              </ColorSwatchPicker.Item>
            ))}
          </ColorSwatchPicker>
        </div>
      </div>
    </div>
  );
});
