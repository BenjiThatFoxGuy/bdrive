import { memo, useMemo, useState } from "react";
import {
  Button,
  Input,
  ListBox,
  Popover,
  Separator,
} from "@heroui/react";
import clsx from "clsx";
import type { ControllerRenderProps } from "react-hook-form";

import { type FormState, isoCodeMap, isoCodes } from "@/components/login";
import { scrollbarClasses } from "@/utils/classes";
import { flags } from "@/utils/country-flags";

interface PhoneNoPickerProps {
  field: ControllerRenderProps<FormState, "phoneCode">;
}

export const PhoneNoPicker = memo(({ field }: PhoneNoPickerProps) => {
  const [isOpen, setIsOpen] = useState(false);

  const [value, setValue] = useState("");

  const codes = useMemo(
    () =>
      value
        ? isoCodes.filter((composer) =>
            composer.country.toLowerCase().includes(value.toLowerCase()),
          )
        : isoCodes,
    [value],
  );
  const TriggerIcon = flags[field.value as keyof typeof flags];

  return (
    <Popover
      isOpen={isOpen}
      onOpenChange={(open) => setIsOpen(open)}
    >
      <Popover.Trigger>
        <Button
          variant="ghost"
          className="outline-none flex gap-3 items-center shrink-0 min-w-0 h-auto p-0"
        >
          <TriggerIcon width={30} height={20} className="rounded-sm" />
          <span className="text-foreground font-medium min-w-10">
            {isoCodeMap[field.value].value}
          </span>
          <Separator className="h-6" orientation="vertical" />
        </Button>
      </Popover.Trigger>
      <Popover.Content>
        <div className="flex flex-col w-full gap-2">
          <Input
            value={value}
            placeholder="Search country..."
            className="w-full px-2 pt-2"
            onChange={(e) => setValue(e.target.value)}
          />
          <ListBox
            aria-label="Country Code"
            items={codes}
            className={clsx("max-h-72 overflow-y-auto pr-1", scrollbarClasses)}
            onAction={(key) => {
              field.onChange({ target: { value: key } });
              setIsOpen(false);
            }}
          >
            {(item) => {
              const Flag = flags[item.code as keyof typeof flags];
              return (
                <ListBox.Item
                  key={item.code}
                  textValue={item.country}
                >
                  <div className="flex w-full items-center gap-4">
                    <Flag
                      className="shrink-0 rounded-sm"
                      width={24}
                      height={16}
                    />
                    <span className="flex-1 truncate">{item.country}</span>
                    <span className="text-muted font-mono text-sm">
                      {item.value}
                    </span>
                  </div>
                </ListBox.Item>
              );
            }}
          </ListBox>
        </div>
      </Popover.Content>
    </Popover>
  );
});
