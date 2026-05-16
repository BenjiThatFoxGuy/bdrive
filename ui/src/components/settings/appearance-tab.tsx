import { memo, useCallback, useEffect, useState } from "react";
import { Radio, RadioGroup } from "@heroui/react";
import { Button } from "@heroui/react";
import clsx from "clsx";
import debounce from "lodash.debounce";

import { ColorPickerMenu } from "@/components/menus/color-picker";
import { defaultColorScheme, useTheme } from "@/components/theme-provider";
import { scrollbarClasses } from "@/utils/classes";

import IcOutlinePalette from "~icons/ic/outline-palette";
import IcOutlineLightMode from "~icons/ic/outline-light-mode";
import IcOutlineDarkMode from "~icons/ic/outline-dark-mode";
import IcOutlineSettingsBrightness from "~icons/ic/outline-settings-brightness";
import IcBaselineRestartAlt from "~icons/ic/baseline-restart-alt";

const swatches = [
  "#ff8a80",
  "#ff80ab",
  "#ea80fc",
  "#b388ff",
  "#8c9eff",
  "#82b1ff",
  "#80d8ff",
  "#84ffff",
  "#a7ffeb",
  "#b9f6ca",
  "#ccff90",
  "#f4ff81",
  "#ffff8d",
  "#ffe57f",
  "#ffd180",
  "#ff9e80",
  "#d7ccc8",
  "#f5f5f5",
  "#cfd8dc",
];

export const AppearanceTab = memo(() => {
  const { colorScheme, setColorScheme, theme, setTheme } = useTheme();

  // Local state for the color picker to be snappy
  const [localColor, setLocalColor] = useState(colorScheme.color);

  // Sync local color with global state when global state changes (e.g. on reset)
  useEffect(() => {
    setLocalColor(colorScheme.color);
  }, [colorScheme.color]);

  // Debounced global state update for the color picker
  const debouncedSetColorScheme = useCallback(
    debounce((color: string) => {
      setColorScheme({ color });
    }, 200),
    [setColorScheme],
  );

  const handleColorChange = useCallback(
    (newColor: string) => {
      setLocalColor(newColor);
      debouncedSetColorScheme(newColor);
    },
    [debouncedSetColorScheme],
  );

  const handleSwatchClick = useCallback(
    (newColor: string) => {
      setLocalColor(newColor);
      setColorScheme({ color: newColor });
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

        <RadioGroup
          value={theme} onChange={(v) => setTheme(v as any)}
        >
          <div
            className={clsx(
              "flex items-center gap-4 p-4 rounded-2xl cursor-pointer transition-colors border-2",
              theme === "light"
                ? "bg-accent-soft border-secondary text-accent-soft-foreground"
                : "bg-surface border-transparent hover:bg-surface-secondary",
            )}
            onClick={() => setTheme("light")}
          >
            <Radio value="light">
              <Radio.Control>
                <Radio.Indicator />
              </Radio.Control>
            </Radio>
            <IcOutlineLightMode className="size-6" />
            <span className="font-medium">Light</span>
          </div>

          <div
            className={clsx(
              "flex items-center gap-4 p-4 rounded-2xl cursor-pointer transition-colors border-2",
              theme === "dark"
                ? "bg-accent-soft border-secondary text-accent-soft-foreground"
                : "bg-surface border-transparent hover:bg-surface-secondary",
            )}
            onClick={() => setTheme("dark")}
          >
            <Radio value="dark">
              <Radio.Control>
                <Radio.Indicator />
              </Radio.Control>
            </Radio>
            <IcOutlineDarkMode className="size-6" />
            <span className="font-medium">Dark</span>
          </div>

          <div
            className={clsx(
              "flex items-center gap-4 p-4 rounded-2xl cursor-pointer transition-colors border-2",
              theme === "system"
                ? "bg-accent-soft border-secondary text-accent-soft-foreground"
                : "bg-surface border-transparent hover:bg-surface-secondary",
            )}
            onClick={() => setTheme("system")}
          >
            <Radio value="system">
              <Radio.Control>
                <Radio.Indicator />
              </Radio.Control>
            </Radio>
            <IcOutlineSettingsBrightness className="size-6" />
            <span className="font-medium">System</span>
          </div>
        </RadioGroup>
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

        <div className="flex flex-wrap gap-4 items-center pl-1">
          {swatches.map((swatchColor) => (
            <button
              type="button"
              key={swatchColor}
              style={{ backgroundColor: swatchColor }}
              onClick={() => handleSwatchClick(swatchColor)}
              className={clsx(
                "size-10 rounded-full transition-all duration-200 border-4",
                localColor === swatchColor
                  ? "border-foreground scale-110 shadow-lg"
                  : "border-transparent hover:scale-105",
              )}
              title={swatchColor}
            />
          ))}
          <ColorPickerMenu color={localColor} setColor={handleColorChange} />
        </div>
      </div>
    </div>
  );
});
