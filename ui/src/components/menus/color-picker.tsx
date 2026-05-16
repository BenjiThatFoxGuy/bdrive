import { memo } from "react";
import { Popover } from "@heroui/react";
import { Button } from "@heroui/react";
import TablerColorPicker from "~icons/tabler/color-picker";
import { HexColorPicker } from "react-colorful";

interface ColorPickerMenuProps {
  color: string;
  setColor: (color: string) => void;
}
export const ColorPickerMenu = memo(({ color, setColor }: ColorPickerMenuProps) => {
  return (
    <Popover>
      <Popover.Trigger>
        <Button aria-label="Choose Color"
          variant="secondary"
          isIconOnly
          className="min-w-10 size-10 rounded-xl"
        >
          <TablerColorPicker className="size-5" />
        </Button>
      </Popover.Trigger>
      <Popover.Content className="flex flex-col gap-2 p-3 rounded-[24px] relative">
        <HexColorPicker className="!w-full !h-40" color={color} onChange={setColor} />
        <div className="flex items-center gap-2 px-1 pt-1">
          <div
            className="size-6 rounded-full border border-border"
            style={{ backgroundColor: color }}
          />
          <span className="text-sm font-mono font-medium text-muted uppercase">
            {color}
          </span>
        </div>
      </Popover.Content>
    </Popover>
  );
});
