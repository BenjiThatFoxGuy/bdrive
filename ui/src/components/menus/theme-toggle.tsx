import { Dropdown } from "@heroui/react";
import { Button } from "@heroui/react";
import IconPhSun from "~icons/ph/sun";
import IconRiMoonClearLine from "~icons/ri/moon-clear-line";
import IconIcOutlineSettingsBrightness from "~icons/ic/outline-settings-brightness";

import { useTheme } from "@/components/theme-provider";

export function ThemeToggle() {
  const { setTheme } = useTheme();

  return (
    <Dropdown>
      <Dropdown.Trigger>
        <Button
          variant="ghost"
          isIconOnly
        >
          <IconPhSun className="pointer-events-none size-6 rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0" />
          <IconRiMoonClearLine
            className="pointer-events-none absolute size-6 rotate-90 scale-0 transition-all
            dark:rotate-0 dark:scale-100"
          />
          <span className="sr-only">Toggle theme</span>
        </Button>
      </Dropdown.Trigger>
      <Dropdown.Popover>
        <Dropdown.Menu
          aria-label="Theme Menu"
        >
          <Dropdown.Item
            key="light"
            onPress={() => setTheme("light")}
          >
            Light
          </Dropdown.Item>
          <Dropdown.Item
            key="dark"
            onPress={() => setTheme("dark")}
          >
            Dark
          </Dropdown.Item>
          <Dropdown.Item
            key="system"
            onPress={() => setTheme("system")}
          >
            System
          </Dropdown.Item>
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
}
