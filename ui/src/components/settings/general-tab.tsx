import { memo, useCallback } from "react";
import { scrollbarClasses } from "@/utils/classes";
import clsx from "clsx";
import { Button } from "@heroui/react";

import { categoryConfig, generalSettingsConfig } from "@/config/settings";
import { SettingsField } from "./settings-field";
import { useSettingsStore } from "@/utils/stores/settings";

import IcBaselineCloudUpload from "~icons/ic/baseline-cloud-upload";
import IcBaselineSettings from "~icons/ic/baseline-settings";
import IcBaselineTv from "~icons/ic/baseline-tv";
import IcBaselineRestore from "~icons/ic/baseline-restore";

const iconMap: Record<string, React.ElementType> = {
  display: IcBaselineTv,
  other: IcBaselineSettings,
  upload: IcBaselineCloudUpload,
};

export const GeneralTab = memo(() => {
  const { settings, updateSetting, resetSettings } = useSettingsStore();

  const categories = ["upload", "display", "other"] as const;

  const handleFieldChange = useCallback(
    (key: keyof typeof settings, value: any) => {
      if (value instanceof HTMLElement || value instanceof Event) {
        console.error("Invalid value type for setting:", key, value);
        return;
      }
      updateSetting(key, value);
    },
    [updateSetting],
  );

  return (
    <div className={clsx("flex flex-col gap-6 p-4 h-full overflow-y-auto", scrollbarClasses)}>
      {categories.map((category) => {
        const fields = generalSettingsConfig.filter((f) => f.category === category);
        if (fields.length === 0) {return null;}

        const catConfig = categoryConfig[category];
        const Icon = iconMap[category] || IcBaselineSettings;

        return (
          <div
            key={category}
            className="rounded-3xl p-6 bg-surface border border-border flex flex-col gap-6"
          >
            <div className="flex items-start gap-4">
              <div className="p-3 rounded-2xl bg-accent-soft">
                <Icon className="size-6 text-accent-soft-foreground" />
              </div>
              <div className="flex-1 min-w-0">
                <h3 className="text-xl font-semibold mb-1">{catConfig.title}</h3>
                <p className="text-sm text-muted">{catConfig.description}</p>
              </div>
            </div>
            <div className="space-y-6">
              {fields.map((field) => (
                <SettingsField
                  key={field.key}
                  config={field}
                  value={settings[field.key]}
                  onChange={(value) => handleFieldChange(field.key, value)}
                />
              ))}
            </div>
          </div>
        );
      })}
      <div className="mt-2 mb-6 flex justify-center">
        <Button
          variant="secondary"
          className="px-8 py-6 rounded-2xl font-semibold"
          onPress={resetSettings}
        >
          Reset All Settings
        </Button>
      </div>
    </div>
  );
});
