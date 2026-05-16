import { memo, useCallback, useEffect, useState } from "react";
import { FieldError, InputGroup, ListBox, Select, Switch, TextField } from "@heroui/react";
import clsx from "clsx";
import type { SettingFieldConfig } from "@/config/settings";
import { debounce } from "@/utils/debounce";

interface SettingsFieldProps<T> {
  config: SettingFieldConfig<T>;
  value: T;
  onChange: (value: T) => void;
  disabled?: boolean;
}

function validateUrl(value: string): boolean {
  if (!value) {return true;}
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

export const SettingsField = memo(
  <T,>({ config, value, onChange, disabled }: SettingsFieldProps<T>) => {
    const [error, setError] = useState("");
    const [localValue, setLocalValue] = useState<T>(value);

    useEffect(() => {
      setLocalValue(value);
    }, [value]);

    const debouncedValidate = useCallback(
      debounce((newValue: T) => {
        validateAndSave(newValue);
      }, 1000),
      [config],
    );

    const validateAndSave = (newValue: T) => {
      let errorMessage = "";

      if (config.type === "url" && typeof newValue === "string") {
        if (newValue && !validateUrl(newValue)) {
          errorMessage = "Invalid URL format";
        }
      } else if (config.validation?.pattern && typeof newValue === "string") {
        if (newValue && !config.validation.pattern.test(newValue)) {
          errorMessage = "Invalid format";
        }
      } else if (config.validation?.custom && newValue) {
        const result = config.validation.custom(newValue as any);
        if (result !== true) {
          errorMessage = result;
        }
      }

      setError(errorMessage);

      if (!errorMessage) {
        onChange(newValue);
      }
    };

    const handleFieldChange = (newValue: T) => {
      setLocalValue(newValue);
      debouncedValidate(newValue);
    };

    const renderField = () => {
      switch (config.type) {
        case "text":
        case "email":
        case "url":
          return (
            <TextField
              aria-label={config.label}
              value={localValue as string}
              onChange={(v) => handleFieldChange(v as T)}
              isInvalid={Boolean(error)}
              isDisabled={disabled}
            >
              <InputGroup >
                <InputGroup.Input
                  placeholder={config.placeholder}
                  type={config.type === "text" ? undefined : config.type}
                />
              </InputGroup>
              {error && <FieldError>{error}</FieldError>}
            </TextField>
          );

        case "number":
          return (
            <TextField
              aria-label={config.label}
              value={localValue != null ? String(localValue) : ""}
              onChange={(v) => handleFieldChange(Number(v) as T)}
              isInvalid={Boolean(error)}
              isDisabled={disabled}
            >
              <InputGroup >
                <InputGroup.Input type="number" placeholder={config.placeholder} />
              </InputGroup>
              {error && <FieldError>{error}</FieldError>}
            </TextField>
          );

        case "select":
          return (
            <Select
              aria-label={config.label}
              value={localValue != null ? String(localValue) : null}
              onChange={(key) => {
                if (key !== null) {
                  const option = config.options?.find(
                    (opt) => String(opt.value) === key,
                  );
                  if (option) {
                    handleFieldChange(option.value as T);
                  }
                }
              }}
              isDisabled={disabled}
            >
              <Select.Trigger>
                <Select.Value />
                <Select.Indicator />
              </Select.Trigger>
              <Select.Popover>
                <ListBox className="rounded-xl">
                  {(config.options || []).map((item: any) => (
                    <ListBox.Item
                      key={String(item.value)}
                      id={String(item.value)}
                      textValue={String(item.label)}
                      className="rounded-xl px-4 py-2.5"
                    >
                      {item.label}
                      <ListBox.ItemIndicator />
                    </ListBox.Item>
                  ))}
                </ListBox>
              </Select.Popover>
            </Select>
          );

        case "switch":
          return (
            <Switch
              size="lg"
              onChange={(isSelected) => handleFieldChange(isSelected as T)}
              isSelected={localValue as boolean}
              name={config.key}
              isDisabled={disabled}
            >
              <Switch.Control>
                <Switch.Thumb />
              </Switch.Control>
            </Switch>
          );

        default:
          return null;
      }
    };

    return (
      <div className="flex flex-col md:flex-row gap-4 md:items-center justify-between py-2 border-b border-border/30 last:border-0 last:pb-0 first:pt-0">
        <div className="flex-1 min-w-0">
          <p className="text-base font-semibold">
            {config.label}
          </p>
          <p className="text-sm font-normal text-muted max-w-xl">
            {config.description}
          </p>
        </div>
        <div
          className={clsx(
            "flex justify-start min-w-[200px] md:justify-end",
            disabled && "opacity-50",
          )}
        >
          {renderField()}
        </div>
      </div>
    );
  },
);
