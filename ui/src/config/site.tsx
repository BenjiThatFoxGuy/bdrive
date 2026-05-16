import type { SVGProps } from "react";
import IconBasilGoogleDriveOutline from "~icons/basil/google-drive-outline";
import IconBasilGoogleDriveSolid from "~icons/basil/google-drive-solid";
import IconMdiRecent from "~icons/mdi/recent";
import ShareRegularIcon from "~icons/fluent/share-24-regular";
import ShareFilledIcon from "~icons/fluent/share-24-filled";
import IconIcOutlineSdStorage from "~icons/ic/outline-sd-storage";
import IconIcBaselineSdStorage from "~icons/ic/baseline-sd-storage";

export interface NavItem {
  id: string;
  label: string;
  icon: (props: SVGProps<SVGSVGElement>) => React.ReactNode;
  activeIcon: (props: SVGProps<SVGSVGElement>) => React.ReactNode;
  options: Record<string, unknown>;
}

export const siteConfig = {
  navItems: [
    {
      id: "my-drive",
      label: "My Drive",
      icon: IconBasilGoogleDriveOutline,
      activeIcon: IconBasilGoogleDriveSolid,
      options: { to: "/$view" as const, params: { view: "my-drive" }, search: { path: "/" } },
    },
    {
      id: "recent",
      label: "Recent",
      icon: IconMdiRecent,
      activeIcon: IconMdiRecent,
      options: { to: "/$view" as const, params: { view: "recent" } },
    },
    {
      id: "shared",
      label: "Shared",
      icon: ShareRegularIcon,
      activeIcon: ShareFilledIcon,
      options: { to: "/$view" as const, params: { view: "shared" } },
    },
    {
      id: "storage",
      label: "Storage",
      icon: IconIcOutlineSdStorage,
      activeIcon: IconIcBaselineSdStorage,
      options: { to: "/storage" as const },
    },
  ] satisfies NavItem[],
};
