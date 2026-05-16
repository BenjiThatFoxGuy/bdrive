import { useNavigate } from "@tanstack/react-router";
import { Avatar, Dropdown } from "@heroui/react";
import IconBaselineLogout from "~icons/ic/baseline-logout";
import IconOutlineSettings from "~icons/ic/outline-settings";

import { useSession } from "@/utils/query-options";
import { $api } from "@/utils/api";
import { useCallback } from "react";

export function ProfileDropDown() {
  const [session] = useSession();

  const signOut = $api.useMutation("post", "/auth/logout");

  const navigate = useNavigate();

  const onSignOut = useCallback(() => {
    signOut.mutateAsync({}).then(() => {
      window.location.replace(new URL("/login", window.location.origin));
    });
  }, [signOut]);

  return (
    <Dropdown>
      <Dropdown.Trigger>
        <Avatar className="outline-none shrink-0 hover:ring-4 ring-foreground/5 transition-all">
          <Avatar.Image src={"/api/users/profile"} />
        </Avatar>
      </Dropdown.Trigger>
      <Dropdown.Popover>
        <Dropdown.Menu
          aria-label="Profile Menu"
          onAction={(key) => {
            if (String(key) === "settings") {
              navigate({ to: "/settings/$tabId", params: { tabId: "general" } });
            }
          }}
        >
          <Dropdown.Item
            key="profile"
            className="pointer-events-none mb-1 data-[hover=true]:bg-transparent"
          >
            <div className="flex flex-col">
              <p className="text-xs text-muted">Logged in as</p>
              <p className="font-bold text-foreground">{session?.userName}</p>
            </div>
          </Dropdown.Item>

          <Dropdown.Item
            key="settings"
          >
            Settings
          </Dropdown.Item>
          <Dropdown.Item
            key="logout"
            onPress={onSignOut}
          >
            Logout
          </Dropdown.Item>
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
}
